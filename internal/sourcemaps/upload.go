package sourcemaps

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Defaults for Options.
const (
	DefaultMaxMaps         = 100              // Tlm accepts up to 100 maps per request.
	DefaultMaxRequestBytes = 90 * 1024 * 1024 // Tlm accepts up to 100 MB per request.
	DefaultRetries         = 3
	DefaultBackoff         = time.Second
	DefaultTimeout         = 2 * time.Minute
)

// maxErrorBody is how much of an error answer is read.
const maxErrorBody = 64 * 1024

// Options say where and how to upload.
type Options struct {
	// Release is the release the maps belong to, exactly as the SDK sends it.
	Release string
	// Host is Tlm's base URL, e.g. from HostFromDSN.
	Host string
	// Token is an upload token (tlmu_…). Surrounding whitespace (a pasted CI
	// secret) is ignored.
	Token Token
	// HTTPClient sends the requests; nil means a new http.Client.
	HTTPClient *http.Client
	// Log gets a line per uploaded map ("uploaded /assets/a.js"). May be nil.
	Log func(line string)
	// MaxMaps is the most maps per request; 0 means DefaultMaxMaps.
	MaxMaps int
	// MaxRequestBytes is the most compressed bytes per request (a single
	// larger map still goes alone); 0 means DefaultMaxRequestBytes.
	MaxRequestBytes int64
	// Retries is how many more attempts a request gets after a server or
	// network error: 0 means DefaultRetries, and a negative number none.
	Retries int
	// Backoff is the first wait between attempts; it doubles each time.
	// 0 means DefaultBackoff.
	Backoff time.Duration
	// Timeout is how long one attempt may take before it's abandoned (and
	// retried); 0 means DefaultTimeout.
	Timeout time.Duration
	// Sleep waits between attempts; nil waits for real, or until ctx ends.
	Sleep func(ctx context.Context, d time.Duration) error
	// UserAgent, when set, is sent as User-Agent.
	UserAgent string
}

// Result is what Upload did. Errors are ready to show after "error: ":
// they never carry the token, and the server's words in them have had
// their control characters neutralized.
type Result struct {
	// OK is true when every map was uploaded.
	OK bool
	// Uploaded lists the URL paths of the bundles whose maps were uploaded.
	Uploaded []string
	// Errors says what wasn't uploaded, and why.
	Errors []string
}

type prepared struct {
	m  Map
	gz []byte
}

type outcome struct {
	err   string
	fatal bool // no later batch would fare better
}

// Upload uploads the maps to Tlm in gzipped batches, retrying server
// and network errors with backoff. It never stops at a failed batch unless
// the failure is about the credentials or ctx ended; every failure is in
// Result.Errors.
func Upload(ctx context.Context, maps []Map, opts Options) Result {
	u := uploader{opts: opts, token: strings.TrimSpace(string(opts.Token))}
	u.defaults()

	result := Result{OK: true}
	var items []prepared
	for _, m := range maps {
		gz, err := gzipMap(m)
		if err != nil {
			result.OK = false
			result.Errors = append(result.Errors, u.scrub(fmt.Sprintf("couldn't read %s: %v", m.MapFile, err)))
			continue
		}
		items = append(items, prepared{m: m, gz: gz})
	}

	for _, batch := range batches(items, u.opts.MaxMaps, u.opts.MaxRequestBytes) {
		out := u.send(ctx, batch)
		if out == nil {
			for _, item := range batch {
				result.Uploaded = append(result.Uploaded, item.m.Path)
				if u.opts.Log != nil {
					u.opts.Log(u.scrub("uploaded " + item.m.Path))
				}
			}
			continue
		}
		result.OK = false
		result.Errors = append(result.Errors, u.scrub(fmt.Sprintf("%d map(s) not uploaded: %s", len(batch), out.err)))
		if out.fatal {
			break
		}
	}
	return result
}

type uploader struct {
	opts  Options
	token string
}

func (u *uploader) defaults() {
	o := &u.opts
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}
	if o.MaxMaps <= 0 {
		o.MaxMaps = DefaultMaxMaps
	}
	if o.MaxRequestBytes <= 0 {
		o.MaxRequestBytes = DefaultMaxRequestBytes
	}
	switch {
	case o.Retries == 0:
		o.Retries = DefaultRetries
	case o.Retries < 0:
		o.Retries = 0
	}
	if o.Backoff <= 0 {
		o.Backoff = DefaultBackoff
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Sleep == nil {
		o.Sleep = sleepContext
	}
	o.Host = strings.TrimRight(o.Host, "/")
}

// scrub: whatever the server or the network says, the token is never repeated.
func (u *uploader) scrub(text string) string { return scrub(text, u.token) }

func gzipMap(m Map) ([]byte, error) {
	data := m.Content
	if data == nil {
		var err error
		if data, err = os.ReadFile(m.MapFile); err != nil {
			if pe, ok := errors.AsType[*fs.PathError](err); ok {
				return nil, pe.Err
			}
			return nil, err
		}
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	return buf.Bytes(), nil
}

func batches(items []prepared, maxMaps int, maxBytes int64) [][]prepared {
	var out [][]prepared
	var current []prepared
	var size int64
	for _, item := range items {
		n := int64(len(item.gz))
		if len(current) > 0 && (len(current) >= maxMaps || size+n > maxBytes) {
			out = append(out, current)
			current, size = nil, 0
		}
		current = append(current, item)
		size += n
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

var mapSuffix = regexp.MustCompile(`(\.map)?$`)

// partNames gives each map file a flat, unique part name: index.js.map,
// then index.js-2.map.
func partNames(mapFiles []string) []string {
	used := map[string]bool{}
	names := make([]string, len(mapFiles))
	for i, file := range mapFiles {
		name := filepath.Base(file)
		candidate := name
		for n := 2; used[candidate]; n++ {
			candidate = mapSuffix.ReplaceAllString(name, "-"+strconv.Itoa(n)+"${1}")
		}
		used[candidate] = true
		names[i] = candidate
	}
	return names
}

func (u *uploader) body(items []prepared) (data []byte, contentType string, err error) {
	files := make([]string, len(items))
	for i, item := range items {
		files[i] = item.m.MapFile
	}
	names := partNames(files)
	type manifestEntry struct {
		Path string `json:"path"`
		Map  string `json:"map"`
	}
	manifest := make([]manifestEntry, len(items))
	for i, item := range items {
		manifest[i] = manifestEntry{Path: item.m.Path, Map: names[i]}
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("encoding the manifest: %w", err)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("release", u.opts.Release); err != nil {
		return nil, "", fmt.Errorf("building the request: %w", err)
	}
	if err := w.WriteField("manifest", string(manifestJSON)); err != nil {
		return nil, "", fmt.Errorf("building the request: %w", err)
	}
	for i, item := range items {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", multipart.FileContentDisposition(names[i], names[i]))
		h.Set("Content-Type", "application/gzip")
		pw, err := w.CreatePart(h)
		if err != nil {
			return nil, "", fmt.Errorf("building the request: %w", err)
		}
		if _, err := pw.Write(item.gz); err != nil {
			return nil, "", fmt.Errorf("building the request: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("building the request: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// send uploads one batch, with retries. nil means it was stored.
func (u *uploader) send(ctx context.Context, items []prepared) *outcome {
	body, contentType, err := u.body(items)
	if err != nil {
		return &outcome{err: err.Error()}
	}
	endpoint := u.opts.Host + "/api/sourcemaps"
	last := ""
	for attempt := 0; attempt <= u.opts.Retries; attempt++ {
		if attempt > 0 {
			wait := u.opts.Backoff * time.Duration(math.Pow(2, float64(attempt-1)))
			if err := u.opts.Sleep(ctx, wait); err != nil {
				return &outcome{err: fmt.Sprintf("%s; stopped: %v", last, err), fatal: true}
			}
		}
		status, text, err := u.attempt(ctx, endpoint, body, contentType)
		if err != nil {
			if ctx.Err() != nil {
				return &outcome{err: "stopped: " + ctx.Err().Error(), fatal: true}
			}
			last = u.networkError(err)
			continue
		}
		if status >= 200 && status < 300 {
			return nil
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return &outcome{
				err:   fmt.Sprintf("Tlm rejected the upload token (%s). Check TLM_UPLOAD_TOKEN: it must be an upload token (tlmu_…) from this project's settings, not the DSN's key.", text),
				fatal: true,
			}
		}
		// 408 and 429 are worth another try, like a 5xx.
		if status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
			return &outcome{err: text}
		}
		last = text
	}
	return &outcome{err: fmt.Sprintf("%s, after %d attempts", last, u.opts.Retries+1)}
}

// attempt makes one request. On an answer, text is "HTTP <status>" and the
// server's message.
func (u *uploader) attempt(ctx context.Context, endpoint string, body []byte, contentType string) (status int, text string, err error) {
	ctx, cancel := context.WithTimeout(ctx, u.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	if u.opts.UserAgent != "" {
		req.Header.Set("User-Agent", u.opts.UserAgent)
	}
	resp, err := u.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		return resp.StatusCode, "", nil
	}
	return resp.StatusCode, errorOf(resp), nil
}

// errorOf is an error answer as text: "HTTP 400: …" when Tlm sent
// {"error": "…"}, else just "HTTP 502".
func errorOf(resp *http.Response) string {
	text := "HTTP " + strconv.Itoa(resp.StatusCode)
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return text
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil || body.Error == "" {
		return text
	}
	return text + ": " + neutralize(body.Error)
}

// networkError says why a request got no answer.
func (u *uploader) networkError(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return fmt.Sprintf("%s timed out (no answer within %s)", u.opts.Host, seconds(u.opts.Timeout))
	}
	return fmt.Sprintf("couldn't reach %s (%s)", u.opts.Host, networkDetail(err))
}

// networkDetail is the cause of a failed request, without the request's
// method and URL.
func networkDetail(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "the connection closed without an answer"
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err.Error()
	}
	return err.Error()
}

// seconds shows a timeout the way people say it: "120 s", or "100ms" below
// a second.
func seconds(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}
	return fmt.Sprintf("%.0f s", math.Round(d.Seconds()))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// HostFromDSN is Tlm's base URL from a project's DSN:
// https://tlm_…@host:port becomes https://host:port. An empty DSN means
// DefaultHost. A DSN that isn't one is a *UsageError.
func HostFromDSN(dsn string) (string, error) {
	if dsn == "" {
		return DefaultHost, nil
	}
	u, err := parseDSN(dsn)
	if err != nil {
		return "", err
	}
	return origin(u), nil
}

func parseDSN(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Hostname() == "" {
		return nil, newUsageError("invalid DSN: expected something like https://tlm_…@tlm.hullop.com (copy it from your project's setup page).")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, newUsageError("invalid DSN: it must start with https:// (or http:// for a local Tlm).")
	}
	return u, nil
}

// origin is scheme://host[:port], lowercased, without a default port.
func origin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	defaultPort := map[string]string{"https": "443", "http": "80"}[u.Scheme]
	if port := u.Port(); port != "" && port != defaultPort {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

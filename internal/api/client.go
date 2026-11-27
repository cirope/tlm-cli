// Package api is the HTTP client for Tlm's management API
// (/api/v1): typed methods per endpoint over a small core that handles
// headers, errors, timeouts, redirects and retries.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
	defaultMaxBody    = 32 << 20
	maxRedirects      = 5
	maxRetryAfter     = 60 * time.Second
)

// backoffs are the waits before each retry of a GET.
var backoffs = [...]time.Duration{500 * time.Millisecond, time.Second}

// Options configures a Client. Zero values pick sensible defaults.
type Options struct {
	URL        string // Tlm's origin: https://tlm.hullop.com.
	Token      Token  // Empty for the device flow's calls.
	Lang       string // "en", "es", or "" for the person's setting.
	UserAgent  string // tlm-cli/<version> (<os>; <arch>)
	HTTPClient *http.Client
	Logger     *slog.Logger // Requests and timings at debug level. Never tokens.
	Timeout    time.Duration
	MaxRetries int   // Retries of GETs on network and server errors; negative means none.
	MaxBody    int64 // Largest answer read.
	// NoRetry turns every retry off, 429 included: the device flow
	// paces itself.
	NoRetry bool

	// Sleep waits between retries; tests replace it to not sleep.
	Sleep func(context.Context, time.Duration) error
}

// Client talks to one Tlm with one token.
type Client struct {
	origin string
	base   *url.URL
	http   *http.Client
	o      Options
}

// Query is a request's query string. Empty values are left out.
type Query map[string]string

// OriginOf is url's scheme and host, lowercased, which is how a Tlm
// install is named: https://host:port/any/path → https://host:port.
func OriginOf(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("invalid Tlm URL %q: expected something like https://tlm.hullop.com", raw)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// New returns a client for opts.URL.
func New(opts Options) (*Client, error) {
	origin, err := OriginOf(opts.URL)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(origin + "/api/v1")
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	hc := *opts.HTTPClient
	// Redirects are followed here, by hand: only 307/308, only on this
	// Tlm, keeping the method and body.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	switch {
	case opts.MaxRetries == 0:
		opts.MaxRetries = defaultMaxRetries
	case opts.MaxRetries < 0, opts.NoRetry:
		opts.MaxRetries = 0
	}
	opts.MaxRetries = min(opts.MaxRetries, len(backoffs))
	if opts.MaxBody <= 0 {
		opts.MaxBody = defaultMaxBody
	}
	if opts.Sleep == nil {
		opts.Sleep = Sleep
	}
	return &Client{origin: origin, base: base, http: &hc, o: opts}, nil
}

// Origin is the Tlm this client talks to.
func (c *Client) Origin() string { return c.origin }

// Get fetches path (under /api/v1) as JSON, decodes it into out when out
// is not nil, and returns the raw answer.
func (c *Client) Get(ctx context.Context, path string, q Query, out any) ([]byte, error) {
	return c.Send(ctx, http.MethodGet, path, q, nil, out)
}

// GetAt is Get that also says where the answer came from, after
// redirects: a path under /api/v1, like the one asked for.
func (c *Client) GetAt(ctx context.Context, path string, q Query, out any) ([]byte, string, error) {
	data, final, err := c.do(ctx, http.MethodGet, path, q, nil, "application/json")
	if err != nil {
		return nil, "", err
	}
	if err := c.decode(data, out, http.MethodGet, path); err != nil {
		return nil, "", err
	}
	return data, strings.TrimPrefix(final.EscapedPath(), c.base.Path), nil
}

// Markdown fetches path as text/markdown.
func (c *Client) Markdown(ctx context.Context, path string, q Query) (string, error) {
	data, _, err := c.do(ctx, http.MethodGet, path, q, nil, "text/markdown")
	return string(data), err
}

// Send sends a request with body (when not nil) as JSON, decodes a JSON
// answer into out when out is not nil, and returns the raw answer, empty
// for 204.
func (c *Client) Send(ctx context.Context, method, path string, q Query, body, out any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("encoding the request: %w", err)
		}
	}
	data, _, err := c.do(ctx, method, path, q, payload, "application/json")
	if err != nil {
		return nil, err
	}
	if err := c.decode(data, out, method, path); err != nil {
		return nil, err
	}
	return data, nil
}

// decode reads a JSON answer into out, when there's both.
func (c *Client) decode(data []byte, out any, method, path string) error {
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Status: http.StatusOK, Code: "http_error",
			Message: fmt.Sprintf("Tlm's answer to %s %s isn't the JSON expected (%v)", method, path, err)}
	}
	return nil
}

// AgentGuide is Tlm's guide for AI agents to add Tlm to an app
// (/agents.md, outside the API), as Markdown. With stacks, the general
// steps and those stacks' sections only; without, the whole guide.
func (c *Client) AgentGuide(ctx context.Context, stacks []string) (string, error) {
	u, err := url.Parse(c.origin + "/agents.md")
	if err != nil {
		return "", fmt.Errorf("building the guide's address: %w", err)
	}
	if len(stacks) > 0 {
		// Commas separate stacks, so the value must go out as %2C.
		u.RawQuery = "stack=" + strings.Join(stacks, "%2C")
	}
	data, _, err := c.send(ctx, http.MethodGet, u, nil, "text/markdown")
	return string(data), err
}

// SendRaw sends payload as is with contentType, for `tlm api`.
func (c *Client) SendRaw(ctx context.Context, method, path string, q Query, payload []byte) ([]byte, error) {
	data, _, err := c.do(ctx, method, path, q, payload, "application/json")
	return data, err
}

// do is one logical request: redirects, one 429 retry, and for GETs
// retries on server and network errors. It returns the answer and the
// URL it came from.
func (c *Client) do(ctx context.Context, method, path string, q Query, payload []byte, accept string) ([]byte, *url.URL, error) {
	u, err := c.url(path, q)
	if err != nil {
		return nil, nil, err
	}
	return c.send(ctx, method, u, payload, accept)
}

// send is do for a URL already built.
func (c *Client) send(ctx context.Context, method string, u *url.URL, payload []byte, accept string) ([]byte, *url.URL, error) {
	redirects, failures, rateRetried := 0, 0, false
	for {
		resp, data, err := c.attempt(ctx, method, u, payload, accept)
		if err != nil {
			var e *Error
			if method == http.MethodGet && errors.As(err, &e) && e.Code == "network" && failures < c.o.MaxRetries {
				if err := c.wait(ctx, backoffs[failures], method, u, err); err != nil {
					return nil, nil, err
				}
				failures++
				continue
			}
			return nil, nil, err
		}
		switch status := resp.StatusCode; {
		case status == http.StatusPermanentRedirect || status == http.StatusTemporaryRedirect:
			next, err := c.redirect(u, resp.Header.Get("Location"), status)
			if err != nil {
				return nil, nil, err
			}
			redirects++
			if redirects > maxRedirects {
				return nil, nil, &Error{Status: status, Code: "http_error", Message: "too many redirects"}
			}
			u = next
		case status < 300:
			return data, u, nil
		case status == http.StatusTooManyRequests && !rateRetried && !c.o.NoRetry:
			rateRetried = true
			wait := retryAfter(resp.Header.Get("Retry-After"))
			if wait <= 0 {
				wait = time.Second
			}
			if err := c.wait(ctx, min(wait, maxRetryAfter), method, u, nil); err != nil {
				return nil, nil, err
			}
		case status >= 500 && method == http.MethodGet && failures < c.o.MaxRetries:
			if err := c.wait(ctx, backoffs[failures], method, u, nil); err != nil {
				return nil, nil, err
			}
			failures++
		default:
			return nil, nil, c.parseError(resp, data)
		}
	}
}

func (c *Client) wait(ctx context.Context, d time.Duration, method string, u *url.URL, cause error) error {
	c.o.Logger.Debug("retrying", "method", method, "path", u.Path, "after", d, "error", cause)
	return c.o.Sleep(ctx, d)
}

// attempt sends the request once. A network failure is an *Error with
// code "network"; a cancelled ctx is ctx's error.
// reply is a response read whole.
type reply struct {
	StatusCode int
	Header     http.Header
}

func (c *Client) attempt(ctx context.Context, method string, u *url.URL, payload []byte, accept string) (*reply, []byte, error) {
	actx, cancel := context.WithTimeout(ctx, c.o.Timeout)
	defer cancel()

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(actx, method, u.String(), body)
	if err != nil {
		return nil, nil, fmt.Errorf("building %s %s: %w", method, u.Path, err)
	}
	req.Header.Set("Accept", accept)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.o.UserAgent != "" {
		req.Header.Set("User-Agent", c.o.UserAgent)
	}
	if c.o.Lang != "" {
		req.Header.Set("Accept-Language", c.o.Lang)
	}
	if c.o.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.o.Token.Reveal())
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.o.Logger.Debug("request failed", "method", method, "path", u.Path, "took", time.Since(start), "error", err)
		return nil, nil, c.networkError(ctx, actx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.o.MaxBody+1))
	c.o.Logger.Debug("request", "method", method, "path", u.Path, "status", resp.StatusCode, "took", time.Since(start))
	if err != nil {
		return nil, nil, c.networkError(ctx, actx, err)
	}
	if int64(len(data)) > c.o.MaxBody {
		return nil, nil, &Error{Status: resp.StatusCode, Code: "http_error", Message: "Tlm's answer is too large"}
	}
	return &reply{StatusCode: resp.StatusCode, Header: resp.Header}, data, nil
}

func (c *Client) networkError(ctx, actx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(actx.Err(), context.DeadlineExceeded) {
		return &Error{Code: "network", Message: c.origin + " didn't answer in time"}
	}
	detail := err.Error()
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.As(err, &dns):
		detail = dns.Error()
	case errors.As(err, &op) && op.Err != nil:
		detail = op.Err.Error()
	}
	return &Error{Code: "network", Message: fmt.Sprintf("couldn't reach %s (%s)", c.origin, detail)}
}

// redirect resolves a Location, refusing to take the token anywhere but
// this Tlm.
func (c *Client) redirect(from *url.URL, location string, status int) (*url.URL, error) {
	if location == "" {
		return nil, &Error{Status: status, Code: "http_error", Message: "Tlm redirected without a Location"}
	}
	next, err := from.Parse(location)
	if err != nil {
		return nil, &Error{Status: status, Code: "http_error", Message: "Tlm redirected to an invalid Location"}
	}
	if o := next.Scheme + "://" + strings.ToLower(next.Host); o != c.origin {
		return nil, &Error{Status: status, Code: "http_error",
			Message: fmt.Sprintf("Tlm redirected to another site (%s); not following it", o)}
	}
	return next, nil
}

// url is the full URL for path, which is under /api/v1 and already
// escaped.
func (c *Client) url(path string, q Query) (*url.URL, error) {
	u, err := c.base.Parse(c.base.Path + path)
	if err != nil {
		return nil, fmt.Errorf("building the address for %s: %w", path, err)
	}
	v := url.Values{}
	for k, val := range q {
		if val != "" {
			v.Set(k, val)
		}
	}
	u.RawQuery = v.Encode()
	return u, nil
}

// parseError builds an *Error from an error answer, which may not be the
// envelope at all (a proxy's HTML page). The token never survives into
// the message.
func (c *Client) parseError(resp *reply, data []byte) *Error {
	e := &Error{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	var obj struct {
		Code    string              `json:"code"`
		Message string              `json:"message"`
		Fields  map[string][]string `json:"fields"`
	}
	var rfc string
	switch {
	case json.Unmarshal(data, &env) != nil:
		e.Code = ""
	case json.Unmarshal(env.Error, &obj) == nil && obj.Message != "":
		e.Code, e.Message, e.Fields = obj.Code, obj.Message, obj.Fields
		if e.Code == "" {
			e.Code = "http_error"
		}
	case json.Unmarshal(env.Error, &rfc) == nil && rfc != "":
		// The device flow's RFC 8628 errors: {"error": "slow_down"}.
		e.Code, e.Message = rfc, rfc
	}
	if e.Message == "" {
		e.Code, e.Message = "http_error", "HTTP "+strconv.Itoa(resp.StatusCode)
		if text := http.StatusText(resp.StatusCode); text != "" {
			e.Message += " " + text
		}
	}
	e.Message = c.o.Token.Scrub(e.Message)
	for k, msgs := range e.Fields {
		for i, m := range msgs {
			msgs[i] = c.o.Token.Scrub(m)
		}
		e.Fields[k] = msgs
	}
	return e
}

// retryAfter parses a Retry-After header in seconds; zero when absent.
func retryAfter(h string) time.Duration {
	s, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || s < 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

// Sleep waits d, or until ctx is done.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

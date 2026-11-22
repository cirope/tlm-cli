package sourcemaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const testToken Token = "tlmu_secret"

type uploadCase struct {
	files   map[string]string
	replyTo func(n int) reply
	opts    func(*Options)
}

type uploaded struct {
	result   Result
	lines    []string
	requests []received
	server   *fakeServer
	slept    []time.Duration
}

func runUpload(t *testing.T, c uploadCase) uploaded {
	t.Helper()
	srv := startServer(t, c.replyTo)
	var out uploaded
	opts := Options{
		Release: "r1",
		Host:    srv.URL,
		Token:   testToken,
		Log:     func(l string) { out.lines = append(out.lines, l) },
		Backoff: time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) error {
			out.slept = append(out.slept, d)
			return nil
		},
		HTTPClient: srv.Client(),
	}
	if c.opts != nil {
		c.opts(&opts)
	}
	out.result = Upload(context.Background(), mapsIn(t, c.files), opts)
	out.requests = srv.received()
	out.server = srv
	return out
}

func oneMap() map[string]string { return map[string]string{"a.js.map": testMap("a.js", nil)} }

func allErrors(r Result) string { return strings.Join(r.Errors, "\n") }

func TestUploadSendsReleaseManifestAndGzipParts(t *testing.T) {
	files := map[string]string{
		"assets/a.js":     "x\n//# sourceMappingURL=a.js.map",
		"assets/a.js.map": testMap("a.js", nil),
	}
	dir := writeTree(t, files)
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	srv := startServer(t, nil)
	var lines []string
	result := Upload(context.Background(), found.Maps, Options{
		Release: "r1", Host: srv.URL, Token: testToken, HTTPClient: srv.Client(),
		Log: func(l string) { lines = append(lines, l) }, UserAgent: "tlm/test",
	})
	if !reflect.DeepEqual(result, Result{OK: true, Uploaded: []string{"/assets/a.js"}}) {
		t.Errorf("result = %+v", result)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "uploaded /assets/a.js") {
		t.Errorf("lines = %q", lines)
	}
	reqs := srv.received()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	req := reqs[0]
	for _, check := range []struct{ name, got, want string }{
		{"url", req.URL, "/api/sourcemaps"},
		{"authorization", req.Header.Get("Authorization"), "Bearer tlmu_secret"},
		{"accept", req.Header.Get("Accept"), "application/json"},
		{"user agent", req.Header.Get("User-Agent"), "tlm/test"},
		{"release", req.Release, "r1"},
	} {
		if check.got != check.want {
			t.Errorf("%s = %q, want %q", check.name, check.got, check.want)
		}
	}
	if !reflect.DeepEqual(req.Manifest, []manifestEntry{{Path: "/assets/a.js", Map: "a.js.map"}}) {
		t.Errorf("manifest = %+v", req.Manifest)
	}
	var want any
	if err := json.Unmarshal([]byte(testMap("a.js", nil)), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req.Parts, []part{{Name: "a.js.map", Type: "application/gzip", JSON: want}}) {
		t.Errorf("parts = %+v", req.Parts)
	}
	// Uploading leaves the maps where they are.
	if _, err := os.Stat(filepath.Join(dir, "assets", "a.js.map")); err != nil {
		t.Errorf("map gone after upload: %v", err)
	}
}

func TestUploadSendsFlattenedContent(t *testing.T) {
	got := runUpload(t, uploadCase{files: map[string]string{
		"a.js.map": `{"version":3,"sections":[{"offset":{"line":2,"column":0},"map":` + testMap("a.js", nil) + `}]}`,
	}})
	if !got.result.OK || len(got.requests) != 1 || len(got.requests[0].Parts) != 1 {
		t.Fatalf("result = %+v, requests = %+v", got.result, got.requests)
	}
	m, ok := got.requests[0].Parts[0].JSON.(map[string]any)
	if !ok || m["mappings"] != ";;AAAA" || m["sections"] != nil {
		t.Errorf("sent %+v, want the flattened map", got.requests[0].Parts[0].JSON)
	}
}

func TestUploadPartNames(t *testing.T) {
	for _, tc := range []struct {
		name         string
		files        map[string]string
		wantManifest []manifestEntry
		wantNames    []string
	}{
		{
			name:  "flat and unique",
			files: map[string]string{"a/index.js.map": testMap("index.js", nil), "b/index.js.map": testMap("index.js", nil)},
			wantManifest: []manifestEntry{
				{Path: "/a/index.js", Map: "index.js.map"},
				{Path: "/b/index.js", Map: "index.js-2.map"},
			},
			wantNames: []string{"index.js.map", "index.js-2.map"},
		},
		{
			name: "unique even when a file already has a -2 name",
			files: map[string]string{
				"a/x.js.map":   testMap("x.js", nil),
				"b/x.js.map":   testMap("x.js", nil),
				"c/y.js":       "y\n//# sourceMappingURL=x.js-2.map",
				"c/x.js-2.map": testMap("y.js", nil),
			},
			wantManifest: []manifestEntry{
				{Path: "/a/x.js", Map: "x.js.map"},
				{Path: "/b/x.js", Map: "x.js-2.map"},
				{Path: "/c/y.js", Map: "x.js-2-2.map"},
			},
			wantNames: []string{"x.js.map", "x.js-2.map", "x.js-2-2.map"},
		},
		{
			name: "a name without .map gets the number at the end",
			files: map[string]string{
				"a/m.js":   "x\n//# sourceMappingURL=sm",
				"a/sm":     testMap("", nil),
				"b/m.js":   "x\n//# sourceMappingURL=sm.map",
				"b/sm.map": testMap("", nil),
			},
			// Only files ending in .map are found, so "sm" isn't one.
			wantManifest: []manifestEntry{{Path: "/b/m.js", Map: "sm.map"}},
			wantNames:    []string{"sm.map"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runUpload(t, uploadCase{files: tc.files})
			if len(got.requests) != 1 {
				t.Fatalf("%d requests", len(got.requests))
			}
			if !reflect.DeepEqual(got.requests[0].Manifest, tc.wantManifest) {
				t.Errorf("manifest = %+v", got.requests[0].Manifest)
			}
			var names []string
			for _, p := range got.requests[0].Parts {
				names = append(names, p.Name)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Errorf("part names = %q, want %q", names, tc.wantNames)
			}
		})
	}
}

func TestPartNamesWithoutMapSuffix(t *testing.T) {
	got := partNames([]string{"/a/sm", "/b/sm", "/c/sm.map", "/d/sm.map"})
	if want := []string{"sm", "sm-2", "sm.map", "sm-2.map"}; !reflect.DeepEqual(got, want) {
		t.Errorf("partNames = %q, want %q", got, want)
	}
}

func TestUploadBatches(t *testing.T) {
	many := map[string]string{}
	for i := range 101 {
		many[fmt.Sprintf("m%d.js.map", i)] = testMap(fmt.Sprintf("m%d.js", i), nil)
	}
	three := map[string]string{"a.js.map": testMap("a.js", nil), "b.js.map": testMap("b.js", nil), "c.js.map": testMap("c.js", nil)}
	for _, tc := range []struct {
		name         string
		files        map[string]string
		opts         func(*Options)
		wantParts    []int
		wantUploaded int
	}{
		{name: "by count, 100 by default", files: many, wantParts: []int{100, 1}, wantUploaded: 101},
		{name: "by compressed bytes", files: three, opts: func(o *Options) { o.MaxRequestBytes = 10 }, wantParts: []int{1, 1, 1}, wantUploaded: 3},
		{name: "by a smaller count", files: three, opts: func(o *Options) { o.MaxMaps = 2 }, wantParts: []int{2, 1}, wantUploaded: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runUpload(t, uploadCase{files: tc.files, opts: tc.opts})
			var parts []int
			for _, r := range got.requests {
				parts = append(parts, len(r.Parts))
			}
			if !reflect.DeepEqual(parts, tc.wantParts) {
				t.Errorf("parts per request = %v, want %v", parts, tc.wantParts)
			}
			if !got.result.OK || len(got.result.Uploaded) != tc.wantUploaded {
				t.Errorf("result = %+v", got.result)
			}
		})
	}
}

func TestUploadRetries(t *testing.T) {
	failFirst := func(r reply) func(int) reply {
		return func(n int) reply {
			if n == 0 {
				return r
			}
			return reply{status: http.StatusCreated}
		}
	}
	for _, tc := range []struct {
		name         string
		replyTo      func(int) reply
		opts         func(*Options)
		wantOK       bool
		wantRequests int
		wantError    string
		wantSlept    []time.Duration
	}{
		{
			name:         "a 5xx, then success",
			replyTo:      failFirst(reply{status: 503, body: map[string]any{"error": "down"}}),
			wantOK:       true,
			wantRequests: 2,
			wantSlept:    []time.Duration{time.Millisecond},
		},
		{
			name:         "a 408, then success",
			replyTo:      failFirst(reply{status: 408, body: map[string]any{"error": "slow down"}}),
			wantOK:       true,
			wantRequests: 2,
		},
		{
			name:         "a 429, then success",
			replyTo:      failFirst(reply{status: 429, body: map[string]any{"error": "slow down"}}),
			wantOK:       true,
			wantRequests: 2,
		},
		{
			name:         "gives up on a 5xx after the retries, backing off",
			replyTo:      func(int) reply { return reply{status: 500, body: map[string]any{"error": "boom"}} },
			opts:         func(o *Options) { o.Retries = 2 },
			wantRequests: 3,
			wantError:    "1 map(s) not uploaded: HTTP 500: boom, after 3 attempts",
			wantSlept:    []time.Duration{time.Millisecond, 2 * time.Millisecond},
		},
		{
			name:         "three retries by default",
			replyTo:      func(int) reply { return reply{status: 502, raw: "<html>bad gateway</html>"} },
			wantRequests: 4,
			wantError:    "1 map(s) not uploaded: HTTP 502, after 4 attempts",
		},
		{
			name:         "a negative Retries means one attempt",
			replyTo:      func(int) reply { return reply{status: 500} },
			opts:         func(o *Options) { o.Retries = -1 },
			wantRequests: 1,
			wantError:    "after 1 attempts",
		},
		{
			name:         "a network error is retried, then reported naming the host",
			replyTo:      func(int) reply { return reply{hangUp: true} },
			opts:         func(o *Options) { o.Retries = 1 },
			wantRequests: 2,
			wantError:    "couldn't reach http://127.0.0.1:",
		},
		{
			name:         "a server that never answers times out and is retried",
			replyTo:      func(int) reply { return reply{stall: true} },
			opts:         func(o *Options) { o.Retries = 1; o.Timeout = 100 * time.Millisecond },
			wantRequests: 2,
			wantError:    "timed out (no answer within 100ms), after 2 attempts",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runUpload(t, uploadCase{files: oneMap(), replyTo: tc.replyTo, opts: tc.opts})
			if got.result.OK != tc.wantOK {
				t.Errorf("ok = %v, errors = %q", got.result.OK, got.result.Errors)
			}
			if len(got.requests) != tc.wantRequests {
				t.Errorf("%d requests, want %d", len(got.requests), tc.wantRequests)
			}
			if !strings.Contains(allErrors(got.result), tc.wantError) {
				t.Errorf("errors = %q, want %q", got.result.Errors, tc.wantError)
			}
			if !tc.wantOK && len(got.result.Uploaded) != 0 {
				t.Errorf("uploaded = %q", got.result.Uploaded)
			}
			if tc.wantSlept != nil && !reflect.DeepEqual(got.slept, tc.wantSlept) {
				t.Errorf("slept %v, want %v", got.slept, tc.wantSlept)
			}
		})
	}
}

func TestUploadUnreachableHost(t *testing.T) {
	srv := startServer(t, nil)
	host := srv.URL
	srv.Close()
	result := Upload(context.Background(), mapsIn(t, oneMap()), Options{Release: "r1", Host: host, Token: testToken, Retries: -1})
	if result.OK || !strings.Contains(allErrors(result), "couldn't reach "+host+" (") {
		t.Errorf("result = %+v", result)
	}
}

func TestUploadRefusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reply     reply
		maps      int
		wantError string
		wantSent  int
	}{
		{
			name:      "401 stops at once with a token message",
			reply:     reply{status: 401, body: map[string]any{"error": "invalid token"}},
			maps:      2,
			wantError: "1 map(s) not uploaded: Tlm rejected the upload token (HTTP 401: invalid token). Check TLM_UPLOAD_TOKEN: it must be an upload token (tlmu_…) from this project's settings, not the DSN's key.",
			wantSent:  1,
		},
		{
			name:      "403 stops at once with a token message",
			reply:     reply{status: 403, body: map[string]any{"error": "invalid token"}},
			maps:      2,
			wantError: "TLM_UPLOAD_TOKEN",
			wantSent:  1,
		},
		{
			name:      "a 400 reports the server's error text and goes on",
			reply:     reply{status: 400, body: map[string]any{"error": "bad manifest"}},
			maps:      2,
			wantError: "1 map(s) not uploaded: HTTP 400: bad manifest",
			wantSent:  2,
		},
		{
			name:      "a 413 reports the server's error text and goes on",
			reply:     reply{status: 413, body: map[string]any{"error": "a.js.map is over 20 MB"}},
			maps:      2,
			wantError: "1 map(s) not uploaded: HTTP 413: a.js.map is over 20 MB",
			wantSent:  2,
		},
		{
			name:      "an error that isn't text is just the status",
			reply:     reply{status: 400, body: map[string]any{"error": map[string]any{"message": "nope"}}},
			maps:      1,
			wantError: "1 map(s) not uploaded: HTTP 400",
			wantSent:  1,
		},
		{
			name:      "server text is neutralized",
			reply:     reply{status: 400, body: map[string]any{"error": "bad\x1b[31m\nred\u009b"}},
			maps:      1,
			wantError: "HTTP 400: bad�[31m⏎red�",
			wantSent:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for i := range tc.maps {
				files[fmt.Sprintf("m%d.js.map", i)] = testMap("", nil)
			}
			got := runUpload(t, uploadCase{
				files:   files,
				replyTo: func(int) reply { return tc.reply },
				opts:    func(o *Options) { o.MaxMaps = 1 },
			})
			if got.result.OK {
				t.Errorf("ok = true")
			}
			if len(got.requests) != tc.wantSent {
				t.Errorf("%d requests, want %d (no retries)", len(got.requests), tc.wantSent)
			}
			if !strings.Contains(allErrors(got.result), tc.wantError) {
				t.Errorf("errors = %q\nwant %q", got.result.Errors, tc.wantError)
			}
		})
	}
}

func TestUploadOneFailedBatchKeepsTheOthersListed(t *testing.T) {
	got := runUpload(t, uploadCase{
		files: map[string]string{"a.js.map": testMap("a.js", nil), "b.js.map": testMap("b.js", nil)},
		replyTo: func(n int) reply {
			if n == 1 {
				return reply{status: 413, body: map[string]any{"error": "request too large"}}
			}
			return reply{status: 201}
		},
		opts: func(o *Options) { o.MaxMaps = 1 },
	})
	if got.result.OK || !reflect.DeepEqual(got.result.Uploaded, []string{"/a.js"}) {
		t.Errorf("result = %+v", got.result)
	}
	if !strings.Contains(allErrors(got.result), "request too large") {
		t.Errorf("errors = %q", got.result.Errors)
	}
}

func TestUploadALaterBatchFailing(t *testing.T) {
	// A caller deletes the maps only when Result.OK, which is false here.
	files := map[string]string{}
	for i := range 101 {
		files[fmt.Sprintf("m%d.js.map", i)] = testMap(fmt.Sprintf("m%d.js", i), nil)
	}
	got := runUpload(t, uploadCase{files: files, replyTo: func(n int) reply {
		if n == 1 {
			return reply{status: 400, body: map[string]any{"error": "nope"}}
		}
		return reply{status: 201}
	}})
	if got.result.OK || len(got.result.Uploaded) != 100 || len(got.requests) != 2 {
		t.Errorf("ok = %v, uploaded %d, %d requests", got.result.OK, len(got.result.Uploaded), len(got.requests))
	}
}

func TestUploadNeverShowsTheToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply reply
	}{
		{"401 repeating it", reply{status: 401, body: map[string]any{"error": "invalid token " + string(testToken)}}},
		{"400 repeating it", reply{status: 400, body: map[string]any{"error": "bad " + string(testToken)}}},
		{"500", reply{status: 500}},
		{"hang-up", reply{hangUp: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runUpload(t, uploadCase{files: oneMap(), replyTo: func(int) reply { return tc.reply }, opts: func(o *Options) {
				o.Retries = -1
			}})
			shown := strings.Join(append(got.lines, got.result.Errors...), "\n")
			if strings.Contains(shown, string(testToken)) {
				t.Errorf("token shown: %q", shown)
			}
			if got.result.OK {
				t.Errorf("ok = true")
			}
		})
	}
}

func TestUploadTrimsAPastedToken(t *testing.T) {
	got := runUpload(t, uploadCase{files: oneMap(), opts: func(o *Options) { o.Token = " " + testToken + "\n" }})
	if !got.result.OK || len(got.requests) != 1 || got.requests[0].Header.Get("Authorization") != "Bearer tlmu_secret" {
		t.Errorf("result = %+v, requests = %+v", got.result, got.requests)
	}
}

func TestUploadAMapThatDisappeared(t *testing.T) {
	dir := writeTree(t, oneMap())
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "a.js.map")); err != nil {
		t.Fatal(err)
	}
	result := Upload(context.Background(), found.Maps, Options{Release: "r1", Host: "http://127.0.0.1:1", Token: testToken})
	if result.OK || !strings.Contains(allErrors(result), "couldn't read "+filepath.Join(dir, "a.js.map")+":") {
		t.Errorf("result = %+v", result)
	}
}

func TestUploadStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := startServer(t, func(int) reply {
		cancel()
		return reply{status: 503}
	})
	result := Upload(ctx, mapsIn(t, map[string]string{"a.js.map": testMap("", nil), "b.js.map": testMap("", nil)}), Options{
		Release: "r1", Host: srv.URL, Token: testToken, HTTPClient: srv.Client(), MaxMaps: 1,
	})
	if result.OK || len(srv.received()) != 1 {
		t.Errorf("result = %+v, %d requests", result, len(srv.received()))
	}
	if !slices.ContainsFunc(result.Errors, func(e string) bool { return strings.Contains(e, "canceled") }) {
		t.Errorf("errors = %q", result.Errors)
	}
}

func TestUploadDefaultSleepHonoursTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestHostFromDSN(t *testing.T) {
	for _, tc := range []struct{ dsn, want string }{
		{"http://tlm_abc@localhost:4000", "http://localhost:4000"},
		{"https://tlm_abc@tlm.hullop.com", "https://tlm.hullop.com"},
		{"https://tlm_abc@Tlm.Hullop.COM:443/tlm?x#y", "https://tlm.hullop.com"},
		{"", DefaultHost},
	} {
		got, err := HostFromDSN(tc.dsn)
		if err != nil || got != tc.want {
			t.Errorf("HostFromDSN(%q) = %q, %v; want %q", tc.dsn, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ dsn, want string }{
		{"not a dsn", "invalid DSN: expected something like https://tlm_…@tlm.hullop.com (copy it from your project's setup page)."},
		{"http://", "invalid DSN: expected something like"},
		{"ftp://x@y", "invalid DSN: it must start with https:// (or http:// for a local Tlm)."},
	} {
		_, err := HostFromDSN(tc.dsn)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("HostFromDSN(%q) err = %v, want a usage error", tc.dsn, err)
			continue
		}
		var ue *UsageError
		if !errors.As(err, &ue) || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("HostFromDSN(%q) err = %q, want %q", tc.dsn, err, tc.want)
		}
	}
}

func TestTokenRedactsItself(t *testing.T) {
	tok := Token("tlmu_hidden")
	for _, s := range []string{
		fmt.Sprint(tok), fmt.Sprintf("%s %v %q %#v %x %d", tok, tok, tok, tok, tok, tok), tok.String(),
		fmt.Sprintf("%+v", Options{Token: tok}), fmt.Sprintf("%#v", Options{Token: tok}),
	} {
		if strings.Contains(s, "hidden") || strings.Contains(s, "68696464656e") {
			t.Errorf("token shown: %q", s)
		}
	}
	b, err := json.Marshal(struct{ T Token }{tok})
	if err != nil || strings.Contains(string(b), "hidden") {
		t.Errorf("json = %s, %v", b, err)
	}
}

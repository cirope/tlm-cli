package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const testToken Token = "tlmp_t0ken" //nolint:gosec // A fake token.

// call is one request the fake server saw.
type call struct {
	Method, Path, Query string
	Header              http.Header
	Body                string
}

// answer is one canned response. A nil answer closes the connection.
type answer struct {
	status  int
	body    string
	headers map[string]string
}

// fake is a server that answers each "METHOD /path" (under /api/v1)
// with its answers in turn, repeating the last.
type fake struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string][]*answer
	calls  []call
}

func newFake(t *testing.T, routes map[string][]*answer) *fake {
	t.Helper()
	f := &fake{t: t, routes: routes}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1")
	f.mu.Lock()
	f.calls = append(f.calls, call{r.Method, path, r.URL.RawQuery, r.Header.Clone(), string(body)})
	key := r.Method + " " + path
	list := f.routes[key]
	var a *answer
	if len(list) > 0 {
		a = list[0]
		if len(list) > 1 {
			f.routes[key] = list[1:]
		}
	}
	f.mu.Unlock()
	if list == nil {
		f.t.Errorf("unexpected %s", key)
		http.Error(w, "no route", http.StatusTeapot)
		return
	}
	if a == nil {
		hj, ok := w.(http.Hijacker)
		if !ok {
			f.t.Fatal("cannot hijack")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return
	}
	for k, v := range a.headers {
		w.Header().Set(k, strings.ReplaceAll(v, "$URL", f.srv.URL))
	}
	if w.Header().Get("Content-Type") == "" && a.body != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(a.status)
	_, _ = io.WriteString(w, a.body)
}

func (f *fake) client(t *testing.T, mod ...func(*Options)) (*Client, *[]time.Duration) {
	t.Helper()
	var sleeps []time.Duration
	o := Options{
		URL:       f.srv.URL,
		Token:     testToken,
		UserAgent: "tlm-cli/1.2.3 (test; test)",
		// No reused connections: Go's transport would retry a GET on a
		// reused connection that closed, hiding the network errors tested.
		HTTPClient: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}},
		Sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
	}
	for _, m := range mod {
		m(&o)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, &sleeps
}

func ok(body string) *answer { return &answer{status: 200, body: body} }

func v1Error(status int, code, message string) *answer {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": message}})
	return &answer{status: status, body: string(b)}
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v (%T), want *Error", err, err)
	}
	return e
}

func TestHeaders(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /me": {ok(`{"ok":true}`)}})
	c, _ := f.client(t)
	var out map[string]bool
	if _, err := c.Get(context.Background(), "/me", nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out["ok"] {
		t.Errorf("out = %v", out)
	}
	h := f.calls[0].Header
	if got := h.Get("Authorization"); got != "Bearer "+string(testToken) {
		t.Errorf("Authorization = %q", got)
	}
	if got := h.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if got := h.Get("User-Agent"); got != "tlm-cli/1.2.3 (test; test)" {
		t.Errorf("User-Agent = %q", got)
	}
	if _, set := h["Accept-Language"]; set {
		t.Error("Accept-Language sent without --lang")
	}
}

func TestLangSendsAcceptLanguage(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /me": {ok(`{}`)}})
	c, _ := f.client(t, func(o *Options) { o.Lang = "es" })
	if _, err := c.Get(context.Background(), "/me", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[0].Header.Get("Accept-Language"); got != "es" {
		t.Errorf("Accept-Language = %q", got)
	}
}

func TestMarkdownAndEmptyQueryValues(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /x": {{status: 200, body: "# Hi", headers: map[string]string{"Content-Type": "text/markdown; charset=utf-8"}}}})
	c, _ := f.client(t)
	got, err := c.Markdown(context.Background(), "/x", Query{"a": "1", "b": ""})
	if err != nil {
		t.Fatal(err)
	}
	if got != "# Hi" {
		t.Errorf("got %q", got)
	}
	if f.calls[0].Query != "a=1" {
		t.Errorf("query = %q", f.calls[0].Query)
	}
	if got := f.calls[0].Header.Get("Accept"); got != "text/markdown" {
		t.Errorf("Accept = %q", got)
	}
}

func TestErrorShape(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /orgs/a/projects/p": {v1Error(404, "not_found", "Ese proyecto no existe.")}})
	c, _ := f.client(t)
	_, err := c.Get(context.Background(), "/orgs/a/projects/p", nil, nil)
	e := asError(t, err)
	if e.Status != 404 || e.Code != "not_found" || e.Message != "Ese proyecto no existe." {
		t.Errorf("error = %+v", e)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Error("not ErrNotFound")
	}
}

func TestKeepsFields(t *testing.T) {
	f := newFake(t, map[string][]*answer{"POST /orgs/a/projects": {{status: 422, body: `{"error":{"code":"invalid","message":"Fix the fields.","fields":{"name":["can't be blank"]}}}`}}})
	c, _ := f.client(t)
	_, err := c.Send(context.Background(), http.MethodPost, "/orgs/a/projects", nil, map[string]string{"name": ""}, nil)
	e := asError(t, err)
	if diff := cmp.Diff(map[string][]string{"name": {"can't be blank"}}, e.Fields); diff != "" {
		t.Error(diff)
	}
}

func TestNonJSONErrorGivesStatusLine(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /me": {{status: 502, body: "<html>bad</html>", headers: map[string]string{"Content-Type": "text/html"}}}})
	c, _ := f.client(t, func(o *Options) { o.MaxRetries = -1 })
	_, err := c.Get(context.Background(), "/me", nil, nil)
	e := asError(t, err)
	if e.Status != 502 || e.Code != "http_error" || e.Message != "HTTP 502 Bad Gateway" {
		t.Errorf("error = %+v", e)
	}
}

func TestFollows308KeepingMethodAndBody(t *testing.T) {
	f := newFake(t, map[string][]*answer{
		"PATCH /orgs/o/projects/p/issues/A": {{status: 308, headers: map[string]string{"Location": "$URL/api/v1/orgs/o/projects/p/issues/B"}}},
		"PATCH /orgs/o/projects/p/issues/B": {ok(`{"id":"B"}`)},
	})
	c, _ := f.client(t)
	var out struct{ ID string }
	if _, err := c.Send(context.Background(), http.MethodPatch, "/orgs/o/projects/p/issues/A", nil, map[string]string{"status": "resolved"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "B" {
		t.Errorf("id = %q", out.ID)
	}
	var got []string
	for _, c := range f.calls {
		got = append(got, c.Method+" "+c.Path+" "+c.Body)
	}
	want := []string{
		`PATCH /orgs/o/projects/p/issues/A {"status":"resolved"}`,
		`PATCH /orgs/o/projects/p/issues/B {"status":"resolved"}`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
}

func TestRedirectLoopStopsAfterFive(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /issues/X": {{status: 308, headers: map[string]string{"Location": "/api/v1/issues/X"}}}})
	c, _ := f.client(t)
	_, err := c.Get(context.Background(), "/issues/X", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("error = %v", err)
	}
	if len(f.calls) != 6 {
		t.Errorf("calls = %d, want 6", len(f.calls))
	}
}

func TestRedirectToAnotherOriginIsRefused(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /issues/X": {{status: 308, headers: map[string]string{"Location": "https://evil.test/api/v1/issues/X"}}}})
	c, _ := f.client(t)
	_, err := c.Get(context.Background(), "/issues/X", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "redirected to another site") {
		t.Fatalf("error = %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(f.calls))
	}
}

func TestRateLimitWaitsRetryAfterOnce(t *testing.T) {
	limited := func(after string) *answer {
		a := v1Error(429, "rate_limited", "Slow down.")
		a.headers = map[string]string{"Retry-After": after}
		return a
	}
	t.Run("waits", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /me": {limited("2"), ok(`{}`)}})
		c, sleeps := f.client(t)
		if _, err := c.Get(context.Background(), "/me", nil, nil); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]time.Duration{2 * time.Second}, *sleeps); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("capped at 60 s", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /me": {limited("600"), ok(`{}`)}})
		c, sleeps := f.client(t)
		if _, err := c.Get(context.Background(), "/me", nil, nil); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]time.Duration{60 * time.Second}, *sleeps); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("only once, also for writes", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"POST /x": {limited("3")}})
		c, _ := f.client(t)
		_, err := c.Send(context.Background(), http.MethodPost, "/x", nil, map[string]string{}, nil)
		e := asError(t, err)
		if e.Status != 429 || e.Code != "rate_limited" || e.RetryAfter != 3*time.Second {
			t.Errorf("error = %+v", e)
		}
		if len(f.calls) != 2 {
			t.Errorf("calls = %d, want 2", len(f.calls))
		}
	})
}

func TestGetsRetryServerAndNetworkErrorsTwice(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /me": {{status: 500}, nil, ok(`{"ok":1}`)}})
	c, sleeps := f.client(t)
	var out map[string]int
	if _, err := c.Get(context.Background(), "/me", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != 1 {
		t.Errorf("out = %v", out)
	}
	if diff := cmp.Diff([]time.Duration{500 * time.Millisecond, time.Second}, *sleeps); diff != "" {
		t.Error(diff)
	}
}

func TestWritesDontRetryServerErrors(t *testing.T) {
	f := newFake(t, map[string][]*answer{"PATCH /x": {{status: 500}}})
	c, _ := f.client(t)
	_, err := c.Send(context.Background(), http.MethodPatch, "/x", nil, map[string]string{}, nil)
	if e := asError(t, err); e.Status != 500 {
		t.Errorf("status = %d", e.Status)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(f.calls))
	}
}

func TestNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c, err := New(Options{URL: url, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), http.MethodPost, "/x", nil, map[string]string{}, nil)
	e := asError(t, err)
	if e.Status != 0 || e.Code != "network" {
		t.Errorf("error = %+v", e)
	}
	if !strings.HasPrefix(e.Message, "couldn't reach "+url+" (") {
		t.Errorf("message = %q", e.Message)
	}
	if !errors.Is(err, ErrNetwork) {
		t.Error("not ErrNetwork")
	}
}

func TestTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	c, err := New(Options{URL: srv.URL, Token: testToken, Timeout: 20 * time.Millisecond, MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(context.Background(), "/me", nil, nil)
	e := asError(t, err)
	if e.Code != "network" || e.Message != srv.URL+" didn't answer in time" {
		t.Errorf("error = %+v", e)
	}
}

func TestNoContent(t *testing.T) {
	f := newFake(t, map[string][]*answer{"DELETE /me/token": {{status: 204}}})
	c, _ := f.client(t)
	raw, err := c.Send(context.Background(), http.MethodDelete, "/me/token", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Errorf("raw = %q", raw)
	}
}

func TestTokenNeverInErrors(t *testing.T) {
	f := newFake(t, map[string][]*answer{"GET /me": {v1Error(401, "unauthorized", "bad token Bearer "+string(testToken))}})
	c, _ := f.client(t)
	_, err := c.Get(context.Background(), "/me", nil, nil)
	if got := err.Error(); got != "bad token Bearer [redacted]" {
		t.Errorf("message = %q", got)
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Error("not ErrUnauthorized")
	}
}

func TestBadURL(t *testing.T) {
	for _, u := range []string{"", "ftp://x", "tlm.hullop.com", "https://"} {
		if _, err := New(Options{URL: u}); err == nil {
			t.Errorf("New(%q) succeeded", u)
		}
	}
}

func TestOriginOf(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://tlm.hullop.com", "https://tlm.hullop.com"},
		{"https://tlm.hullop.com/", "https://tlm.hullop.com"},
		{"http://localhost:4000/o/x", "http://localhost:4000"},
		{"HTTPS://Tlm.Hullop.Com", "https://tlm.hullop.com"},
	}
	for _, tt := range tests {
		got, err := OriginOf(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("OriginOf(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"nope", "ftp://x", "", "https://"} {
		if _, err := OriginOf(bad); err == nil {
			t.Errorf("OriginOf(%q) succeeded", bad)
		}
	}
}

func TestPages(t *testing.T) {
	t.Run("one page keeps its cursor", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /l": {ok(`{"data":[1],"next_cursor":"c1"}`)}})
		c, _ := f.client(t)
		p, err := List[int](context.Background(), c, "/l", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]int{1}, p.Items); diff != "" {
			t.Error(diff)
		}
		if p.NextCursor != "c1" {
			t.Errorf("cursor = %q", p.NextCursor)
		}
		if got := string(p.JSON(false)); got != `{"data":[1],"next_cursor":"c1"}` {
			t.Errorf("json = %s", got)
		}
	})
	t.Run("all concatenates", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /l": {ok(`{"data":[1,2],"next_cursor":"c1"}`), ok(`{"data":[3],"next_cursor":null}`)}})
		c, _ := f.client(t)
		p, err := List[int](context.Background(), c, "/l", Query{"limit": "2"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]int{1, 2, 3}, p.Items); diff != "" {
			t.Error(diff)
		}
		if p.NextCursor != "" {
			t.Errorf("cursor = %q", p.NextCursor)
		}
		if f.calls[1].Query != "cursor=c1&limit=2" {
			t.Errorf("query = %q", f.calls[1].Query)
		}
		if got := string(p.JSON(true)); got != `{"data":[1,2,3]}` {
			t.Errorf("json = %s", got)
		}
	})
	t.Run("a missing next_cursor ends", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /l": {ok(`{"data":[1],"next_cursor":"c1"}`), ok(`{"data":[2]}`)}})
		c, _ := f.client(t)
		p, err := List[int](context.Background(), c, "/l", nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]int{1, 2}, p.Items); diff != "" {
			t.Error(diff)
		}
		if len(f.calls) != 2 {
			t.Errorf("calls = %d", len(f.calls))
		}
	})
	t.Run("a repeated cursor is refused", func(t *testing.T) {
		f := newFake(t, map[string][]*answer{"GET /l": {ok(`{"data":[1],"next_cursor":"same"}`)}})
		c, _ := f.client(t)
		_, err := List[int](context.Background(), c, "/l", nil, true)
		if err == nil || !strings.Contains(err.Error(), "same page cursor twice") {
			t.Fatalf("error = %v", err)
		}
		if len(f.calls) > 3 {
			t.Errorf("calls = %d", len(f.calls))
		}
	})
}

func TestTokenRedacts(t *testing.T) {
	tok := Token("tlmp_secret")
	for _, s := range []string{
		fmt.Sprint(tok), fmt.Sprintf("%v %s %q %x %#v %+v", tok, tok, tok, tok, tok, tok),
	} {
		if strings.Contains(s, "secret") {
			t.Errorf("leaked: %s", s)
		}
	}
	b, _ := json.Marshal(struct{ T Token }{tok})
	if strings.Contains(string(b), "secret") {
		t.Errorf("leaked in JSON: %s", b)
	}
	if tok.Reveal() != "tlmp_secret" {
		t.Error("Reveal")
	}
}

func TestFullName(t *testing.T) {
	for _, tt := range []struct {
		u    User
		want string
	}{
		{User{Name: "Ada", LastName: "Lovelace"}, "Ada Lovelace"},
		{User{Name: "Ada"}, "Ada"},
		{User{LastName: "Lovelace"}, "Lovelace"},
		{User{}, ""},
	} {
		if got := tt.u.FullName(); got != tt.want {
			t.Errorf("%+v.FullName() = %q, want %q", tt.u, got, tt.want)
		}
	}
}

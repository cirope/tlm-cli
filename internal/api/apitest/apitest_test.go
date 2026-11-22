package apitest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Every fixture parses, names a request under /api, and never holds
// anything that looks like a real token.
func TestFixturesAreWellFormed(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no fixtures")
	}
	for _, n := range names {
		f := Load(t, n)
		if f.Request.Method == "" || !strings.HasPrefix(f.Request.Path, "/api/") {
			t.Errorf("%s: bad request %s %s", n, f.Request.Method, f.Request.Path)
		}
		if f.Response.Status == 0 {
			t.Errorf("%s: no status", n)
		}
		b, _ := json.Marshal(f)
		for _, prefix := range []string{"tlmp_", "tlmu_"} {
			for _, part := range strings.Split(string(b), prefix)[1:] {
				if !strings.HasPrefix(part, "test") {
					t.Errorf("%s: a %s value that isn't an obvious fake", n, prefix)
				}
			}
		}
	}
}

func TestServerAnswersInOrderAndRepeatsTheLast(t *testing.T) {
	s := NewServer(t, "login.token.pending", "login.token.ok")
	for _, want := range []int{400, 200, 200} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+"/api/v1/login/token", strings.NewReader(`{"device_code":"dc_test_device_code"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("status = %d, want %d", resp.StatusCode, want)
		}
	}
}

func TestServerRewritesTheRecordedOrigin(t *testing.T) {
	s := NewServer(t, "project.get.ok")
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL+"/api/v1/orgs/acme/projects/shop", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(b), recordedOrigin) || !strings.Contains(string(b), s.URL+"/projects/shop?org=") {
		t.Errorf("body = %s", b)
	}
}

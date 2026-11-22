package sourcemaps

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A local stand-in for Tlm's POST /api/sourcemaps.

type part struct {
	Name string
	Type string
	JSON any
}

type manifestEntry struct {
	Path string `json:"path"`
	Map  string `json:"map"`
}

type received struct {
	URL      string
	Header   http.Header
	Release  string
	Manifest []manifestEntry
	Parts    []part
}

// reply is what the server does with request n (0-based).
type reply struct {
	status int
	body   any    // JSON; nil echoes {release, files}
	raw    string // a non-JSON body, when set
	hangUp bool   // close the connection without answering
	stall  bool   // never answer
}

type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []received
}

func (s *fakeServer) received() []received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]received(nil), s.requests...)
}

func startServer(t *testing.T, replyTo func(n int) reply) *fakeServer {
	t.Helper()
	if replyTo == nil {
		replyTo = func(int) reply { return reply{status: http.StatusCreated} }
	}
	s := &fakeServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := received{URL: r.URL.Path, Header: r.Header.Clone()}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("not multipart: %v", err)
			http.Error(w, "not multipart", http.StatusBadRequest)
			return
		}
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Errorf("reading parts: %v", err)
				return
			}
			data, err := io.ReadAll(p)
			if err != nil {
				t.Errorf("reading part: %v", err)
				return
			}
			switch {
			case p.FileName() == "" && p.FormName() == "release":
				got.Release = string(data)
			case p.FileName() == "" && p.FormName() == "manifest":
				if err := json.Unmarshal(data, &got.Manifest); err != nil {
					t.Errorf("manifest: %v", err)
				}
			default:
				typ := p.Header.Get("Content-Type")
				if typ == "application/gzip" {
					zr, err := gzip.NewReader(bytes.NewReader(data))
					if err != nil {
						t.Errorf("gzip: %v", err)
						return
					}
					if data, err = io.ReadAll(zr); err != nil {
						t.Errorf("gunzip: %v", err)
						return
					}
				}
				var v any
				if err := json.Unmarshal(data, &v); err != nil {
					t.Errorf("part %s isn't JSON: %v", p.FileName(), err)
				}
				if p.FormName() != p.FileName() {
					t.Errorf("part named %q has file name %q", p.FormName(), p.FileName())
				}
				got.Parts = append(got.Parts, part{Name: p.FileName(), Type: typ, JSON: v})
			}
		}
		s.mu.Lock()
		s.requests = append(s.requests, got)
		n := len(s.requests) - 1
		s.mu.Unlock()

		rep := replyTo(n)
		switch {
		case rep.stall:
			<-r.Context().Done()
			return
		case rep.hangUp:
			conn, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rep.status)
		if rep.raw != "" {
			_, _ = io.WriteString(w, rep.raw)
			return
		}
		body := rep.body
		if body == nil {
			files := []map[string]string{}
			for _, m := range got.Manifest {
				files = append(files, map[string]string{"path": m.Path})
			}
			body = map[string]any{"release": got.Release, "files": files}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.Close)
	return s
}

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cirope/tlm-cli/internal/api"
)

// fakeKeyring is a keychain in memory; broken makes every call fail, as
// on a machine without one.
type fakeKeyring struct {
	items  map[string]string
	broken bool
}

func newKeyring() *fakeKeyring { return &fakeKeyring{items: map[string]string{}} }

var errNoKeychain = errors.New("no keychain")

func (k *fakeKeyring) Get(service, user string) (string, error) {
	if k.broken {
		return "", errNoKeychain
	}
	v, ok := k.items[service+"/"+user]
	if !ok {
		return "", ErrKeyringNotFound
	}
	return v, nil
}

func (k *fakeKeyring) Set(service, user, secret string) error {
	if k.broken {
		return errNoKeychain
	}
	k.items[service+"/"+user] = secret
	return nil
}

func (k *fakeKeyring) Delete(service, user string) error {
	if k.broken {
		return errNoKeychain
	}
	delete(k.items, service+"/"+user)
	return nil
}

const u1, u2 = "https://tlm.test", "http://localhost:4000"

func TestFileStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tlm")
	f := &FileStore{Path: filepath.Join(dir, "tokens.json")}
	if _, err := f.Get(u1); !errors.Is(err, ErrNoToken) {
		t.Fatalf("empty Get = %v, want ErrNoToken", err)
	}
	if err := f.Set(u1, "tlmp_one"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(u2, "tlmp_two"); err != nil {
		t.Fatal(err)
	}
	for u, want := range map[string]api.Token{u1: "tlmp_one", u2: "tlmp_two"} {
		if got, err := f.Get(u); err != nil || got != want {
			t.Errorf("Get(%s): %v", u, err)
		}
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(f.Path); st.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v", st.Mode().Perm())
		}
		if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %v", st.Mode().Perm())
		}
	}
	if err := f.Delete(u2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Get(u2); !errors.Is(err, ErrNoToken) {
		t.Errorf("after Delete: %v", err)
	}
	if tok, err := f.Get(u1); err != nil || tok != "tlmp_one" {
		t.Errorf("other login lost: %v", err)
	}
}

func TestFileStoreDamagedFileNeverQuotesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte(`{"tokens": tlmp_secret`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&FileStore{Path: path}).Get(u1)
	if err == nil || strings.Contains(err.Error(), "tlmp_secret") || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("err = %v", err)
	}
}

func TestFileStoreWarnsWhenOthersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX modes")
	}
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{}}`), 0o644); err != nil { //nolint:gosec // The point of the test.
		t.Fatal(err)
	}
	var warned []string
	f := &FileStore{Path: path, Warn: func(s string) { warned = append(warned, s) }}
	if _, err := f.Get(u1); !errors.Is(err, ErrNoToken) {
		t.Fatal(err)
	}
	if len(warned) == 0 || !strings.Contains(warned[0], "chmod 600 "+path) {
		t.Errorf("warned = %q", warned)
	}
}

func TestStorePrefersTheKeychain(t *testing.T) {
	k := newKeyring()
	file := &FileStore{Path: filepath.Join(t.TempDir(), "tokens.json")}
	if err := file.Set(u1, "tlmp_stale"); err != nil {
		t.Fatal(err)
	}
	s := &Store{Keyring: k, File: file, Notice: func(string) { t.Error("notice with a keychain") }}
	if err := s.Set(u1, "tlmp_new"); err != nil {
		t.Fatal(err)
	}
	if k.items["tlm-cli/"+u1] != "tlmp_new" {
		t.Error("not in the keychain")
	}
	if _, err := file.Get(u1); !errors.Is(err, ErrNoToken) {
		t.Error("the stale file copy lingers")
	}
	if tok, _ := s.Get(u1); tok != "tlmp_new" {
		t.Error("Get")
	}
	if err := s.Delete(u1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(u1); !errors.Is(err, ErrNoToken) {
		t.Errorf("after Delete: %v", err)
	}
}

func TestStoreFallsBackToTheFile(t *testing.T) {
	k := newKeyring()
	k.broken = true
	path := filepath.Join(t.TempDir(), "tokens.json")
	var notices []string
	s := &Store{Keyring: k, File: &FileStore{Path: path}, Notice: func(p string) { notices = append(notices, p) }}
	for _, tok := range []api.Token{"tlmp_a", "tlmp_b"} {
		if err := s.Set(u1, tok); err != nil {
			t.Fatal(err)
		}
	}
	if tok, err := s.Get(u1); err != nil || tok != "tlmp_b" {
		t.Errorf("Get = %v", err)
	}
	if len(notices) != 1 || notices[0] != path {
		t.Errorf("notices = %q, want one, for the first write", notices)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tlm", "config.json")
	c, err := LoadConfig(path)
	if err != nil || c.URL != "" || c.Defaults != nil {
		t.Fatalf("missing config = %+v, %v", c, err)
	}
	want := Config{URL: u2, Defaults: map[string]Default{u2: {Org: "acme", Project: "shop"}}}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadConfig(path); got.URL != want.URL || got.Defaults[u2] != want.Defaults[u2] {
		t.Errorf("got %+v", got)
	}
}

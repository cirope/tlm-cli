package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/zalando/go-keyring"
)

// ErrNoToken means no token is stored for the Tlm server.
var ErrNoToken = errors.New("not signed in")

// ErrKeyringNotFound is what a Keyring returns for a missing item.
var ErrKeyringNotFound = keyring.ErrNotFound

const keyringService = "tlm-cli"

// TokenStore keeps one token per Tlm URL.
type TokenStore interface {
	Get(url string) (api.Token, error)
	Set(url string, token api.Token) error
	Delete(url string) error
}

// Keyring is the operating system's keychain.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

// SystemKeyring is the real keychain: macOS Keychain, Windows Credential
// Manager or the Secret Service on Linux.
type SystemKeyring struct{}

// Get implements Keyring.
func (SystemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

// Set implements Keyring.
func (SystemKeyring) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

// Delete implements Keyring.
func (SystemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// Store keeps tokens in the keychain and, where there is none (a
// container, CI), in File. Notice is called with the file's path the
// first time a token is written there.
type Store struct {
	Keyring Keyring
	File    *FileStore
	Notice  func(path string)
}

// Get implements TokenStore.
func (s *Store) Get(url string) (api.Token, error) {
	if s.Keyring != nil {
		if v, err := s.Keyring.Get(keyringService, url); err == nil && v != "" {
			return api.Token(v), nil
		}
	}
	return s.File.Get(url)
}

// Set implements TokenStore.
func (s *Store) Set(url string, token api.Token) error {
	if s.Keyring != nil {
		if err := s.Keyring.Set(keyringService, url, token.Reveal()); err == nil {
			return s.File.Delete(url) // An older file copy must not linger.
		}
	}
	_, statErr := os.Stat(s.File.Path)
	if err := s.File.Set(url, token); err != nil {
		return err
	}
	if errors.Is(statErr, fs.ErrNotExist) && s.Notice != nil {
		s.Notice(s.File.Path)
	}
	return nil
}

// Delete implements TokenStore.
func (s *Store) Delete(url string) error {
	if s.Keyring != nil {
		_ = s.Keyring.Delete(keyringService, url) // Missing, or no keychain: the file is what's left.
	}
	return s.File.Delete(url)
}

// FileStore keeps tokens in a JSON file only its owner can read.
type FileStore struct {
	Path string
	// Warn is called when the file is readable by others.
	Warn func(string)
	mu   sync.Mutex
}

type credentials struct {
	Tokens map[string]string `json:"tokens"`
}

func (f *FileStore) read() (credentials, error) {
	c := credentials{Tokens: map[string]string{}}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("reading %s: %w", f.Path, err)
	}
	if json.Unmarshal(b, &c) != nil {
		// The decoder's message can quote the file; never show it.
		return c, fmt.Errorf("reading %s: the file is damaged; delete it and run tlm login", f.Path)
	}
	if c.Tokens == nil {
		c.Tokens = map[string]string{}
	}
	if f.Warn != nil && runtime.GOOS != "windows" {
		if st, err := os.Stat(f.Path); err == nil && st.Mode().Perm()&0o077 != 0 {
			f.Warn(fmt.Sprintf("%s is readable by others; run: chmod 600 %s", f.Path, f.Path))
		}
	}
	return c, nil
}

// Get implements TokenStore.
func (f *FileStore) Get(url string) (api.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.read()
	if err != nil {
		return "", err
	}
	t, ok := c.Tokens[url]
	if !ok || t == "" {
		return "", ErrNoToken
	}
	return api.Token(t), nil
}

// Set implements TokenStore.
func (f *FileStore) Set(url string, token api.Token) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.read()
	if err != nil {
		return err
	}
	c.Tokens[url] = token.Reveal()
	return f.write(c)
}

// Delete implements TokenStore. Deleting nothing is not an error.
func (f *FileStore) Delete(url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := c.Tokens[url]; !ok {
		return nil
	}
	delete(c.Tokens, url)
	return f.write(c)
}

func (f *FileStore) write(c credentials) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(f.Path, append(b, '\n'), 0o600)
}

// MemoryStore is a TokenStore for tests.
type MemoryStore struct {
	mu     sync.Mutex
	tokens map[string]api.Token
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{tokens: map[string]api.Token{}} }

// Get implements TokenStore.
func (m *MemoryStore) Get(url string) (api.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[url]
	if !ok {
		return "", ErrNoToken
	}
	return t, nil
}

// Set implements TokenStore.
func (m *MemoryStore) Set(url string, token api.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[url] = token
	return nil
}

// Delete implements TokenStore.
func (m *MemoryStore) Delete(url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, url)
	return nil
}

// Config is tlm's non-secret settings, kept as JSON in
// <config dir>/tlm/config.json.
type Config struct {
	URL string `json:"url,omitempty"` // The Tlm server last signed in to.
	// Defaults are the organization and project the user chose, per
	// Tlm URL.
	Defaults map[string]Default `json:"defaults,omitempty"`
}

// Default is a default organization and, optionally, project.
type Default struct {
	Org     string `json:"org"`
	Project string `json:"project,omitempty"`
}

// LoadConfig reads the config at path; a missing file is an empty config.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path) //nolint:gosec // tlm's own config file.
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("reading %s: %w", path, err)
	}
	return c, nil
}

// Save writes the config to path.
func (c Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o600)
}

// writeFileAtomic writes data to a temporary file beside path and renames
// it into place, creating the directory (0700) if needed.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(perm); err != nil && runtime.GOOS != "windows" {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

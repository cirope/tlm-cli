package sourcemaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTree writes files (slash-separated relative path → content) into a
// fresh temporary directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// testMap is a small valid source map whose `file` is file. extra replaces or
// adds fields; a nil value removes the field.
func testMap(file string, extra map[string]any) string {
	m := map[string]any{
		"version":        3,
		"file":           file,
		"sources":        []string{"../src/main.ts"},
		"sourcesContent": []string{"export function main() {}\n"},
		"names":          []string{"main"},
		"mappings":       "AAAA",
	}
	for k, v := range extra {
		if v == nil {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// mapsIn finds the maps in a fresh tree of files, served at "/".
func mapsIn(t *testing.T, files map[string]string) []Map {
	t.Helper()
	found, err := Find(writeTree(t, files), FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	return found.Maps
}

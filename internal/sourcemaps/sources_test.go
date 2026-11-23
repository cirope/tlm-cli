package sourcemaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// With Sources, scripts that have no map get one line for line, holding
// their code, so a stack trace into them shows it.
func TestFindSourcesMapsPlainScriptsLineForLine(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"controllers/hello_controller-1a2b3c.js": "import { Controller } from \"@hotwired/stimulus\"\n\nexport default class extends Controller {\n}\n",
		"application-4d5e6f.js":                  "import \"controllers\"",
		"bundle.js":                              "x\n//# sourceMappingURL=bundle.js.map",
		"bundle.js.map":                          testMap("bundle.js", nil),
		"styles.css":                             "a{}",
	})
	found, err := Find(dir, FindOptions{URLPrefix: "/assets", Sources: true})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, m := range found.Maps {
		paths = append(paths, m.Path)
	}
	want := []string{"/assets/bundle.js", "/assets/application-4d5e6f.js", "/assets/controllers/hello_controller-1a2b3c.js"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %q, want %q", paths, want)
	}
	if len(found.Warnings) != 0 {
		t.Errorf("warnings = %q", found.Warnings)
	}

	m := found.Maps[2]
	if !m.Generated || m.Content == nil || m.BundleFile != filepath.Join(dir, "controllers", "hello_controller-1a2b3c.js") {
		t.Errorf("map = %+v", m)
	}
	var got struct {
		Version        int      `json:"version"`
		File           string   `json:"file"`
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if err := json.Unmarshal(m.Content, &got); err != nil {
		t.Fatal(err)
	}
	code := "import { Controller } from \"@hotwired/stimulus\"\n\nexport default class extends Controller {\n}\n"
	if got.Version != 3 || got.File != "hello_controller-1a2b3c.js" ||
		!reflect.DeepEqual(got.Sources, []string{"controllers/hello_controller-1a2b3c.js"}) ||
		!reflect.DeepEqual(got.SourcesContent, []string{code}) {
		t.Errorf("map = %+v", got)
	}
	// Line 1 column 0 to line 1; every next line, one line further. The
	// empty line has a segment too, and the final newline adds none.
	if got.Mappings != "AAAA;AACA;AACA;AACA" {
		t.Errorf("mappings = %q", got.Mappings)
	}
	if m.Bytes != int64(len(m.Content)) {
		t.Errorf("bytes = %d, want %d", m.Bytes, len(m.Content))
	}
}

// Without Sources, scripts without a map are left alone, as before.
func TestFindWithoutSourcesSkipsPlainScripts(t *testing.T) {
	maps := mapsIn(t, map[string]string{"app.js": "console.log(1)\n"})
	if len(maps) != 0 {
		t.Errorf("maps = %+v", maps)
	}
}

// A line for line map of minified code points every error at one line:
// such files are skipped, with a warning saying what they need.
func TestFindSourcesSkipsMinifiedScripts(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"vendor/turbo.min.js": "!function(){" + strings.Repeat("var a=1;", 200) + "}()\n",
		"app.js":              "console.log(1)\n",
	})
	found, err := Find(dir, FindOptions{URLPrefix: "/", Sources: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Maps) != 1 || found.Maps[0].Path != "/app.js" {
		t.Errorf("maps = %+v", found.Maps)
	}
	if !reflect.DeepEqual(found.Warnings, []string{"vendor/turbo.min.js: looks minified, so it needs the source map its build makes"}) {
		t.Errorf("warnings = %q", found.Warnings)
	}
}

// Delete leaves made-up maps, and the scripts they were made from, alone.
func TestDeleteSkipsGeneratedMaps(t *testing.T) {
	dir := writeTree(t, map[string]string{"app.js": "console.log(1)\n"})
	found, err := Find(dir, FindOptions{URLPrefix: "/", Sources: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(found.Maps); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "app.js")); err != nil || string(b) != "console.log(1)\n" {
		t.Errorf("app.js = %q, %v", b, err)
	}
}

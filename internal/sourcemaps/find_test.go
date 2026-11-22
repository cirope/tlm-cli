package sourcemaps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePrefix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"static", "/static/"},
		{"/static", "/static/"},
		{"/static/", "/static/"},
		{"", "/"},
		{"/", "/"},
		{"//a//", "/a/"},
	} {
		if got := NormalizePrefix(tc.in); got != tc.want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFindPairsBySourceMappingURL(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"assets/a-1234abcd.js":     "x\n//# sourceMappingURL=a-1234abcd.js.map",
		"assets/a-1234abcd.js.map": testMap("other-name.js", nil),
	})
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", found.Warnings)
	}
	want := []Map{{
		Path:       "/assets/a-1234abcd.js",
		MapFile:    filepath.Join(dir, "assets", "a-1234abcd.js.map"),
		BundleFile: filepath.Join(dir, "assets", "a-1234abcd.js"),
		Bytes:      int64(len(testMap("other-name.js", nil))),
	}}
	if !reflect.DeepEqual(found.Maps, want) {
		t.Errorf("maps = %+v\nwant %+v", found.Maps, want)
	}
}

func TestFindPairing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		files      map[string]string
		mapFile    string
		wantPath   string
		wantBundle string // relative; "" for none
	}{
		{
			name:       "the map's file field",
			files:      map[string]string{"bundle.js": "no comment", "weird.map": testMap("bundle.js", nil)},
			mapFile:    "weird.map",
			wantPath:   "/bundle.js",
			wantBundle: "bundle.js",
		},
		{
			name:       "the name without .map",
			files:      map[string]string{"plain.js": "no comment", "plain.js.map": testMap("", nil)},
			mapFile:    "plain.js.map",
			wantPath:   "/plain.js",
			wantBundle: "plain.js",
		},
		{
			name: "sourceMappingURL wins over the file field",
			files: map[string]string{
				"real.js":  "x\n//# sourceMappingURL=m.js.map\n",
				"other.js": "y",
				"m.js.map": testMap("other.js", nil),
			},
			mapFile:    "m.js.map",
			wantPath:   "/real.js",
			wantBundle: "real.js",
		},
		{
			name: "the //@ form, a query and an escaped name",
			files: map[string]string{
				"js/app.mjs":         "x\n//@ sourceMappingURL=maps/app%20v2.map?v=3#x  \n",
				"js/maps/app v2.map": testMap("", nil),
			},
			mapFile:    "js/maps/app v2.map",
			wantPath:   "/js/app.mjs",
			wantBundle: "js/app.mjs",
		},
		{
			name: "a remote or data: sourceMappingURL is ignored",
			files: map[string]string{
				"a.js":     "x\n//# sourceMappingURL=https://cdn.test/b.js.map",
				"b.js":     "y\n//# sourceMappingURL=data:application/json;base64,e30=",
				"b.js.map": testMap("", nil),
			},
			mapFile:    "b.js.map",
			wantPath:   "/b.js",
			wantBundle: "b.js",
		},
		{
			name:     "a missing bundle still gets its path from the name",
			files:    map[string]string{"gone.cjs.map": testMap("", nil)},
			mapFile:  "gone.cjs.map",
			wantPath: "/gone.cjs",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			found, err := Find(dir, FindOptions{URLPrefix: "/"})
			if err != nil {
				t.Fatal(err)
			}
			mapFile := filepath.Join(dir, filepath.FromSlash(tc.mapFile))
			for _, m := range found.Maps {
				if m.MapFile != mapFile {
					continue
				}
				if m.Path != tc.wantPath {
					t.Errorf("path = %q, want %q", m.Path, tc.wantPath)
				}
				wantBundle := ""
				if tc.wantBundle != "" {
					wantBundle = filepath.Join(dir, filepath.FromSlash(tc.wantBundle))
				}
				if m.BundleFile != wantBundle {
					t.Errorf("bundle = %q, want %q", m.BundleFile, wantBundle)
				}
				return
			}
			t.Fatalf("%s not found in %+v", tc.mapFile, found.Maps)
		})
	}
}

func TestFindURLPrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		prefix string
		want   []string
	}{
		{
			name:   "nested dirs and prefixes build URL paths with /",
			files:  map[string]string{"js/app.js": "x", "js/app.js.map": testMap("app.js", nil)},
			prefix: "static",
			want:   []string{"/static/js/app.js"},
		},
		{
			name: "--url-prefix sets the served path",
			files: map[string]string{
				"assets/a.js":     "x\n//# sourceMappingURL=a.js.map",
				"assets/a.js.map": testMap("a.js", nil),
			},
			prefix: "/static",
			want:   []string{"/static/assets/a.js"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := Find(writeTree(t, tc.files), FindOptions{URLPrefix: tc.prefix})
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(found.Maps); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("paths = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindWarnings(t *testing.T) {
	section := map[string]any{}
	if err := json.Unmarshal([]byte(testMap("a.js", nil)), &section); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		files        map[string]string
		maxBytes     int64
		wantPaths    []string
		wantWarnings []string // each warning must contain every part, in order
	}{
		{
			name: "a map without sourcesContent",
			files: map[string]string{
				"a.js.map": testMap("a.js", map[string]any{"sourcesContent": nil}),
				"b.js.map": testMap("b.js", map[string]any{"sourcesContent": []any{nil}}),
			},
			wantPaths: []string{"/a.js", "/b.js"},
			wantWarnings: []string{
				"a.js.map: has no sourcesContent, so Tlm can't show your code or name every function",
				"b.js.map: has no sourcesContent",
			},
		},
		{
			name:      "no sources means nothing to warn about",
			files:     map[string]string{"a.js.map": testMap("a.js", map[string]any{"sources": []string{}, "sourcesContent": nil})},
			wantPaths: []string{"/a.js"},
		},
		{
			name: "a map over maxMapBytes is skipped",
			files: map[string]string{
				"big.js.map":   testMap("big.js", nil),
				"small.js.map": `{"version":3,"mappings":"","sources":[],"sourcesContent":[]}`,
			},
			maxBytes:     80,
			wantPaths:    []string{"/small.js"},
			wantWarnings: []string{"big.js.map: skipped, it's over 0 MB"},
		},
		{
			name:         "an index map whose sections point to other files is skipped",
			files:        map[string]string{"i.js.map": `{"version":3,"sections":[{"offset":{"line":0,"column":0},"url":"other.map"}]}`},
			wantWarnings: []string{"i.js.map: skipped, an index map whose sections point to other files or nest sections"},
		},
		{
			name:         "an index map with bad mappings is skipped",
			files:        map[string]string{"i.js.map": `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":[],"mappings":"!"}}]}`},
			wantWarnings: []string{"i.js.map: skipped, an index map"},
		},
		{
			name: "a file that isn't a source map is skipped",
			files: map[string]string{
				"broken.js.map": "not json",
				"array.js.map":  "[]",
				"null.js.map":   "null",
			},
			wantWarnings: []string{
				"array.js.map: skipped, it isn't a source map (not JSON)",
				"broken.js.map: skipped, it isn't a source map (not JSON)",
				"null.js.map: skipped, it isn't a source map (not JSON)",
			},
		},
		{
			name: "nested maps are shown relative, with /",
			files: map[string]string{
				"x/y/broken.js.map": "nope",
			},
			wantWarnings: []string{"x/y/broken.js.map: skipped"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := Find(writeTree(t, tc.files), FindOptions{URLPrefix: "/", MaxMapBytes: tc.maxBytes})
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(found.Maps); !reflect.DeepEqual(got, tc.wantPaths) {
				t.Errorf("paths = %q, want %q", got, tc.wantPaths)
			}
			if len(found.Warnings) != len(tc.wantWarnings) {
				t.Fatalf("warnings = %q, want %d", found.Warnings, len(tc.wantWarnings))
			}
			for i, want := range tc.wantWarnings {
				if !strings.HasPrefix(found.Warnings[i], want) {
					t.Errorf("warning %d = %q, want it to start with %q", i, found.Warnings[i], want)
				}
			}
		})
	}
}

func TestFindDefaultLimitIs20MB(t *testing.T) {
	if MaxMapBytes != 20*1024*1024 {
		t.Fatalf("MaxMapBytes = %d", MaxMapBytes)
	}
	dir := writeTree(t, map[string]string{"big.js.map": ""})
	if err := os.Truncate(filepath.Join(dir, "big.js.map"), MaxMapBytes+1); err != nil {
		t.Fatal(err)
	}
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Maps) != 0 || len(found.Warnings) != 1 || found.Warnings[0] != "big.js.map: skipped, it's over 20 MB" {
		t.Errorf("found = %+v", found)
	}
}

func TestFindIgnores(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"node_modules and non-map files", map[string]string{"node_modules/x/y.js.map": testMap("y.js", nil), "a.js": "x", "a.css": "y"}},
		{"maps of anything but scripts", map[string]string{"a.css": "y\n/*# sourceMappingURL=a.css.map */", "a.css.map": testMap("a.css", nil)}},
		{"an empty dir", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := Find(writeTree(t, tc.files), FindOptions{URLPrefix: "/"})
			if err != nil {
				t.Fatal(err)
			}
			if len(found.Maps) != 0 || len(found.Warnings) != 0 {
				t.Errorf("found = %+v, want nothing", found)
			}
		})
	}
}

func TestFindMissingDir(t *testing.T) {
	dir := writeTree(t, map[string]string{"file": "x"})
	for _, missing := range []string{filepath.Join(dir, "nope"), filepath.Join(dir, "file")} {
		_, err := Find(missing, FindOptions{URLPrefix: "/"})
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("Find(%q) error = %v, want one naming it", missing, err)
		}
		if !errors.Is(err, ErrNotDirectory) {
			t.Errorf("Find(%q) error = %v, want ErrNotDirectory", missing, err)
		}
	}
}

func TestFindFileFieldOutsideDirIsIgnored(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.js":      "x\n//# sourceMappingURL=../dist/m.js.map",
		"dist/m.js.map": testMap("../src/a.js", nil),
	})
	found, err := Find(filepath.Join(root, "dist"), FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Maps) != 1 || found.Maps[0].Path != "/m.js" || found.Maps[0].BundleFile != "" {
		t.Errorf("maps = %+v, want /m.js with no bundle", found.Maps)
	}
}

func TestFindFlattensIndexMaps(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.js":     "x\n//# sourceMappingURL=a.js.map",
		"a.js.map": `{"version":3,"file":"a.js","sections":[{"offset":{"line":0,"column":0},"map":` + testMap("a.js", nil) + `}]}`,
	})
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Warnings) != 0 {
		t.Errorf("warnings = %q", found.Warnings)
	}
	if got := paths(found.Maps); !reflect.DeepEqual(got, []string{"/a.js"}) {
		t.Fatalf("paths = %q", got)
	}
	m := found.Maps[0]
	if m.Content == nil || m.Bytes != int64(len(m.Content)) {
		t.Fatalf("content = %q, bytes = %d", m.Content, m.Bytes)
	}
	var flat RawMap
	if err := json.Unmarshal(m.Content, &flat); err != nil {
		t.Fatal(err)
	}
	if flat.Version != 3 || !reflect.DeepEqual(flat.Sources, []string{"../src/main.ts"}) || flat.Mappings != "AAAA" {
		t.Errorf("flat = %+v", flat)
	}
	if m.BundleFile != filepath.Join(dir, "a.js") {
		t.Errorf("bundle = %q", m.BundleFile)
	}
}

func TestFindNestedMapsUseTheirOwnFiles(t *testing.T) {
	// Maps under a nested dir upload from their own files.
	dir := writeTree(t, map[string]string{"x/y.js.map": testMap("y.js", nil)})
	found, err := Find(dir, FindOptions{URLPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Maps) != 1 || found.Maps[0].MapFile != filepath.Join(dir, "x", "y.js.map") {
		t.Errorf("maps = %+v", found.Maps)
	}
}

func TestDelete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		gone   []string
		bundle map[string]string // file → content afterwards
	}{
		{
			name: "removes maps and their comments, leaving other code intact",
			files: map[string]string{
				"a.js":     "x\n//# sourceMappingURL=a.js.map",
				"a.js.map": testMap("a.js", nil),
				"b.css":    "y\n/*# sourceMappingURL=b.css.map */",
			},
			gone:   []string{"a.js.map"},
			bundle: map[string]string{"a.js": "x", "b.css": "y\n/*# sourceMappingURL=b.css.map */"},
		},
		{
			name: "a comment with a directory, the //@ form and trailing space",
			files: map[string]string{
				"a.js":          "x\n//@ sourceMappingURL=maps/a.js.map \n",
				"maps/a.js.map": testMap("../a.js", nil),
			},
			gone:   []string{"maps/a.js.map"},
			bundle: map[string]string{"a.js": "x"},
		},
		{
			name: "a comment naming another map stays",
			files: map[string]string{
				"a.js":     "x\n//# sourceMappingURL=other.js.map",
				"a.js.map": testMap("a.js", nil),
			},
			gone:   []string{"a.js.map"},
			bundle: map[string]string{"a.js": "x\n//# sourceMappingURL=other.js.map"},
		},
		{
			name:  "a map without a bundle",
			files: map[string]string{"a.js.map": testMap("a.js", nil)},
			gone:  []string{"a.js.map"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			found, err := Find(dir, FindOptions{URLPrefix: "/"})
			if err != nil {
				t.Fatal(err)
			}
			if err := Delete(found.Maps); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.gone {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s still there (%v)", name, err)
				}
			}
			for name, want := range tc.bundle {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestDeleteReportsAMissingMap(t *testing.T) {
	err := Delete([]Map{{Path: "/a.js", MapFile: filepath.Join(t.TempDir(), "a.js.map")}})
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func paths(maps []Map) []string {
	var out []string
	for _, m := range maps {
		out = append(out, m.Path)
	}
	return out
}

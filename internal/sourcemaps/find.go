package sourcemaps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// MaxMapBytes is the largest map Tlm accepts, uncompressed. Larger
// maps are skipped with a warning.
const MaxMapBytes = 20 * 1024 * 1024

// ErrNotDirectory is returned by Find when the directory to search isn't
// there or isn't a directory.
var ErrNotDirectory = errors.New("no such directory")

// Map is one source map to upload, and the bundle it belongs to.
type Map struct {
	// Path is the URL path the bundle is served at, e.g. /assets/index-a1b2c3d4.js.
	Path string
	// MapFile is the path of the .map file.
	MapFile string
	// BundleFile is the path of the bundle, or "" when it isn't there.
	BundleFile string
	// Bytes is the size of what will be uploaded, uncompressed.
	Bytes int64
	// Content, when set, is uploaded instead of MapFile: an index map,
	// flattened, or a map made from the bundle itself.
	Content []byte
	// Generated is a map made from the bundle, line for line: there's no
	// MapFile to delete.
	Generated bool
}

// FindOptions says where a directory is served and which maps to skip.
type FindOptions struct {
	// URLPrefix is the URL path the directory is served at ("" or "/" for
	// the root; "static", "/static" and "/static/" are the same).
	URLPrefix string
	// MaxMapBytes skips larger maps with a warning; 0 means MaxMapBytes.
	MaxMapBytes int64
	// Sources maps scripts that have no map of their own line for line,
	// for apps that serve their code as written.
	Sources bool
}

// Found is what Find found: the maps to upload, and warnings about the
// ones skipped or incomplete. Each warning names a map by its path
// relative to the directory, with "/".
type Found struct {
	Maps     []Map
	Warnings []string
}

var (
	scriptName = regexp.MustCompile(`\.[cm]?js$`)
	mappingURL = regexp.MustCompile(`//[#@] sourceMappingURL=(\S+)\s*$`)
	remoteURL  = regexp.MustCompile(`(?i)^[a-z]+://`)
)

// NormalizePrefix turns "static", "/static" and "/static/" into "/static/",
// and "" into "/".
func NormalizePrefix(prefix string) string {
	trimmed := strings.Trim(prefix, "/")
	if trimmed == "" {
		return "/"
	}
	return "/" + trimmed + "/"
}

// Find finds every source map under dir and the bundle it belongs to: the
// script whose sourceMappingURL comment names it, else the map's `file`
// field (when that's inside dir), else the map's name without ".map".
// Maps of anything but scripts (CSS, say) are left out, and node_modules
// isn't searched. Index maps are flattened. Problems with single maps are
// warnings; an error means dir couldn't be searched.
func Find(dir string, opts FindOptions) (Found, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Found{}, fmt.Errorf("%w: %s: %w", ErrNotDirectory, dir, err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return Found{}, fmt.Errorf("%w: %s", ErrNotDirectory, dir)
	}
	maxBytes := opts.MaxMapBytes
	if maxBytes <= 0 {
		maxBytes = MaxMapBytes
	}
	prefix := NormalizePrefix(opts.URLPrefix)
	files, err := walk(root)
	if err != nil {
		return Found{}, err
	}

	bundleOfMap := map[string]string{}
	for _, file := range files {
		if !scriptName.MatchString(file) {
			continue
		}
		if mapFile := mapOfBundle(file); mapFile != "" {
			bundleOfMap[mapFile] = file
		}
	}

	var mapFiles []string
	for _, file := range files {
		if strings.HasSuffix(file, ".map") {
			mapFiles = append(mapFiles, file)
		}
	}
	slices.Sort(mapFiles)

	var found Found
	for _, mapFile := range mapFiles {
		shown := filepath.ToSlash(relativeTo(root, mapFile))
		m, warning, err := readMap(root, mapFile, maxBytes, bundleOfMap)
		if err != nil {
			return Found{}, err
		}
		if warning != "" {
			found.Warnings = append(found.Warnings, shown+": "+warning)
		}
		if m == nil {
			continue
		}
		m.Path = prefix + filepath.ToSlash(relativeTo(root, m.Path))
		found.Maps = append(found.Maps, *m)
	}
	if opts.Sources {
		if err := found.addSources(root, prefix, files, maxBytes); err != nil {
			return Found{}, err
		}
	}
	return found, nil
}

// readMap reads one map. It returns nil for a map to skip, with the
// warning to give (if any). m.Path is the bundle's file, for Find to turn
// into a URL path.
func readMap(root, mapFile string, maxBytes int64, bundleOfMap map[string]string) (m *Map, warning string, err error) {
	info, err := os.Stat(mapFile)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", mapFile, err)
	}
	if info.Size() > maxBytes {
		mb := math.Round(float64(maxBytes) / 1024 / 1024)
		return nil, fmt.Sprintf("skipped, it's over %.0f MB", mb), nil
	}
	data, err := os.ReadFile(mapFile) //nolint:gosec // G304: a map found under the directory the user named.
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", mapFile, err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return nil, "skipped, it isn't a source map (not JSON)", nil
	}

	entry := &Map{MapFile: mapFile, Bytes: info.Size()}
	// Index maps (Turbopack, some server bundles) are flattened: Tlm
	// reads plain maps only.
	if _, isIndex := fields["sections"]; isIndex {
		flat, err := Flatten(data)
		if err != nil {
			return nil, "skipped, an index map whose sections point to other files or nest sections", nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(flat); err != nil {
			return nil, "", fmt.Errorf("encoding the flattened %s: %w", mapFile, err)
		}
		entry.Content = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		entry.Bytes = int64(len(entry.Content))
		fields = map[string]json.RawMessage{}
		if err := json.Unmarshal(entry.Content, &fields); err != nil {
			return nil, "", fmt.Errorf("reading the flattened %s: %w", mapFile, err)
		}
	}

	byName := strings.TrimSuffix(mapFile, ".map")
	bundle := bundleOfMap[mapFile]
	if bundle == "" {
		if fromField := fileField(root, mapFile, fields["file"]); fromField != "" && exists(fromField) {
			bundle = fromField
		} else if exists(byName) {
			bundle = byName
		}
	}
	target := bundle
	if target == "" {
		target = byName
	}
	if !scriptName.MatchString(target) {
		return nil, "", nil
	}
	entry.Path = target
	entry.BundleFile = bundle
	if lacksSourcesContent(fields) {
		warning = "has no sourcesContent, so Tlm can't show your code or name every function"
	}
	return entry, warning, nil
}

// fileField is the bundle a map's `file` field names, when it's inside root.
// Only bundles inside root are served from it (and may be edited by Delete).
func fileField(root, mapFile string, raw json.RawMessage) string {
	var file string
	if json.Unmarshal(raw, &file) != nil || file == "" {
		return ""
	}
	path := filepath.FromSlash(file)
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(mapFile), path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.Clean(path)
}

// lacksSourcesContent: the map lists sources but carries none of their code.
func lacksSourcesContent(fields map[string]json.RawMessage) bool {
	var sources []json.RawMessage
	if json.Unmarshal(fields["sources"], &sources) != nil || len(sources) == 0 {
		return false
	}
	var contents []json.RawMessage
	if json.Unmarshal(fields["sourcesContent"], &contents) != nil || contents == nil {
		return true
	}
	for _, c := range contents {
		if c := bytes.TrimSpace(c); len(c) > 0 && c[0] == '"' {
			return false
		}
	}
	return true
}

// walk lists the regular files under root, depth first in name order,
// skipping node_modules and symbolic links.
func walk(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.Name() == "node_modules" {
			continue
		}
		full := filepath.Join(root, entry.Name())
		switch {
		case entry.IsDir():
			sub, err := walk(full)
			if err != nil {
				return nil, err
			}
			files = append(files, sub...)
		case entry.Type().IsRegular():
			files = append(files, full)
		}
	}
	return files, nil
}

// mappingComment is what a bundle's sourceMappingURL comment says, or
// "". Only the last kilobyte of the bundle is read.
func mappingComment(bundle string) string {
	f, err := os.Open(bundle) //nolint:gosec // G304: a file found under the directory the user named.
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	const tail = 1024
	offset := max(info.Size()-tail, 0)
	buf := make([]byte, info.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	match := mappingURL.FindSubmatch(buf)
	if match == nil {
		return ""
	}
	return string(match[1])
}

// mapOfBundle is the local map a bundle's sourceMappingURL comment points
// at, or "".
func mapOfBundle(bundle string) string {
	ref := mappingComment(bundle)
	if ref == "" || strings.HasPrefix(ref, "data:") || remoteURL.MatchString(ref) {
		return ""
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	path := filepath.FromSlash(ref)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(bundle), path)
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Delete deletes the maps, and the sourceMappingURL comments in their
// bundles that point at them. It stops at the first error.
func Delete(maps []Map) error {
	for _, m := range maps {
		if m.Generated {
			continue
		}
		if err := os.Remove(m.MapFile); err != nil {
			return fmt.Errorf("deleting %s: %w", m.MapFile, err)
		}
		if m.BundleFile == "" {
			continue
		}
		if err := stripComment(m.BundleFile, filepath.Base(m.MapFile)); err != nil {
			return err
		}
	}
	return nil
}

// stripComment removes the bundle's final sourceMappingURL comment when it
// names mapName, with the newline before it.
func stripComment(bundle, mapName string) error {
	comment := regexp.MustCompile(`\n?//[#@] sourceMappingURL=(?:\S*/)?` + regexp.QuoteMeta(mapName) + `\s*$`)
	content, err := os.ReadFile(bundle) //nolint:gosec // G304: a bundle found under the directory the user named.
	if err != nil {
		return fmt.Errorf("removing the sourceMappingURL comment: %w", err)
	}
	loc := comment.FindIndex(content)
	if loc == nil {
		return nil
	}
	stripped := slices.Concat(content[:loc[0]], content[loc[1]:])
	if err := os.WriteFile(bundle, stripped, 0o600); err != nil { //nolint:gosec // G703: a bundle found under the directory the user named; the file exists, so its mode is kept.
		return fmt.Errorf("removing the sourceMappingURL comment: %w", err)
	}
	return nil
}

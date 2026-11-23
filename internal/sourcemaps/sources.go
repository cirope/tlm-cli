package sourcemaps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// minifiedLine is the line length past which a script is taken as
// minified: a line for line map of it would point every error at one line.
const minifiedLine = 1000

// addSources adds a line for line map for each script under root that has
// no map of its own: none found for it, no sourceMappingURL comment, no
// .map beside it.
func (f *Found) addSources(root, prefix string, files []string, maxBytes int64) error {
	mapped := map[string]bool{}
	for _, m := range f.Maps {
		mapped[m.BundleFile] = true
	}
	for _, file := range files {
		if !scriptName.MatchString(file) || mapped[file] || exists(file+".map") || mappingComment(file) != "" {
			continue
		}
		rel := filepath.ToSlash(relativeTo(root, file))
		code, err := os.ReadFile(file) //nolint:gosec // G304: a script found under the directory the user named.
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if looksMinified(string(code)) {
			f.Warnings = append(f.Warnings, rel+": looks minified, so it needs the source map its build makes")
			continue
		}
		content, err := lineForLine(filepath.Base(file), rel, string(code))
		if err != nil {
			return err
		}
		if int64(len(content)) > maxBytes {
			f.Warnings = append(f.Warnings, rel+": skipped, its map would be too large")
			continue
		}
		f.Maps = append(f.Maps, Map{
			Path: prefix + rel, MapFile: file + ".map", BundleFile: file,
			Bytes: int64(len(content)), Content: content, Generated: true,
		})
	}
	return nil
}

func looksMinified(code string) bool {
	for line := range strings.Lines(code) {
		if len(line) > minifiedLine {
			return true
		}
	}
	return false
}

// lineForLine is a source map from code to itself: each line's start to
// the same line of source, which holds the code.
func lineForLine(file, source, code string) ([]byte, error) {
	lines := strings.Count(code, "\n")
	if !strings.HasSuffix(code, "\n") {
		lines++
	}
	segments := make([]string, lines)
	for i := range segments {
		// One segment at column 0: source 0, one line below the last, column 0.
		segments[i] = "AACA"
	}
	if lines > 0 {
		segments[0] = "AAAA"
	}
	b, err := json.Marshal(map[string]any{
		"version":        3,
		"file":           file,
		"sources":        []string{source},
		"sourcesContent": []string{code},
		"names":          []string{},
		"mappings":       strings.Join(segments, ";"),
	})
	if err != nil {
		return nil, fmt.Errorf("making the map of %s: %w", source, err)
	}
	return b, nil
}

package sourcemaps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// RawMap is a plain (non-index) source map, version 3.
type RawMap struct {
	Version        int       `json:"version"`
	File           *string   `json:"file,omitempty"`
	Sources        []string  `json:"sources"`
	SourcesContent []*string `json:"sourcesContent"`
	Names          []string  `json:"names"`
	Mappings       string    `json:"mappings"`
	// IgnoreList holds the indexes into Sources of third-party code.
	IgnoreList []int `json:"ignoreList,omitempty"`
}

// Segment is one decoded mapping, with absolute values: the generated
// column, then optionally the source, original line, original column and
// name. It has 1, 4 or 5 values.
type Segment []int

// ErrUnsupportedIndexMap is returned by Flatten for an index map it can't
// turn into a plain one: sections that point to other files (`url`), nest
// sections, or aren't well formed.
var ErrUnsupportedIndexMap = errors.New("unsupported index map")

// maxFlatLines bounds the generated lines of a flattened map, so that a
// section's offset can't make Flatten allocate without end.
const maxFlatLines = 1 << 24

const base64Digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func decodeVLQ(text string) ([]int, error) {
	var values []int
	value, shift := 0, 0
	for i := range len(text) {
		digit := strings.IndexByte(base64Digits, text[i])
		if digit < 0 {
			return nil, fmt.Errorf("invalid mapping character %q", text[i])
		}
		value += (digit & 31) << shift
		if digit&32 != 0 {
			shift += 5
			if shift > 30 {
				return nil, errors.New("mapping value too large")
			}
			continue
		}
		if value&1 == 1 {
			values = append(values, -(value >> 1))
		} else {
			values = append(values, value>>1)
		}
		value, shift = 0, 0
	}
	return values, nil
}

func appendVLQ(out []byte, value int) []byte {
	vlq := value << 1
	if value < 0 {
		vlq = (-value << 1) | 1
	}
	for {
		digit := vlq & 31
		vlq >>= 5
		if vlq > 0 {
			digit |= 32
		}
		out = append(out, base64Digits[digit])
		if vlq == 0 {
			return out
		}
	}
}

// DecodeMappings decodes a map's `mappings` into absolute segments, one
// slice per generated line. Segments of 2 or 3 values keep only the column,
// and values past the fifth are dropped, as the format says.
func DecodeMappings(mappings string) ([][]Segment, error) {
	var state [4]int
	rawLines := strings.Split(mappings, ";")
	lines := make([][]Segment, 0, len(rawLines))
	for _, line := range rawLines {
		column := 0
		segments := []Segment{}
		for raw := range strings.SplitSeq(line, ",") {
			if raw == "" {
				continue
			}
			v, err := decodeVLQ(raw)
			if err != nil {
				return nil, err
			}
			if len(v) > 0 {
				column += v[0]
			}
			if len(v) < 4 {
				segments = append(segments, Segment{column})
				continue
			}
			for i := range 3 {
				state[i] += v[i+1]
			}
			if len(v) < 5 {
				segments = append(segments, Segment{column, state[0], state[1], state[2]})
				continue
			}
			state[3] += v[4]
			segments = append(segments, Segment{column, state[0], state[1], state[2], state[3]})
		}
		lines = append(lines, segments)
	}
	return lines, nil
}

func encodeMappings(lines [][]Segment) string {
	var state [4]int
	var out []byte
	for i, segments := range lines {
		if i > 0 {
			out = append(out, ';')
		}
		column := 0
		for j, seg := range segments {
			if j > 0 {
				out = append(out, ',')
			}
			out = appendVLQ(out, seg[0]-column)
			column = seg[0]
			for k := 1; k < len(seg); k++ {
				out = appendVLQ(out, seg[k]-state[k-1])
				state[k-1] = seg[k]
			}
		}
	}
	return string(out)
}

type indexMap struct {
	File     json.RawMessage `json:"file"`
	Sections json.RawMessage `json:"sections"`
}

type indexSection struct {
	Offset *struct {
		Line   *int `json:"line"`
		Column *int `json:"column"`
	} `json:"offset"`
	Map json.RawMessage `json:"map"`
}

type sectionMap struct {
	SourceRoot     *string           `json:"sourceRoot"`
	Sources        []string          `json:"sources"`
	SourcesContent []json.RawMessage `json:"sourcesContent"`
	Names          []string          `json:"names"`
	Mappings       *string           `json:"mappings"`
	IgnoreList     []int             `json:"ignoreList"`
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedIndexMap, fmt.Sprintf(format, args...))
}

// Flatten turns an index map (one with `sections`, as Turbopack and some
// server bundlers write) into one plain map, which Tlm can read:
// every section's mappings shifted by its offset (the column offset applies
// to its first line only), with sources and names merged. Sections that
// point to other files or nest sections give ErrUnsupportedIndexMap.
func Flatten(index []byte) (*RawMap, error) {
	var idx indexMap
	if err := json.Unmarshal(index, &idx); err != nil {
		return nil, unsupported("%v", err)
	}
	var sections []json.RawMessage
	if err := json.Unmarshal(idx.Sections, &sections); err != nil || sections == nil {
		return nil, unsupported("sections isn't a list")
	}

	flat := &RawMap{Version: 3, Sources: []string{}, SourcesContent: []*string{}, Names: []string{}}
	ignored := map[int]bool{}
	sourceIndex := map[string]int{}
	nameIndex := map[string]int{}
	var lines [][]Segment

	for n, raw := range sections {
		var sec indexSection
		if err := json.Unmarshal(raw, &sec); err != nil || sec.Offset == nil || sec.Offset.Line == nil || sec.Offset.Column == nil {
			return nil, unsupported("section %d has no offset", n)
		}
		m, err := plainSectionMap(sec.Map)
		if err != nil {
			return nil, unsupported("section %d: %v", n, err)
		}
		root := ""
		if m.SourceRoot != nil && *m.SourceRoot != "" {
			root = strings.TrimSuffix(*m.SourceRoot, "/") + "/"
		}
		sourceIDs := make([]int, len(m.Sources))
		for i, source := range m.Sources {
			path := root + source
			content := contentAt(m.SourcesContent, i)
			id, seen := sourceIndex[path]
			if !seen {
				id = len(flat.Sources)
				sourceIndex[path] = id
				flat.Sources = append(flat.Sources, path)
				flat.SourcesContent = append(flat.SourcesContent, content)
			} else if flat.SourcesContent[id] == nil {
				// The first section to carry this source's content wins.
				flat.SourcesContent[id] = content
			}
			if slices.Contains(m.IgnoreList, i) {
				ignored[id] = true
			}
			sourceIDs[i] = id
		}
		nameIDs := make([]int, len(m.Names))
		for i, name := range m.Names {
			id, seen := nameIndex[name]
			if !seen {
				id = len(flat.Names)
				nameIndex[name] = id
				flat.Names = append(flat.Names, name)
			}
			nameIDs[i] = id
		}

		decoded, err := DecodeMappings(*m.Mappings)
		if err != nil {
			return nil, unsupported("section %d: %v", n, err)
		}
		line, column := *sec.Offset.Line, *sec.Offset.Column
		if line < 0 || line+len(decoded) > maxFlatLines {
			return nil, unsupported("section %d: offset line %d is out of range", n, line)
		}
		for i, segments := range decoded {
			target := line + i
			for len(lines) <= target {
				lines = append(lines, []Segment{})
			}
			shift := 0
			if i == 0 {
				shift = column
			}
			for _, seg := range segments {
				moved := Segment{seg[0] + shift}
				if len(seg) >= 4 {
					moved = append(moved, at(sourceIDs, seg[1]), seg[2], seg[3])
				}
				if len(seg) >= 5 {
					moved = append(moved, at(nameIDs, seg[4]))
				}
				lines[target] = append(lines[target], moved)
			}
		}
	}
	for _, segments := range lines {
		slices.SortStableFunc(segments, func(a, b Segment) int { return a[0] - b[0] })
	}
	flat.Mappings = encodeMappings(lines)

	var file string
	if json.Unmarshal(idx.File, &file) == nil && len(idx.File) > 0 && idx.File[0] == '"' {
		flat.File = &file
	}
	for id := range ignored {
		flat.IgnoreList = append(flat.IgnoreList, id)
	}
	slices.Sort(flat.IgnoreList)
	return flat, nil
}

// plainSectionMap reads a section's embedded map: a plain map with sources
// and mappings, not another index map.
func plainSectionMap(raw json.RawMessage) (*sectionMap, error) {
	if len(raw) == 0 {
		return nil, errors.New("no embedded map (a url to another file isn't supported)")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return nil, errors.New("the map isn't an object")
	}
	if _, nested := keys["sections"]; nested {
		return nil, errors.New("nested sections aren't supported")
	}
	var m sectionMap
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("the map isn't well formed: %w", err)
	}
	if m.Sources == nil || m.Mappings == nil {
		return nil, errors.New("the map has no sources or mappings")
	}
	return &m, nil
}

// contentAt is sourcesContent[i] when it's a string, else nil (null).
func contentAt(contents []json.RawMessage, i int) *string {
	if i >= len(contents) {
		return nil
	}
	var s string
	if err := json.Unmarshal(contents[i], &s); err != nil || !bytes.HasPrefix(bytes.TrimSpace(contents[i]), []byte(`"`)) {
		return nil
	}
	return &s
}

// at is ids[i], or 0 when a mapping names an index the map doesn't have.
func at(ids []int, i int) int {
	if i < 0 || i >= len(ids) {
		return 0
	}
	return ids[i]
}

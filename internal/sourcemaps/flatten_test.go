package sourcemaps

import (
	"reflect"
	"strconv"
	"testing"
)

const (
	sectionA = `{"version":3,"sources":["a.ts"],"sourcesContent":["const a = 1;"],"names":["a"],"mappings":"AAAAA"}`
	sectionB = `{"version":3,"sources":["b.ts"],"sourcesContent":["const b = 2;"],"names":["b"],"mappings":"AAAAA;AACA"}`
)

func index(sections ...string) []byte {
	out := `{"version":3,"sections":[`
	for i, s := range sections {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return []byte(out + "]}")
}

func section(line, column int, m string) string {
	return `{"offset":{"line":` + strconv.Itoa(line) + `,"column":` + strconv.Itoa(column) + `},"map":` + m + `}`
}

func mustFlatten(t *testing.T, idx []byte) *RawMap {
	t.Helper()
	flat, err := Flatten(idx)
	if err != nil {
		t.Fatal(err)
	}
	return flat
}

func mustDecode(t *testing.T, mappings string) [][]Segment {
	t.Helper()
	lines, err := DecodeMappings(mappings)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func str(s string) *string { return &s }

func TestFlattenTwoSections(t *testing.T) {
	flat := mustFlatten(t, index(section(0, 0, sectionA), section(10, 0, sectionB)))
	if flat.Version != 3 ||
		!reflect.DeepEqual(flat.Sources, []string{"a.ts", "b.ts"}) ||
		!reflect.DeepEqual(flat.SourcesContent, []*string{str("const a = 1;"), str("const b = 2;")}) ||
		!reflect.DeepEqual(flat.Names, []string{"a", "b"}) {
		t.Errorf("flat = %+v", flat)
	}
	lines := mustDecode(t, flat.Mappings)
	if !reflect.DeepEqual(lines[0], []Segment{{0, 0, 0, 0, 0}}) {
		t.Errorf("line 0 = %v", lines[0])
	}
	if !reflect.DeepEqual(lines[10], []Segment{{0, 1, 0, 0, 1}}) {
		t.Errorf("line 10 = %v", lines[10])
	}
	if !reflect.DeepEqual(lines[11], []Segment{{0, 1, 1, 0}}) {
		t.Errorf("line 11 = %v", lines[11])
	}
	for i := 1; i < 10; i++ {
		if len(lines[i]) != 0 {
			t.Errorf("line %d = %v, want empty", i, lines[i])
		}
	}
}

func TestFlattenColumnOffsetAppliesToFirstLineOnly(t *testing.T) {
	lines := mustDecode(t, mustFlatten(t, index(section(0, 0, sectionA), section(0, 50, sectionB))).Mappings)
	if !reflect.DeepEqual(lines[0], []Segment{{0, 0, 0, 0, 0}, {50, 1, 0, 0, 1}}) {
		t.Errorf("line 0 = %v", lines[0])
	}
	if !reflect.DeepEqual(lines[1], []Segment{{0, 1, 1, 0}}) {
		t.Errorf("line 1 = %v", lines[1])
	}
}

func TestFlattenSameSourceListedOnce(t *testing.T) {
	flat := mustFlatten(t, index(section(0, 0, sectionA), section(1, 0, sectionA)))
	if !reflect.DeepEqual(flat.Sources, []string{"a.ts"}) {
		t.Errorf("sources = %q", flat.Sources)
	}
	if got := mustDecode(t, flat.Mappings)[1]; !reflect.DeepEqual(got, []Segment{{0, 0, 0, 0, 0}}) {
		t.Errorf("line 1 = %v", got)
	}
}

func TestFlattenUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  string
	}{
		{"nested sections", `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sections":[]}}]}`},
		{"a section pointing to another file", `{"version":3,"sections":[{"offset":{"line":0,"column":0},"url":"x.map"}]}`},
		{"sections that aren't a list", `{"version":3,"sections":{}}`},
		{"a section without an offset", `{"version":3,"sections":[{"map":` + sectionA + `}]}`},
		{"a section map without mappings", `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"sources":[]}}]}`},
		{"bad VLQ", `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"sources":[],"mappings":"A*"}}]}`},
		{"an absurd line offset", `{"version":3,"sections":[{"offset":{"line":999999999,"column":0},"map":` + sectionA + `}]}`},
		{"not JSON", `nope`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if flat, err := Flatten([]byte(tc.idx)); err == nil {
				t.Errorf("Flatten = %+v, want an error", flat)
			}
		})
	}
}

func TestFlattenRoundTripsLargeValuesAndNegatives(t *testing.T) {
	const mappings = "gvBAAgB,CAAC;AACA"
	flat := mustFlatten(t, index(section(0, 0, `{"version":3,"sources":["x.ts"],"names":[],"mappings":"`+mappings+`"}`)))
	if got, want := mustDecode(t, flat.Mappings), mustDecode(t, mappings); !reflect.DeepEqual(got, want) {
		t.Errorf("decoded = %v, want %v", got, want)
	}
}

func TestFlattenRepeatedSourceKeepsFirstNonNullContent(t *testing.T) {
	empty := `{"version":3,"sources":["a.ts"],"sourcesContent":[null],"names":["a"],"mappings":"AAAAA"}`
	flat := mustFlatten(t, index(section(0, 0, empty), section(1, 0, sectionA)))
	if !reflect.DeepEqual(flat.SourcesContent, []*string{str("const a = 1;")}) {
		t.Errorf("sourcesContent = %v", flat.SourcesContent)
	}
}

func TestFlattenKeepsIgnoreListRemapped(t *testing.T) {
	vendor := `{"version":3,"sources":["node_modules/x.js"],"names":[],"mappings":"AAAA","ignoreList":[0]}`
	flat := mustFlatten(t, index(section(0, 0, sectionA), section(1, 0, vendor)))
	if !reflect.DeepEqual(flat.IgnoreList, []int{1}) {
		t.Errorf("ignoreList = %v", flat.IgnoreList)
	}
}

func TestFlattenSourceRootAndFile(t *testing.T) {
	withRoot := `{"version":3,"sourceRoot":"src","sources":["a.ts"],"names":[],"mappings":"AAAA"}`
	flat := mustFlatten(t, []byte(`{"version":3,"file":"out.js","sections":[`+section(0, 0, withRoot)+`]}`))
	if !reflect.DeepEqual(flat.Sources, []string{"src/a.ts"}) {
		t.Errorf("sources = %q", flat.Sources)
	}
	if flat.File == nil || *flat.File != "out.js" {
		t.Errorf("file = %v", flat.File)
	}
	if !reflect.DeepEqual(flat.SourcesContent, []*string{nil}) {
		t.Errorf("sourcesContent = %v", flat.SourcesContent)
	}
}

func TestFlattenSortsSegmentsByColumn(t *testing.T) {
	// The later section's segment lands before the earlier one's on line 0.
	late := `{"version":3,"sources":["a.ts"],"names":[],"mappings":"UAAA"}`
	early := `{"version":3,"sources":["b.ts"],"names":[],"mappings":"AAAA"}`
	lines := mustDecode(t, mustFlatten(t, index(section(0, 0, late), section(0, 2, early))).Mappings)
	if !reflect.DeepEqual(lines[0], []Segment{{2, 1, 0, 0}, {10, 0, 0, 0}}) {
		t.Errorf("line 0 = %v", lines[0])
	}
}

func TestDecodeMappings(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want [][]Segment
	}{
		{"", [][]Segment{{}}},
		{"A", [][]Segment{{{0}}}},
		{"C,CAAC;;E", [][]Segment{{{1}, {2, 0, 0, 1}}, {}, {{2}}}},
		{"AAAAC,CAAAC", [][]Segment{{{0, 0, 0, 0, 1}, {1, 0, 0, 0, 2}}}},
		{"D", [][]Segment{{{-1}}}},
	} {
		if got := mustDecode(t, tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("DecodeMappings(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"!", "A=", "gggggggggggggA"} {
		if _, err := DecodeMappings(bad); err == nil {
			t.Errorf("DecodeMappings(%q) succeeded, want an error", bad)
		}
	}
}

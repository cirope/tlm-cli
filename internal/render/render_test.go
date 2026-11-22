package render

import (
	"bytes"
	"testing"
	"time"
)

func TestTable(t *testing.T) {
	got := Table([]string{"ID", "NAME"}, [][]string{{"a", "Shop"}, {"long-id", "X"}})
	want := "ID       NAME\na        Shop\nlong-id  X"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Width counts characters, not bytes, and trailing spaces go.
	got = Table([]string{"A", "B"}, [][]string{{"ñandú", ""}, {"x", "y"}})
	want = "A      B\nñandú\nx      y"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		at   string
		want string
	}{
		{"2026-11-13T11:59:30Z", "just now"},
		{"2026-11-13T11:55:00Z", "5m ago"},
		{"2026-11-13T09:00:00Z", "3h ago"},
		{"2026-11-11T12:00:00Z", "2d ago"},
		{"2026-07-13T12:00:00Z", "4mo ago"},
		{"2024-11-13T12:00:00Z", "2y ago"},
		{"2026-11-13T12:05:00Z", "just now"}, // A clock ahead.
		{"not a time", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Ago(tt.at, now); got != tt.want {
			t.Errorf("Ago(%q) = %q, want %q", tt.at, got, tt.want)
		}
	}
}

func TestClip(t *testing.T) {
	if got := Clip("abcdef", 4); got != "abc…" {
		t.Errorf("got %q", got)
	}
	if got := Clip("ñandú", 5); got != "ñandú" {
		t.Errorf("got %q", got)
	}
}

func TestSafe(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"two\nlines", "two⏎lines"},
		{"crlf\r\nline", "crlf⏎line"},
		{"bell\a and \x1b[31mred\x1b[0m", "bell� and �[31mred�[0m"},
		{"c1 \u009b31m", "c1 �31m"},
		{"tab\there", "tab�here"},
		{"del\x7f", "del�"},
		{"ñandú ✓", "ñandú ✓"},
		{"bad \xff utf8", "bad � utf8"},
	}
	for _, tt := range tests {
		if got := Safe(tt.in); got != tt.want {
			t.Errorf("Safe(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestJSONIndentsAsIs(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, []byte(`{"z":1,"a":{"<b>":"&"},"n":null}`)); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"<b>\": \"&\"\n  },\n  \"n\": null\n}\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

func TestLocalTime(t *testing.T) {
	loc := time.FixedZone("ART", -3*3600)
	if got := Time("2026-11-13T12:00:00.123456Z", loc, "2006-01-02 15:04"); got != "2026-11-13 09:00" {
		t.Errorf("got %q", got)
	}
	if got := Time("nope", loc, "15:04"); got != "nope" {
		t.Errorf("got %q", got)
	}
}

func TestSafeText(t *testing.T) {
	if got := SafeText("# Title\r\n\n\tcode \x1b[2J done\a"); got != "# Title\n\n\tcode �[2J done�" {
		t.Errorf("got %q", got)
	}
}

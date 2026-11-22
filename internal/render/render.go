// Package render turns Tlm's answers into output: aligned tables,
// relative times, JSON passed through as is, and server text made safe
// for a terminal.
package render

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Table aligns columns to their widest cell, two spaces apart, with no
// borders; trailing spaces are trimmed. Cells are made Safe.
func Table(headers []string, rows [][]string) string {
	all := append([][]string{headers}, rows...)
	widths := make([]int, len(headers))
	for _, row := range all {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], utf8.RuneCountInString(Safe(cell)))
			}
		}
	}
	lines := make([]string, 0, len(all))
	for _, row := range all {
		var b strings.Builder
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = Safe(row[i])
			}
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
			if i < len(headers)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)))
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

var units = []struct {
	size time.Duration
	unit string
}{
	{365 * 24 * time.Hour, "y"},
	{30 * 24 * time.Hour, "mo"},
	{24 * time.Hour, "d"},
	{time.Hour, "h"},
	{time.Minute, "m"},
}

// Ago is how long before now an ISO 8601 time was: "just now", "5m ago",
// "3h ago", "2d ago", "4mo ago", "2y ago"; "" for a bad time.
func Ago(iso string, now time.Time) string {
	at, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	d := max(0, now.Sub(at))
	for _, u := range units {
		if d >= u.size {
			return strconv.Itoa(int(d/u.size)) + u.unit + " ago"
		}
	}
	return "just now"
}

// Time shows an ISO 8601 time in loc with layout; unparseable ones are
// shown as given (made Safe).
func Time(iso string, loc *time.Location, layout string) string {
	at, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return Safe(iso)
	}
	return at.In(loc).Format(layout)
}

// Clip cuts s to max characters, ending in "…".
func Clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:max(0, n-1)]) + "…"
}

// Safe neutralizes text from the server before it reaches a terminal:
// line breaks become ⏎ and every other control character (C0, DEL, C1)
// or invalid byte becomes �, so no escape sequence gets through.
func Safe(s string) string {
	if isPlain(s) {
		return s
	}
	var b strings.Builder
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			b.WriteRune('⏎')
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == utf8.RuneError:
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SafeText is Safe for multi-line text (Markdown): line breaks and tabs
// stay, every other control character becomes �.
func SafeText(s string) string {
	var b strings.Builder
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == utf8.RuneError:
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// JSON writes Tlm's JSON as is, indented by two spaces, with a
// final newline: key order and escaping are the server's.
func JSON(w io.Writer, raw []byte) error {
	var b bytes.Buffer
	if err := json.Indent(&b, bytes.TrimSpace(raw), "", "  "); err != nil {
		return err
	}
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}

// Value writes v as indented JSON, for output Tlm didn't send as
// one document (a merged list, NDJSON lines are written by the caller).
func Value(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

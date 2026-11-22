package sourcemaps

import "testing"

func TestNeutralize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain text, ñandú", "plain text, ñandú"},
		{"two\nlines", "two⏎lines"},
		{"\x1b[2Jclear", "�[2Jclear"},
		{"bell\a tab\t cr\r del\x7f", "bell� tab� cr� del�"},
		{"c1 \u0085 \u009b", "c1 � �"},
	} {
		if got := neutralize(tc.in); got != tc.want {
			t.Errorf("neutralize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScrub(t *testing.T) {
	if got := scrub("a tlmu_x b tlmu_x", "tlmu_x"); got != "a [redacted] b [redacted]" {
		t.Errorf("scrub = %q", got)
	}
	if got := scrub("nothing to hide", ""); got != "nothing to hide" {
		t.Errorf("scrub with no secret = %q", got)
	}
}

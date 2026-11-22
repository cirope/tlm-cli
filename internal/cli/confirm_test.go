package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	tests := []struct {
		name     string
		yes, tty bool
		answer   string
		want     int // exit code; 0 means go ahead
	}{
		{"--yes", true, false, "", ExitOK},
		{"no terminal", false, false, "y\n", ExitUsage},
		{"yes at a terminal", false, true, "y\n", ExitOK},
		{"YES at a terminal", false, true, " Yes \n", ExitOK},
		{"no at a terminal", false, true, "n\n", ExitError},
		{"just enter", false, true, "\n", ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			a := &app{env: withDefaults(Env{Stderr: &stderr, Stdin: strings.NewReader(tt.answer), StdinIsTerminal: tt.tty})}
			err := a.confirm(tt.yes, "Remove bob?")
			if got := exitCode(err); got != tt.want {
				t.Errorf("exit = %d, want %d (err %v)", got, tt.want, err)
			}
			if tt.tty && !tt.yes && !strings.Contains(stderr.String(), "Remove bob? [y/N] ") {
				t.Errorf("stderr = %q", stderr.String())
			}
			var ue usageError
			if tt.want == ExitUsage && !errors.As(err, &ue) {
				t.Errorf("err = %v, want a usage error", err)
			}
		})
	}
}

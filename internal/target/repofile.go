package target

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RepoFileName is the file that names a repository's Tlm project. It's
// a small subset of TOML: key = "value" lines and # comments.
const RepoFileName = ".tlm.toml"

// ErrBadRepoFile is a repository file that can't be read: a usage
// mistake to fix in the file.
var ErrBadRepoFile = errors.New("bad " + RepoFileName)

type repoFileError struct{ msg string }

func (e *repoFileError) Error() string        { return e.msg }
func (e *repoFileError) Is(target error) bool { return target == ErrBadRepoFile }

var (
	repoLine  = regexp.MustCompile(`^([a-z_]+)\s*=\s*"([^"\\]*)"\s*(#.*)?$`)
	repoValue = regexp.MustCompile(`^[^"\\\r\n]+$`)
)

// FindRepoFile reads the repository file in dir or the nearest
// directory above it. With none, it returns an empty Given and path.
func FindRepoFile(dir string) (Given, string, error) {
	for {
		path := filepath.Join(dir, RepoFileName)
		b, err := os.ReadFile(path) //nolint:gosec // Read only, from the person's own working directory.
		switch {
		case err == nil:
			g, err := parseRepoFile(string(b), path)
			return g, path, err
		case !errors.Is(err, fs.ErrNotExist):
			return Given{}, "", fmt.Errorf("reading %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Given{}, "", nil
		}
		dir = parent
	}
}

func parseRepoFile(content, path string) (Given, error) {
	var g Given
	seen := map[string]bool{}
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := repoLine.FindStringSubmatch(line)
		if m == nil {
			return Given{}, &repoFileError{fmt.Sprintf("%s:%d: expected key = \"value\"", path, i+1)}
		}
		key, value := m[1], m[2]
		if seen[key] {
			return Given{}, &repoFileError{fmt.Sprintf("%s:%d: %s is set twice", path, i+1, key)}
		}
		seen[key] = true
		switch key {
		case "org":
			g.Org = value
		case "project":
			g.Project = value
		}
	}
	if g.Project != "" && g.Org == "" {
		return Given{}, &repoFileError{fmt.Sprintf("%s: project needs org", path)}
	}
	return g, nil
}

// WriteRepoFile writes g as the repository file in dir, and returns its
// path.
func WriteRepoFile(dir string, g Given) (string, error) {
	for _, v := range []string{g.Org, g.Project} {
		if v != "" && !repoValue.MatchString(v) {
			return "", fmt.Errorf("%q can't go in %s", v, RepoFileName)
		}
	}
	var b strings.Builder
	b.WriteString("# The Tlm project of this repository, for the tlm command.\n")
	fmt.Fprintf(&b, "org = %q\n", g.Org)
	if g.Project != "" {
		fmt.Fprintf(&b, "project = %q\n", g.Project)
	}
	path := filepath.Join(dir, RepoFileName)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil { //nolint:gosec // Meant to be committed and read by others.
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

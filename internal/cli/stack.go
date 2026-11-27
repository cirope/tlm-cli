package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// stacks are the ids /agents.md?stack= takes, in the order they're named.
var stacks = []string{"elixir", "node", "javascript", "ruby", "other"}

// parseStacks reads --stack: comma-separated ids, or none for the whole
// guide.
func parseStacks(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []string
	for _, id := range strings.Split(s, ",") {
		id = strings.ToLower(strings.TrimSpace(id))
		if !slices.Contains(stacks, id) {
			return nil, usageError{fmt.Errorf("unknown stack %q: use %s", id, strings.Join(stacks, ", "))}
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// detectStacks reads the stack of the app in dir from its manifests:
// Gemfile → ruby, mix.exs → elixir, package.json → node and the browser
// (a package.json doesn't say which of the two the code is, so both).
// With several manifests or none, the stack isn't guessed: it answers
// nil, and the whole guide is shown.
func detectStacks(dir string) []string {
	if dir == "" {
		return nil
	}
	ruby, elixir, npm := exists(filepath.Join(dir, "Gemfile")), exists(filepath.Join(dir, "mix.exs")), isPackage(filepath.Join(dir, "package.json"))
	switch {
	case ruby && !elixir && !npm:
		return []string{"ruby"}
	case elixir && !ruby && !npm:
		return []string{"elixir"}
	case npm && !ruby && !elixir:
		return []string{"node", "javascript"}
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isPackage reports whether path is a package.json: a JSON object.
func isPackage(path string) bool {
	b, err := os.ReadFile(path) //nolint:gosec // G304: read only, from the person's own directory.
	if err != nil {
		return false
	}
	var pkg map[string]json.RawMessage
	return json.Unmarshal(b, &pkg) == nil
}

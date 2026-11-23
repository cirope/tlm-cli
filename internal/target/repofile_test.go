package target

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRepoFileWalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "app", "javascript", "controllers")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, RepoFileName), "# The Tlm project of this repository\norg = \"acme\"\n\nproject = \"shop\"  # the storefront\n")

	got, path, err := FindRepoFile(deep)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Given{Org: "acme", Project: "shop"}) || path != filepath.Join(root, RepoFileName) {
		t.Errorf("FindRepoFile = %+v, %q", got, path)
	}
}

func TestFindRepoFileWithoutOne(t *testing.T) {
	got, path, err := FindRepoFile(t.TempDir())
	if err != nil || got != (Given{}) || path != "" {
		t.Errorf("FindRepoFile = %+v, %q, %v; want nothing", got, path, err)
	}
}

// A broken file is a mistake to fix, said with its place; keys this
// version doesn't know are left alone, for newer ones.
func TestRepoFileSyntax(t *testing.T) {
	for _, tc := range []struct{ content, want string }{
		{"org = acme\n", ".tlm.toml:1: expected key = \"value\""},
		{"org = \"acme\"\nproject = \"sh\\\"op\"\n", ".tlm.toml:2: expected key = \"value\""},
		{"org = \"acme\"\norg = \"globex\"\n", ".tlm.toml:2: org is set twice"},
		{"project = \"shop\"\n", ".tlm.toml: project needs org"},
	} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, RepoFileName), tc.content)
		_, _, err := FindRepoFile(dir)
		if !errors.Is(err, ErrBadRepoFile) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.content, err, tc.want)
		}
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, RepoFileName), "org = \"acme\"\nurl = \"https://tlm.example.com\"\n")
	if got, _, err := FindRepoFile(dir); err != nil || got != (Given{Org: "acme"}) {
		t.Errorf("unknown key: %+v, %v", got, err)
	}
}

func TestWriteRepoFileReadsBack(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteRepoFile(dir, Given{Org: "acme", Project: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := FindRepoFile(dir)
	if err != nil || got != (Given{Org: "acme", Project: "shop"}) || found != path {
		t.Errorf("read back %+v from %q, %v", got, found, err)
	}
	if _, err := WriteRepoFile(dir, Given{Org: "acme", Project: `sh"op`}); err == nil {
		t.Error("wrote a slug that can't be read back")
	}
}

// The repository's file comes after flags and the environment, before
// the machine's default; its project belongs to its organization.
func TestRepoComesBeforeTheDefault(t *testing.T) {
	ctx := context.Background()
	f := &fakeAPI{orgs: []string{"a", "b"}, projects: []string{"x", "y"}}
	r := &Resolver{API: f, Repo: Given{Org: "b", Project: "x"}, Default: Given{Org: "a", Project: "y"}}
	if org, project, err := r.Both(ctx); err != nil || org != "b" || project != "x" {
		t.Errorf("repo: %s/%s, %v", org, project, err)
	}
	if !r.Explicit() {
		t.Error("a repository's org and project count as named")
	}
	r = &Resolver{API: f, Env: Given{Org: "a"}, Repo: Given{Org: "b", Project: "x"}}
	if _, err := r.Project(ctx, "a"); err == nil || !strings.Contains(err.Error(), "a has 2 projects") {
		t.Errorf("another org: %v", err)
	}
	r = &Resolver{API: f, Repo: Given{Org: "b"}, Default: Given{Org: "a", Project: "y"}}
	if org, _ := r.Org(ctx); org != "b" {
		t.Errorf("repo org: %s", org)
	}
	if _, err := r.Project(ctx, "b"); err == nil {
		t.Error("the default's project leaked into the repository's org")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

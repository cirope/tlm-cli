package target

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cirope/tlm-cli/internal/api"
)

type fakeAPI struct {
	orgs, projects []string
	meCalls        int
	projCalls      []api.Query
}

func (f *fakeAPI) Me(context.Context) (*api.Me, []byte, error) {
	f.meCalls++
	me := &api.Me{}
	for _, o := range f.orgs {
		me.Organizations = append(me.Organizations, api.Organization{Slug: o})
	}
	return me, nil, nil
}

func (f *fakeAPI) Projects(_ context.Context, _ string, q api.Query, _ bool) (*api.Page[api.Project], error) {
	f.projCalls = append(f.projCalls, q)
	p := &api.Page[api.Project]{}
	for _, s := range f.projects {
		p.Items = append(p.Items, api.Project{Slug: s})
	}
	return p, nil
}

func TestOrg(t *testing.T) {
	ctx := context.Background()
	one := &fakeAPI{orgs: []string{"acme"}}
	if got, _ := (&Resolver{API: one, Flag: Given{Org: "flag"}, Env: Given{Org: "env"}}).Org(ctx); got != "flag" {
		t.Errorf("flag: %s", got)
	}
	if got, _ := (&Resolver{API: one, Env: Given{Org: "env"}}).Org(ctx); got != "env" {
		t.Errorf("env: %s", got)
	}
	if one.meCalls != 0 {
		t.Error("asked /me with the org given")
	}
	r := &Resolver{API: one}
	if got, _ := r.Org(ctx); got != "acme" {
		t.Errorf("only: %s", got)
	}
	_, _ = r.Org(ctx)
	if one.meCalls != 1 {
		t.Errorf("/me asked %d times, want once", one.meCalls)
	}

	_, err := (&Resolver{API: &fakeAPI{orgs: []string{"a", "b", "c"}}}).Org(ctx)
	if !errors.Is(err, ErrAmbiguous) || err.Error() != "you're in 3 organizations; pass --org a (or b, c)" {
		t.Errorf("several: %v", err)
	}
	_, err = (&Resolver{API: &fakeAPI{}}).Org(ctx)
	if !errors.Is(err, ErrAmbiguous) || err.Error() != "you're in no organizations" {
		t.Errorf("none: %v", err)
	}
}

func TestProject(t *testing.T) {
	ctx := context.Background()
	one := &fakeAPI{orgs: []string{"acme"}, projects: []string{"web"}}
	if got, _ := (&Resolver{API: one, Flag: Given{Project: "p"}}).Project(ctx, "acme"); got != "p" {
		t.Errorf("flag: %s", got)
	}
	if got, _ := (&Resolver{API: one, Env: Given{Project: "q"}}).Project(ctx, "acme"); got != "q" {
		t.Errorf("env: %s", got)
	}
	if len(one.projCalls) != 0 {
		t.Error("listed projects with the project given")
	}
	if got, _ := (&Resolver{API: one}).Project(ctx, "acme"); got != "web" {
		t.Errorf("only: %s", got)
	}
	if one.projCalls[0]["limit"] != "100" {
		t.Errorf("query = %v", one.projCalls[0])
	}
	_, err := (&Resolver{API: &fakeAPI{projects: []string{"web", "api"}}}).Project(ctx, "acme")
	if err == nil || err.Error() != "acme has 2 projects; pass --project web (or api)" {
		t.Errorf("several: %v", err)
	}
	_, err = (&Resolver{API: &fakeAPI{}}).Project(ctx, "acme")
	if err == nil || err.Error() != "acme has no projects" {
		t.Errorf("none: %v", err)
	}
}

func TestBoth(t *testing.T) {
	r := &Resolver{API: &fakeAPI{orgs: []string{"acme"}, projects: []string{"web"}}}
	org, project, err := r.Both(context.Background())
	if err != nil || org != "acme" || project != "web" {
		t.Errorf("got %s/%s, %v", org, project, err)
	}
}

func TestIssueRef(t *testing.T) {
	const origin = "https://tlm.hullop.com"
	got, err := ParseIssue("2C7ZffP787kc", origin)
	if err != nil || got != (IssueRef{ID: "2C7ZffP787kc"}) {
		t.Errorf("bare: %+v, %v", got, err)
	}
	for _, link := range []string{
		origin + "/projects/web/issues/2C7ZffP787kc",
		origin + "/projects/web/issues/2C7ZffP787kc/?x=1#top",
		origin + "/projects/web/issues/2C7ZffP787kc/events/0e2f4b1c-7d1e-4f6a-9a51-3c2d1e0f9a8b",
	} {
		got, err = ParseIssue(link, origin)
		if err != nil || got != (IssueRef{ID: "2C7ZffP787kc", FromURL: true}) {
			t.Errorf("%s: %+v, %v", link, got, err)
		}
	}
	for _, bad := range []string{
		"http://localhost:4000/projects/b/issues/X",
		origin + "/projects/web",
		"not an id!",
		"",
	} {
		if _, err := ParseIssue(bad, origin); !errors.Is(err, ErrBadIssue) {
			t.Errorf("ParseIssue(%q) = %v, want ErrBadIssue", bad, err)
		}
	}
	_, err = ParseIssue("http://localhost:4000/projects/b/issues/X", origin)
	if err == nil || err.Error() != "that issue is on http://localhost:4000, but the Tlm in use is "+origin+"; pass --url http://localhost:4000" {
		t.Errorf("other Tlm: %v", err)
	}
}

// A stored default (tlm use) comes after flags and the environment,
// before asking Tlm.
func TestDefaultComesAfterFlagsAndEnv(t *testing.T) {
	ctx := context.Background()
	f := &fakeAPI{orgs: []string{"a", "b"}, projects: []string{"x", "y"}}
	r := &Resolver{API: f, Default: Given{Org: "a", Project: "y"}}
	org, project, err := r.Both(ctx)
	if err != nil || org != "a" || project != "y" {
		t.Errorf("default: %s/%s, %v", org, project, err)
	}
	if f.meCalls != 0 || len(f.projCalls) != 0 {
		t.Error("asked Tlm with a default set")
	}
	if !r.Explicit() {
		t.Error("a default org and project count as named")
	}
	r = &Resolver{API: f, Flag: Given{Org: "b"}, Env: Given{Project: "x"}, Default: Given{Org: "a", Project: "y"}}
	if org, project, _ := r.Both(ctx); org != "b" || project != "x" {
		t.Errorf("flag and env: %s/%s", org, project)
	}
	// A default project belongs to its default org only.
	r = &Resolver{API: f, Flag: Given{Org: "b"}, Default: Given{Org: "a", Project: "y"}}
	if _, err := r.Project(ctx, "b"); err == nil || !strings.Contains(err.Error(), "b has 2 projects") {
		t.Errorf("another org: %v", err)
	}
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/cirope/tlm-cli/internal/target"
	"github.com/spf13/cobra"
)

func newIssuesCmd(a *app) *cobra.Command {
	return group(a, "issues", "What broke: list, read and settle issues",
		"  tlm issues list --env production\n"+
			"  tlm issues show 2C7ZffP787kc --markdown > issue.md\n"+
			"  tlm issues resolve 2C7ZffP787kc",
		newIssuesListCmd(a), newIssuesShowCmd(a), newIssuesEventsCmd(a), newIssuesEventCmd(a),
		newIssuesStatusCmd(a, "resolve", "resolved", "Mark resolved"),
		newIssuesStatusCmd(a, "ignore", "ignored", "Mark ignored"),
		newIssuesStatusCmd(a, "reopen", "unresolved", "Mark unresolved again"),
		newIssuesAssignCmd(a))
}

func issuePath(org, project, id string) string {
	return api.ProjectPath(org, project) + "/issues/" + api.Seg(id)
}

// locate is the issue's canonical path. An id given with --org and
// --project (or their defaults) is used as is; otherwise Tlm finds it
// through /issues/:id, which also follows merges.
func (a *app) locate(ctx context.Context, c *api.Client, ref string) (string, error) {
	r, err := target.ParseIssue(ref, c.Origin())
	if err != nil {
		return "", err
	}
	if res := a.resolver(c); !r.FromURL && res.Explicit() {
		org, project, err := res.Both(ctx)
		if err != nil {
			return "", err
		}
		a.org = org
		return issuePath(org, project, r.ID), nil
	}
	_, path, err := c.GetAt(ctx, "/issues/"+api.Seg(r.ID), nil, nil)
	if err != nil {
		return "", err
	}
	if org, ok := orgOf(path); ok {
		a.org = org
	}
	return path, nil
}

// orgOf is the organization in an API path, /orgs/:org/….
func orgOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/orgs/")
	if !ok {
		return "", false
	}
	seg, _, _ := strings.Cut(rest, "/")
	org, err := url.PathUnescape(seg)
	return org, err == nil && org != ""
}

// showPath is where to GET one issue: its canonical path when it's
// known without asking, else /issues/:id, which Tlm redirects.
func (a *app) showPath(ctx context.Context, c *api.Client, ref string) (string, error) {
	r, err := target.ParseIssue(ref, c.Origin())
	if err != nil {
		return "", err
	}
	if !r.FromURL && a.resolver(c).Explicit() {
		return a.locate(ctx, c, ref)
	}
	return "/issues/" + api.Seg(r.ID), nil
}

func headline(i *api.Issue) string {
	if i.Subtitle == "" {
		return i.Title
	}
	return i.Title + ": " + i.Subtitle
}

var rangeText = map[string]string{"day": "in the last day", "week": "in the last week", "month": "in the last month"}

type issueFilters struct {
	status, rng, env, platform, release, q string
}

func (f issueFilters) emptyLine() string {
	var parts []string
	if f.env != "" {
		parts = append(parts, "environment "+f.env)
	}
	if f.platform != "" {
		parts = append(parts, "platform "+f.platform)
	}
	if f.release != "" {
		parts = append(parts, "introduced in "+f.release)
	}
	if f.q != "" {
		parts = append(parts, fmt.Sprintf("matching %q", f.q))
	}
	line := fmt.Sprintf("No %s issues", cmpOr(f.status, "unresolved"))
	if rng := cmpOr(f.rng, "all"); rng != "all" {
		text, ok := rangeText[rng]
		if !ok {
			text = "in range " + rng
		}
		line += " seen " + text
	}
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}
	return render.Safe(line + ".")
}

func newIssuesListCmd(a *app) *cobra.Command {
	var (
		l listFlags
		f issueFilters
	)
	c := &cobra.Command{
		Use:   "list [words...]",
		Short: "Issues, last seen first",
		Long: "Issues, last seen first. Without filters: the unresolved ones. Words\n" +
			"(or --search) search titles, messages and culprits.",
		Example: "  tlm issues list\n" +
			"  tlm issues list --status resolved --range month --env production\n" +
			"  tlm issues list is not a function --json",
		Args: argsBetween(0, 100),
		RunE: func(cmd *cobra.Command, args []string) error {
			if words := strings.TrimSpace(strings.Join(args, " ")); words != "" {
				if f.q != "" {
					return usageError{errors.New("give the search as words or as --search, not both")}
				}
				f.q = words
			}
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			q := api.Query{"status": f.status, "range": f.rng, "environment": f.env, "platform": f.platform, "release": f.release, "q": f.q}
			now := a.env.Now()
			return printList(ctx, a, cl, &l, api.ProjectPath(org, project)+"/issues", q, func(is []api.Issue) string {
				rows := make([][]string, 0, len(is))
				for _, i := range is {
					events := i.EventsCount
					if i.EventsInRange != nil {
						events = *i.EventsInRange
					}
					assignee := ""
					if i.Assignee != nil {
						assignee = i.Assignee.Email
					}
					rows = append(rows, []string{i.ID, i.Level, render.Clip(render.Safe(headline(&i)), 80),
						strconv.Itoa(events), strconv.Itoa(i.UsersCount), render.Ago(i.LastSeenAt, now), assignee})
				}
				return render.Table([]string{"ID", "LEVEL", "TITLE", "EVENTS", "USERS", "LAST SEEN", "ASSIGNEE"}, rows)
			}, f.emptyLine())
		},
	}
	fl := c.Flags()
	fl.StringVar(&f.status, "status", "", "unresolved (default), resolved or ignored")
	fl.StringVar(&f.rng, "range", "", "seen in the last day, week or month, or all (default)")
	fl.StringVar(&f.env, "env", "", "only this environment")
	fl.StringVar(&f.platform, "platform", "", "the platform of its first event: elixir, javascript, node, ruby, other")
	fl.StringVar(&f.release, "release", "", "only issues this release introduced")
	fl.StringVar(&f.q, "search", "", "text in the title, message or culprit (or give it as words)")
	l.add(c)
	return c
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func topFrame(i *api.Issue) string {
	if i.LatestEvent == nil || i.LatestEvent.Payload == nil || i.LatestEvent.Payload.Exception == nil {
		return ""
	}
	frames := i.LatestEvent.Payload.Exception.Frames
	if len(frames) == 0 {
		return ""
	}
	f := frames[0]
	for _, fr := range frames {
		if fr.InApp {
			f = fr
			break
		}
	}
	return frameText(f)
}

func frameText(f api.Frame) string {
	if f.File == "" {
		return ""
	}
	s := f.File
	if f.Line != nil {
		s += ":" + strconv.Itoa(*f.Line)
	}
	if f.Function != "" {
		s += " in " + f.Function
	}
	return s
}

func newIssuesShowCmd(a *app) *cobra.Command {
	var markdown bool
	c := &cobra.Command{
		Use:   "show <id|url>",
		Short: "A summary; --markdown for the AI-ready text",
		Long: "One issue. --markdown prints the issue as Tlm writes it for an AI\n" +
			"assistant asked to fix it: the error, the stack trace with the code\n" +
			"around each line, the request, breadcrumbs and tags.",
		Example: "  tlm issues show 2C7ZffP787kc\n" +
			"  tlm issues show 2C7ZffP787kc --markdown > issue.md\n" +
			"  tlm issues show https://tlm.hullop.com/projects/shop/issues/2C7ZffP787kc --json",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if markdown && a.flags.json {
				return usageError{errors.New("--markdown and --json don't go together")}
			}
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			path, err := a.showPath(ctx, cl, args[0])
			if err != nil {
				return err
			}
			if markdown {
				md, err := cl.Markdown(ctx, path, nil)
				if err != nil {
					return err
				}
				a.out("%s", render.SafeText(strings.TrimRight(md, "\n")))
				return nil
			}
			var i api.Issue
			raw, err := cl.Get(ctx, path, nil, &i)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			now := a.env.Now()
			rel := func(r *string) string {
				if r == nil || *r == "" {
					return ""
				}
				return " (" + *r + ")"
			}
			lines := []string{
				headline(&i),
				fmt.Sprintf("%s · %s · %s · %s · %s", i.Status, i.Level, i.Platform, plural(i.EventsCount, "event"), plural(i.UsersCount, "user")),
				fmt.Sprintf("First seen %s%s · last seen %s%s", render.Ago(i.FirstSeenAt, now), rel(i.FirstRelease), render.Ago(i.LastSeenAt, now), rel(i.LastRelease)),
			}
			if i.Assignee != nil {
				lines = append(lines, "Assigned to "+i.Assignee.Email)
			}
			if f := topFrame(&i); f != "" {
				lines = append(lines, "At "+f)
			}
			lines = append(lines, i.URL)
			for _, l := range lines {
				a.out("%s", render.Safe(l))
			}
			a.progress("Full details for an AI assistant: tlm issues show %s --markdown", i.ID)
			return nil
		},
	}
	c.Flags().BoolVar(&markdown, "markdown", false, "the issue as Markdown, for an AI assistant")
	return c
}

func newIssuesEventsCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "events <id|url>",
		Short:   "Its events, newest first",
		Example: "  tlm issues events 2C7ZffP787kc\n  tlm issues events 2C7ZffP787kc --all --json",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			path, err := a.locate(ctx, cl, args[0])
			if err != nil {
				return err
			}
			now := a.env.Now()
			return printList(ctx, a, cl, &l, path+"/events", nil, func(es []api.Event) string {
				rows := make([][]string, 0, len(es))
				for _, e := range es {
					details := "kept"
					if !e.Kept {
						details = "count only"
					}
					rows = append(rows, []string{e.UUID, render.Ago(e.OccurredAt, now), deref(e.Environment), deref(e.Release), details})
				}
				return render.Table([]string{"EVENT", "OCCURRED", "ENVIRONMENT", "RELEASE", "DETAILS"}, rows)
			}, "No events.")
		},
	}
	l.add(c)
	return c
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func newIssuesEventCmd(a *app) *cobra.Command {
	var markdown bool
	c := &cobra.Command{
		Use:     "event <id|url> <uuid>",
		Short:   "One event in full",
		Example: "  tlm issues event 2C7ZffP787kc 0e2f4b1c-7d1e-4f6a-9a51-3c2d1e0f9a8b\n  tlm issues event 2C7ZffP787kc 0e2f4b1c-7d1e-4f6a-9a51-3c2d1e0f9a8b --markdown",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if markdown && a.flags.json {
				return usageError{errors.New("--markdown and --json don't go together")}
			}
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			path, err := a.locate(ctx, cl, args[0])
			if err != nil {
				return err
			}
			if markdown {
				md, err := cl.Markdown(ctx, path+"/events/"+api.Seg(args[1]), nil)
				if err != nil {
					return err
				}
				a.out("%s", render.SafeText(strings.TrimRight(md, "\n")))
				return nil
			}
			var e api.Event
			raw, err := cl.Get(ctx, path+"/events/"+api.Seg(args[1]), nil, &e)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printEvent(&e)
			return nil
		},
	}
	c.Flags().BoolVar(&markdown, "markdown", false, "the issue told through this event, as Markdown for an AI")
	return c
}

func (a *app) printEvent(e *api.Event) {
	meta := []string{render.Time(e.OccurredAt, a.env.Location, "2006-01-02 15:04:05")}
	if v := deref(e.Environment); v != "" {
		meta = append(meta, v)
	}
	if v := deref(e.Release); v != "" {
		meta = append(meta, "release "+v)
	}
	a.out("%s", render.Safe(e.UUID+" · "+strings.Join(meta, " · ")))
	p := e.Payload
	if p == nil {
		a.out("Stored without details (a burst was rate limited).")
		return
	}
	if ex := p.Exception; ex != nil {
		a.out("%s", render.Safe(strings.TrimSuffix(ex.Type+": "+ex.Message, ": ")))
		for _, f := range ex.Frames {
			if t := frameText(f); t != "" {
				mark := "  "
				if f.InApp {
					mark = "> "
				}
				a.out("%s%s", mark, render.Safe(t))
			}
		}
	}
	if r := p.Request; r != nil && r.URL != "" {
		a.out("Request: %s", render.Safe(strings.TrimSpace(r.Method+" "+r.URL)))
	}
	if len(p.Tags) > 0 {
		var tags []string
		for _, k := range slices.Sorted(maps.Keys(p.Tags)) {
			tags = append(tags, fmt.Sprintf("%s=%v", k, p.Tags[k]))
		}
		a.out("Tags: %s", render.Safe(strings.Join(tags, " ")))
	}
}

// patchEach PATCHes each issue in order, one line per issue. Any failure
// is reported and the rest still go; it exits 1, or 3 if the token was
// refused.
func (a *app) patchEach(ctx context.Context, refs []string, body map[string]any, done func(*api.Issue) string) error {
	cl, _, err := a.client()
	if err != nil {
		return err
	}
	code := ExitOK
	for _, ref := range refs {
		var i api.Issue
		path, err := a.locate(ctx, cl, ref)
		var raw []byte
		if err == nil {
			raw, err = cl.Send(ctx, http.MethodPatch, path, nil, body, &i)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.reportError(fmt.Errorf("%s: %w", render.Safe(ref), err))
			c := exitCode(err)
			switch {
			case c == ExitAuth:
				code = ExitAuth
			case code != ExitAuth:
				code = ExitError
			}
			continue
		}
		if a.flags.json {
			if err := a.printJSON(raw); err != nil {
				return err
			}
			continue
		}
		a.out("%s %s", render.Safe(i.ID), render.Safe(done(&i)))
	}
	if code != ExitOK {
		return silentError{code}
	}
	return nil
}

func newIssuesStatusCmd(a *app, verb, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:     verb + " <id|url>...",
		Short:   short,
		Example: fmt.Sprintf("  tlm issues %s 2C7ZffP787kc\n  tlm issues %s 2C7ZffP787kc 3D8AggQ898ld", verb, verb),
		Args:    argsBetween(1, 100),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.patchEach(cmd.Context(), args, map[string]any{"status": status}, func(i *api.Issue) string { return i.Status })
		},
	}
}

func newIssuesAssignCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "assign <id|url> <email|me|none>",
		Short:   "Assign or unassign",
		Example: "  tlm issues assign 2C7ZffP787kc me\n  tlm issues assign 2C7ZffP787kc ada@example.com\n  tlm issues assign 2C7ZffP787kc none",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var who any = args[1]
			if args[1] == "none" {
				who = nil
			}
			return a.patchEach(cmd.Context(), args[:1], map[string]any{"assignee": who}, func(i *api.Issue) string {
				if i.Assignee == nil {
					return "unassigned"
				}
				return "assigned to " + i.Assignee.Email
			})
		},
	}
}

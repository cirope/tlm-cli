package cli

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

// speedFlags are the Speed tab's filters, sent as is: Tlm says
// which values it takes when one isn't.
type speedFlags struct {
	env, period, device string
}

func (f *speedFlags) query() api.Query {
	return api.Query{"env": f.env, "period": f.period, "device": f.device}
}

// speedTop is how many of the slowest pages and requests the overview
// shows.
const speedTop = 5

// speedError explains a 404: Tlm without ClickHouse keeps no speed
// data.
func speedError(err error) error {
	var e *api.Error
	if errors.As(err, &e) && e.Status == http.StatusNotFound {
		e.Message = "this Tlm doesn't keep speed data, or the project doesn't exist (" + e.Message + ")"
	}
	return err
}

func newSpeedCmd(a *app) *cobra.Command {
	f := &speedFlags{}
	c := &cobra.Command{
		Use:   "speed",
		Short: "Web Vitals at p75, the slowest pages and requests",
		Long: "How fast the project's pages are for real visitors, as the Speed tab\n" +
			"shows: Web Vitals at p75, then the slowest pages and requests. Their\n" +
			"own commands list them all and drill into one.",
		Example: "  tlm speed\n" +
			"  tlm speed --env production --period month --device phone\n" +
			"  tlm speed pages --all --json\n" +
			"  tlm speed page /checkout\n" +
			"  tlm speed request GET /api/cart",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown action %q for speed (use %s)", args[0], actions(cmd))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			path := api.ProjectPath(org, project) + "/speed"
			var o api.SpeedOverview
			raw, err := cl.Get(ctx, path, f.query(), &o)
			if err != nil {
				return speedError(err)
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			pages, err := api.List[api.SpeedPage](ctx, cl, path+"/pages", f.query(), false)
			if err != nil {
				return speedError(err)
			}
			reqs, err := api.List[api.SpeedRequest](ctx, cl, path+"/requests", f.query(), false)
			if err != nil {
				return speedError(err)
			}
			a.out("%s", render.Safe(speedHeadline(&o)))
			a.out("")
			a.out("%s", vitalsTable(o.Vitals))
			if len(pages.Items) > 0 {
				a.out("")
				a.out("Slowest pages")
				a.out("%s", speedPagesTable(pages.Items[:min(speedTop, len(pages.Items))]))
			}
			if len(reqs.Items) > 0 {
				a.out("")
				a.out("Slowest requests")
				a.out("%s", speedRequestsTable(reqs.Items[:min(speedTop, len(reqs.Items))]))
			}
			if others := slices.DeleteFunc(slices.Clone(o.Environments), func(e string) bool {
				return o.Environment != nil && e == *o.Environment
			}); len(others) > 0 {
				a.progress("Other environments (--env): %s", render.Safe(strings.Join(others, ", ")))
			}
			a.progress("Every page: tlm speed pages · one page: tlm speed page <route>")
			return nil
		},
	}
	pf := c.PersistentFlags()
	pf.StringVar(&f.env, "env", "", "this environment (default: the one with most page views)")
	pf.StringVar(&f.period, "period", "", "day, week (default), month or quarter")
	pf.StringVar(&f.device, "device", "", "phone, tablet or desktop (default: all)")
	c.AddCommand(newSpeedPagesCmd(a, f), newSpeedPageCmd(a, f), newSpeedRequestsCmd(a, f), newSpeedRequestCmd(a, f))
	return c
}

func newSpeedPagesCmd(a *app, f *speedFlags) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "pages",
		Short:   "The pages, slowest first",
		Example: "  tlm speed pages\n  tlm speed pages --period month --device phone --all --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			return speedError(printList(ctx, a, cl, &l, api.ProjectPath(org, project)+"/speed/pages", f.query(),
				speedPagesTable, "No page views in this period."))
		},
	}
	l.add(c)
	return c
}

func newSpeedRequestsCmd(a *app, f *speedFlags) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "requests",
		Short:   "The endpoints pages call, slowest p75 first",
		Example: "  tlm speed requests\n  tlm speed requests --env production --all --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			return speedError(printList(ctx, a, cl, &l, api.ProjectPath(org, project)+"/speed/requests", f.query(),
				speedRequestsTable, "No requests from pages in this period."))
		},
	}
	l.add(c)
	return c
}

func newSpeedPageCmd(a *app, f *speedFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "page <route>",
		Short:   "One page: its vitals, devices and slowest visits",
		Example: "  tlm speed page /checkout\n  tlm speed page /checkout --device phone --json",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			q := f.query()
			q["route"] = args[0]
			var p api.SpeedPageDetail
			raw, err := cl.Get(ctx, api.ProjectPath(org, project)+"/speed/page", q, &p)
			if err != nil {
				return speedError(err)
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			now := a.env.Now()
			a.out("%s", render.Safe(p.Route+" · "+pluralF(p.Views, "page view")))
			a.out("")
			a.out("%s", vitalsTable(p.Vitals))
			if len(p.Devices) > 0 {
				rows := make([][]string, 0, len(p.Devices))
				for _, d := range p.Devices {
					rows = append(rows, []string{d.Value, count(d.Views), percent(d.Share), millis(d.LCP), millis(d.Nav)})
				}
				a.out("")
				a.out("Devices")
				a.out("%s", render.Table([]string{"DEVICE", "VIEWS", "SHARE", "LCP", "NAV"}, rows))
			}
			if len(p.SlowVisits) > 0 {
				rows := make([][]string, 0, len(p.SlowVisits))
				for _, v := range p.SlowVisits {
					rows = append(rows, []string{render.Ago(v.At, now), deref(v.Device), deref(v.Browser), deref(v.Country),
						deref(v.Release), millis(v.LCP), millis(v.Nav), v.Session, v.ID})
				}
				a.out("")
				a.out("Slow visits")
				a.out("%s", render.Table([]string{"WHEN", "DEVICE", "BROWSER", "COUNTRY", "RELEASE", "LCP", "NAV", "SESSION", "PAGE VIEW"}, rows))
			}
			return nil
		},
	}
}

func newSpeedRequestCmd(a *app, f *speedFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "request <method> <route>",
		Short:   "One endpoint: the pages that call it and its slowest calls",
		Example: "  tlm speed request GET /api/cart\n  tlm speed request POST /api/orders --period day --json",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			q := f.query()
			q["method"] = strings.ToUpper(args[0])
			q["route"] = args[1]
			var r api.SpeedRequestDetail
			raw, err := cl.Get(ctx, api.ProjectPath(org, project)+"/speed/request", q, &r)
			if err != nil {
				return speedError(err)
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			now := a.env.Now()
			a.out("%s", render.Safe(r.Method+" "+r.Route))
			if len(r.Pages) > 0 {
				rows := make([][]string, 0, len(r.Pages))
				for _, p := range r.Pages {
					rows = append(rows, []string{p.Route, count(p.Calls), percent(p.Failed), millis(p.P75), millis(p.Start), millis(p.LCP), millis(p.Nav)})
				}
				a.out("")
				a.out("Pages that call it")
				a.out("%s", render.Table([]string{"PAGE", "CALLS", "FAILED", "P75", "START", "PAGE LCP", "PAGE NAV"}, rows))
			}
			if len(r.SlowCalls) > 0 {
				rows := make([][]string, 0, len(r.SlowCalls))
				for _, s := range r.SlowCalls {
					status := "—"
					if s.Status != nil {
						status = strconv.Itoa(*s.Status)
					}
					rows = append(rows, []string{render.Ago(s.At, now), deref(s.Page), status, millis(s.Duration),
						deref(s.Device), deref(s.Browser), deref(s.Country), s.Session, s.ID})
				}
				a.out("")
				a.out("Slow calls")
				a.out("%s", render.Table([]string{"WHEN", "PAGE", "STATUS", "DURATION", "DEVICE", "BROWSER", "COUNTRY", "SESSION", "PAGE VIEW"}, rows))
			}
			return nil
		},
	}
}

var periodText = map[string]string{"day": "last day", "week": "last week", "month": "last month", "quarter": "last quarter"}

// speedHeadline is "production · last week · all devices · 1520 page
// views".
func speedHeadline(o *api.SpeedOverview) string {
	var parts []string
	if o.Environment != nil && *o.Environment != "" {
		parts = append(parts, *o.Environment)
	}
	parts = append(parts, cmpOr(periodText[o.Period], o.Period))
	if o.Device != nil && *o.Device != "" {
		parts = append(parts, *o.Device)
	} else {
		parts = append(parts, "all devices")
	}
	parts = append(parts, pluralF(o.Views, "page view"))
	if o.Dropped > 0 {
		parts = append(parts, count(o.Dropped)+" dropped")
	}
	return strings.Join(parts, " · ")
}

// vitalOrder is how vitals are listed; any others follow by name.
var vitalOrder = []string{"lcp", "inp", "cls", "fcp", "ttfb", "nav"}

func vitalsTable(vs map[string]api.Vital) string {
	names := slices.DeleteFunc(slices.Clone(vitalOrder), func(n string) bool { _, ok := vs[n]; return !ok })
	for _, n := range slices.Sorted(maps.Keys(vs)) {
		if !slices.Contains(vitalOrder, n) {
			names = append(names, n)
		}
	}
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		v := vs[n]
		p75 := millis(v.P75)
		if n == "cls" {
			p75 = "—"
			if v.P75 != nil {
				p75 = strconv.FormatFloat(*v.P75, 'f', 2, 64)
			}
		}
		if v.Samples == 0 {
			rows = append(rows, []string{strings.ToUpper(n), p75, "—", "—", "—", "0"})
			continue
		}
		rows = append(rows, []string{strings.ToUpper(n), p75, percent(v.Good), percent(v.NeedsWork), percent(v.Poor), strconv.Itoa(v.Samples)})
	}
	return render.Table([]string{"VITAL", "P75", "GOOD", "NEEDS WORK", "POOR", "SAMPLES"}, rows)
}

func speedPagesTable(ps []api.SpeedPage) string {
	rows := make([][]string, 0, len(ps))
	for _, p := range ps {
		rows = append(rows, []string{p.Route, p.Navigation, count(p.Views), millis(p.LCP), millis(p.Nav), millis(p.INP)})
	}
	return render.Table([]string{"ROUTE", "LOAD", "VIEWS", "LCP", "NAV", "INP"}, rows)
}

func speedRequestsTable(rs []api.SpeedRequest) string {
	rows := make([][]string, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, []string{r.Method, r.Route, count(r.Calls), percent(r.Failed), millis(r.P75), millis(r.Start)})
	}
	return render.Table([]string{"METHOD", "ROUTE", "CALLS", "FAILED", "P75", "START"}, rows)
}

// millis shows a time in ms: "150 ms" under a second, else "2.40 s";
// "—" when there's none.
func millis(v *float64) string {
	switch {
	case v == nil:
		return "—"
	case math.Round(*v) < 1000:
		return strconv.FormatFloat(math.Round(*v), 'f', 0, 64) + " ms"
	}
	return strconv.FormatFloat(*v/1000, 'f', 2, 64) + " s"
}

// percent shows a share (0 to 1) as a whole percentage.
func percent(share float64) string {
	return strconv.FormatFloat(math.Round(share*100), 'f', 0, 64) + "%"
}

// count shows a count Tlm may weight by sample rate, rounded.
func count(v float64) string { return strconv.FormatFloat(math.Round(v), 'f', 0, 64) }

func pluralF(v float64, word string) string { return plural(int(math.Round(v)), word) }

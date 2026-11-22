package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newSignalsCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:   "signals [<project>]",
		Short: "Which servers and browsers report, per environment",
		Long: "Signs of life: which servers and browsers report to the project, one\n" +
			"row per environment and source, latest first. active: seen lately (three\n" +
			"hours for servers, a day for browsers); quiet: a server gone silent where\n" +
			"that matters; idle: otherwise silent.",
		Example: "  tlm signals\n  tlm signals shop --json",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := ""
			if len(args) > 0 {
				slug = args[0]
			}
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, slug)
			if err != nil {
				return err
			}
			now := a.env.Now()
			return printList(ctx, a, cl, &l, api.ProjectPath(org, project)+"/signals", nil, func(ss []api.Signal) string {
				rows := make([][]string, 0, len(ss))
				for _, s := range ss {
					seen := "never"
					if s.LastSeenAt != nil {
						seen = render.Ago(*s.LastSeenAt, now)
					}
					sdk := strings.TrimSpace(deref(s.SDKName) + " " + deref(s.SDKVersion))
					rows = append(rows, []string{deref(s.Environment), s.Source, s.Status, seen, sdk,
						deref(s.Release), deref(s.Server), runtimeText(s.Runtime)})
				}
				return render.Table([]string{"ENVIRONMENT", "SOURCE", "STATUS", "LAST SEEN", "SDK", "RELEASE", "SERVER", "RUNTIME"}, rows)
			}, "Nothing has reported to "+render.Safe(project)+" yet.")
		},
	}
	l.add(c)
	return c
}

// runtimeText is "elixir 1.19.4, otp 28", by name.
func runtimeText(rt map[string]any) string {
	parts := make([]string, 0, len(rt))
	for _, k := range slices.Sorted(maps.Keys(rt)) {
		if rt[k] == nil {
			parts = append(parts, k)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %v", k, rt[k]))
	}
	return strings.Join(parts, ", ")
}

package cli

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newAuditCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:   "audit",
		Short: "Who did what in the organization, newest first",
		Long: "The organization's audit log, newest first: who did what, to what, and\n" +
			"how (the web app, or \"API: <token name>\"). Admins.",
		Example: "  tlm audit\n  tlm audit --org acme --limit 20\n  tlm audit --all --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			org, err := a.resolver(cl).Org(ctx)
			if err != nil {
				return err
			}
			a.org = org
			now := a.env.Now()
			return printList(ctx, a, cl, &l, api.OrgPath(org)+"/audits", nil, func(as []api.Audit) string {
				rows := make([][]string, 0, len(as))
				for _, e := range as {
					rows = append(rows, []string{e.ID, render.Ago(e.CreatedAt, now), actorText(&e), e.Event,
						strings.TrimSpace(e.ItemType + " " + e.ItemID), viaText(&e), render.Clip(render.Safe(changesText(e.Changes)), 60)})
				}
				return render.Table([]string{"ID", "WHEN", "WHO", "EVENT", "ITEM", "VIA", "CHANGES"}, rows)
			}, "Nothing in "+render.Safe(org)+"'s audit log.")
		},
	}
	l.add(c)
	return c
}

// actorText is the person, "System" for Tlm itself, or "—" for an
// account since deleted.
func actorText(e *api.Audit) string {
	switch {
	case e.Actor != nil:
		return e.Actor.FullName()
	case e.Origin == "system":
		return "System"
	}
	return "—"
}

// viaText is "API: <token name>" for changes made with a token, "web"
// for a person's others, and nothing for Tlm's own.
func viaText(e *api.Audit) string {
	switch {
	case deref(e.Via) == "api":
	case e.Origin == "system":
		return ""
	default:
		return "web"
	}
	if name := deref(e.TokenName); name != "" {
		return "API: " + name
	}
	return "API"
}

// changesText is "name: Shop → Storefront; …": [before, after] pairs
// with an arrow, other values as they are.
func changesText(cs map[string]json.RawMessage) string {
	parts := make([]string, 0, len(cs))
	for _, k := range slices.Sorted(maps.Keys(cs)) {
		var pair []json.RawMessage
		if err := json.Unmarshal(cs[k], &pair); err == nil && len(pair) == 2 {
			parts = append(parts, k+": "+jsonText(pair[0])+" → "+jsonText(pair[1]))
			continue
		}
		parts = append(parts, k+": "+jsonText(cs[k]))
	}
	return strings.Join(parts, "; ")
}

// jsonText is a JSON value for people: strings without quotes, null as
// "—", anything else compact.
func jsonText(raw json.RawMessage) string {
	if string(bytes.TrimSpace(raw)) == "null" {
		return "—"
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/cirope/tlm-cli/internal/update"
)

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "This tlm version, and whether a newer one is out",
		Long: "Prints tlm's version and asks GitHub for the latest release, in a\n" +
			"pipe or CI too, then says how to upgrade. When the check fails it\n" +
			"warns and still exits 0. --json: {version, commit, date, latest,\n" +
			"update_available, upgrade}; latest and upgrade are null when unknown.",
		Example: "  tlm version\n  tlm version --json",
		Args:    argsBetween(0, 0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.noNotice = true
			b := a.env.Build
			latest, err := a.updateChecker().Latest(cmd.Context())
			newer := err == nil && update.Newer(latest, b.Version)
			if a.flags.json {
				out := map[string]any{
					"version": b.Version, "commit": b.Commit, "date": b.Date,
					"latest": nil, "update_available": newer, "upgrade": nil,
				}
				if err == nil {
					out["latest"] = latest
				}
				if newer {
					out["upgrade"] = a.upgradeHint()
				}
				data, _ := json.Marshal(out)
				return a.printJSON(data)
			}
			a.out("tlm %s (commit %s, built %s)", b.Version, b.Commit, b.Date)
			switch {
			case err != nil:
				a.warn("couldn't check for a newer version: %v", err)
			case newer:
				a.out("A new version of tlm is available: %s.", latest)
				a.out("Update with: %s", a.upgradeHint())
			case latest == b.Version:
				a.out("This is the latest version.")
			default:
				a.out("The latest release is %s.", latest)
			}
			return nil
		},
	}
}

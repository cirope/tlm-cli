package cli

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/spf13/cobra"
)

// projectTabs are the project pages tlm open goes to by name.
var projectTabs = []string{"setup", "speed", "settings"}

func newOpenCmd(a *app) *cobra.Command {
	var noBrowser bool
	c := &cobra.Command{
		Use:   "open [issue <id|url> | setup | speed | settings]",
		Short: "Open the web app's page for a project or an issue",
		Long: "Open a page of Tlm's web app: the project's issues (no argument), one\n" +
			"issue, or the project's setup, Speed or settings tab. The URL is\n" +
			"printed too.",
		Example: "  tlm open\n" +
			"  tlm open issue 2C7ZffP787kc\n" +
			"  tlm open speed\n" +
			"  tlm open settings --no-browser",
		Args: argsBetween(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			page := ""
			if len(args) > 0 {
				page, args = args[0], args[1:]
			}
			var link string
			switch {
			case page == "issue":
				if len(args) != 1 {
					return usageError{errors.New("missing arguments: tlm open issue <id|url>")}
				}
				cl, _, err := a.client()
				if err != nil {
					return err
				}
				path, err := a.showPath(ctx, cl, args[0])
				if err != nil {
					return err
				}
				var i api.Issue
				if _, err := cl.Get(ctx, path, nil, &i); err != nil {
					return err
				}
				link = i.URL
			case page == "" || slices.Contains(projectTabs, page):
				if len(args) > 0 {
					return usageError{errors.New("unexpected arguments: " + strings.Join(args, " "))}
				}
				p, _, err := a.fetchProject(ctx, nil)
				if err != nil {
					return err
				}
				if link, err = tabURL(p.URL, page); err != nil {
					return err
				}
			default:
				return usageError{fmt.Errorf("unknown page %q: use issue, %s", page, strings.Join(projectTabs, ", "))}
			}
			a.out("%s", link)
			if a.env.StdoutIsTerminal && !noBrowser {
				_ = a.env.OpenBrowser(link)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the URL")
	return c
}

// tabURL is a tab of the project page at projectURL, keeping its query
// (the organization it's in).
func tabURL(projectURL, tab string) (string, error) {
	u, err := url.Parse(projectURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("no project URL to open in the answer (%q)", projectURL)
	}
	if tab != "" {
		u = u.JoinPath(tab)
	}
	return u.String(), nil
}

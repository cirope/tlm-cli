package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/cirope/tlm-cli/internal/target"
	"github.com/spf13/cobra"
)

func newUseCmd(a *app) *cobra.Command {
	var clear, here bool
	c := &cobra.Command{
		Use:   "use [<org>[/<project>]]",
		Short: "Set the organization and project commands use by default",
		Long: "Set the organization, and optionally the project, that commands use\n" +
			"when --org/--project and TLM_ORG/TLM_PROJECT don't say. Without an\n" +
			"argument, show it.\n\n" +
			"With --here, for this repository: it writes .tlm.toml in the current\n" +
			"directory, to commit, found from any directory below it. It wins over\n" +
			"this machine's default, which is per Tlm.",
		Example: "  tlm use --here acme/shop\n  tlm use acme/shop\n  tlm use acme\n  tlm use\n  tlm use --clear",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if here {
				return a.useHere(cmd, args, clear)
			}
			s, err := a.resolve()
			if err != nil {
				return err
			}
			cfg, _, err := a.config()
			if err != nil {
				return err
			}
			if clear {
				if len(args) > 0 {
					return usageError{errors.New("--clear takes no argument")}
				}
				delete(cfg.Defaults, s.url)
				if err := cfg.Save(a.configPath()); err != nil {
					return err
				}
				a.out("No default on %s.", s.url)
				return nil
			}
			if len(args) == 0 {
				if err := a.readRepoFile(); err != nil {
					return err
				}
				if a.repoPath != "" {
					a.out("%s, from %s", render.Safe(givenText(a.repo)), a.repoPath)
					return nil
				}
				d, ok := cfg.Defaults[s.url]
				if !ok {
					a.out("No default on %s.", s.url)
					a.progress("Set one with: tlm use <org>[/<project>]")
					return nil
				}
				a.out("%s", render.Safe(defaultText(d)))
				return nil
			}
			g, err := parseUse(args[0])
			if err != nil {
				return err
			}
			c, _, err := a.client()
			if err != nil {
				return err
			}
			if err := a.checkUse(cmd, c, g); err != nil {
				return err
			}
			if cfg.Defaults == nil {
				cfg.Defaults = map[string]auth.Default{}
			}
			d := auth.Default{Org: g.Org, Project: g.Project}
			cfg.Defaults[s.url] = d
			if err := cfg.Save(a.configPath()); err != nil {
				return err
			}
			a.out("Commands on %s now use %s.", s.url, render.Safe(defaultText(d)))
			a.progress("--org and --project, and TLM_ORG and TLM_PROJECT, still win.")
			return nil
		},
	}
	c.Flags().BoolVar(&clear, "clear", false, "forget the default")
	c.Flags().BoolVar(&here, "here", false, "for this repository: write (or with --clear remove) .tlm.toml here")
	return c
}

// useHere writes, or with clear removes, the repository's .tlm.toml in
// the current directory.
func (a *app) useHere(cmd *cobra.Command, args []string, clear bool) error {
	if a.env.Dir == "" {
		return errors.New("can't tell which directory this is")
	}
	path := filepath.Join(a.env.Dir, target.RepoFileName)
	if clear {
		if len(args) > 0 {
			return usageError{errors.New("--clear takes no argument")}
		}
		err := os.Remove(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			a.out("No %s here.", target.RepoFileName)
			return nil
		case err != nil:
			return err
		}
		a.out("Removed %s.", path)
		return nil
	}
	if len(args) == 0 {
		return usageError{errors.New("say which: tlm use --here <org>[/<project>]")}
	}
	g, err := parseUse(args[0])
	if err != nil {
		return err
	}
	c, _, err := a.client()
	if err != nil {
		return err
	}
	if err := a.checkUse(cmd, c, g); err != nil {
		return err
	}
	if path, err = target.WriteRepoFile(a.env.Dir, g); err != nil {
		return err
	}
	a.out("Commands in %s now use %s: commit %s.", a.env.Dir, render.Safe(givenText(g)), path)
	a.progress("--org and --project, and TLM_ORG and TLM_PROJECT, still win.")
	return nil
}

// parseUse reads <org>[/<project>].
func parseUse(arg string) (target.Given, error) {
	org, project, _ := strings.Cut(arg, "/")
	if org == "" || strings.Contains(project, "/") {
		return target.Given{}, usageError{fmt.Errorf("%q: expected <org> or <org>/<project>", arg)}
	}
	return target.Given{Org: org, Project: project}, nil
}

// checkUse checks the organization or project exists, and that this
// token can see it.
func (a *app) checkUse(cmd *cobra.Command, c *api.Client, g target.Given) error {
	path := api.OrgPath(g.Org)
	if g.Project != "" {
		path = api.ProjectPath(g.Org, g.Project)
	}
	_, err := c.Get(cmd.Context(), path, nil, nil)
	return err
}

func givenText(g target.Given) string {
	return defaultText(auth.Default{Org: g.Org, Project: g.Project})
}

func defaultText(d auth.Default) string {
	if d.Project == "" {
		return d.Org
	}
	return d.Org + "/" + d.Project
}

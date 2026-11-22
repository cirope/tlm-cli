package cli

import (
	"errors"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newGuideCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "How to add Tlm to an app, step by step, for an AI agent",
		Long: "Tlm's guide for AI agents, as Markdown: install and sign in, pick the\n" +
			"project, add Tlm to the app in its language (package, settings, speed,\n" +
			"source maps in CI), then check it worked. It holds nothing secret and\n" +
			"needs no sign-in. --lang picks English or Spanish.",
		Example: "  tlm guide\n" +
			"  tlm guide --lang es > TLM.md\n" +
			"  claude \"$(tlm guide)\"",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.flags.json {
				return usageError{errors.New("the guide is Markdown; drop --json")}
			}
			s, err := a.resolve()
			if err != nil {
				return err
			}
			c, err := api.New(api.Options{
				URL: s.url, Lang: s.lang, UserAgent: a.userAgent(), HTTPClient: a.env.HTTPClient,
				Logger: a.logger, Sleep: a.env.Sleep,
			})
			if err != nil {
				return usageError{err}
			}
			md, err := c.AgentGuide(cmd.Context())
			if err != nil {
				return err
			}
			a.out("%s", render.SafeText(strings.TrimRight(md, "\n")))
			return nil
		},
	}
}

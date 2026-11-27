package cli

import (
	"errors"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newGuideCmd(a *app) *cobra.Command {
	var stack string
	c := &cobra.Command{
		Use:   "guide",
		Short: "How to add Tlm to an app, step by step, for an AI agent",
		Long: "Tlm's guide for AI agents, as Markdown: install and sign in, pick the\n" +
			"project, add Tlm to the app in its stack (package, settings, speed, source\n" +
			"maps), then check it worked. It holds nothing secret and needs no\n" +
			"sign-in. --lang picks English or Spanish.\n\n" +
			"--stack shows one stack's part only (elixir, node, javascript, ruby or\n" +
			"other; several, comma-separated). Without it, the stack is read from\n" +
			"the current directory (Gemfile, mix.exs, package.json); with several or\n" +
			"none, the whole guide.",
		Example: "  tlm guide\n" +
			"  tlm guide --stack ruby\n" +
			"  tlm guide --stack node,javascript --lang es > TLM.md\n" +
			"  claude \"$(tlm guide)\"",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.flags.json {
				return usageError{errors.New("the guide is Markdown; drop --json")}
			}
			picked := detectStacks(a.env.Dir)
			if cmd.Flags().Changed("stack") {
				var err error
				if picked, err = parseStacks(stack); err != nil {
					return err
				}
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
			md, err := c.AgentGuide(cmd.Context(), picked)
			if err != nil {
				return err
			}
			a.out("%s", render.SafeText(strings.TrimRight(md, "\n")))
			return nil
		},
	}
	c.Flags().StringVar(&stack, "stack", "", "only this stack's part: elixir, node, javascript, ruby or other (several with commas; default: detected here)")
	return c
}

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// addYes adds --yes/-y to a destructive command.
func addYes(c *cobra.Command, yes *bool) {
	c.Flags().BoolVarP(yes, "yes", "y", false, "don't ask before doing it")
}

// confirm asks before a destructive change: yes skips the question;
// without a terminal to ask at, it's a usage mistake.
func (a *app) confirm(yes bool, question string) error {
	return a.ask(yes, question, "this can't be undone: pass --yes to do it without a terminal to ask at")
}

// ask is confirm with its own words for when there's no terminal.
func (a *app) ask(yes bool, question, noTerminal string) error {
	if yes {
		return nil
	}
	if !a.env.StdinIsTerminal {
		return usageError{errors.New(noTerminal)}
	}
	fmt.Fprintf(a.env.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(a.env.Stdin).ReadString('\n')
	if ans := strings.ToLower(strings.TrimSpace(line)); ans == "y" || ans == "yes" {
		return nil
	}
	fmt.Fprintln(a.env.Stderr, "Nothing changed.")
	return silentError{ExitError}
}

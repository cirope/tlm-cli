//go:build live

package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestLive drives tlm against a real Tlm, never in the
// default run:
//
//	TLM_LIVE_URL=http://localhost:4000 TLM_LIVE_TOKEN=tlmp_… \
//	TLM_LIVE_ORG=acme TLM_LIVE_PROJECT=shop \
//	  go test -tags live -run TestLive ./cmd/tlm
//
// Use a local Tlm with seeded data, never tlm.hullop.com. Only
// reads, plus a dry-run upload: nothing in Tlm changes.
func TestLive(t *testing.T) {
	url, token := os.Getenv("TLM_LIVE_URL"), os.Getenv("TLM_LIVE_TOKEN")
	org, project := os.Getenv("TLM_LIVE_ORG"), os.Getenv("TLM_LIVE_PROJECT")
	if url == "" || token == "" || org == "" || project == "" {
		t.Skip("set TLM_LIVE_URL, TLM_LIVE_TOKEN, TLM_LIVE_ORG and TLM_LIVE_PROJECT")
	}
	testscript.Run(t, testscript.Params{
		Dir: "testdata/live",
		Setup: func(env *testscript.Env) error {
			env.Setenv("TLM_URL", url)
			env.Setenv("TLM_TOKEN", token)
			env.Setenv("TLM_ORG", org)
			env.Setenv("TLM_PROJECT", project)
			env.Setenv("TLM_NO_UPDATE_NOTIFIER", "1")
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){"exits": exits},
	})
}

package cli

import (
	"encoding/json"
	"testing"

	"github.com/cirope/tlm-cli/internal/api"
)

func TestAuditWhoAndVia(t *testing.T) {
	viaAPI := "api"
	laptop := "laptop"
	tests := []struct {
		name     string
		entry    api.Audit
		who, via string
	}{
		{"a person on the web", api.Audit{Actor: &api.User{Name: "Ada", LastName: "Lovelace"}, Origin: "user"}, "Ada Lovelace", "web"},
		{"a person with a token", api.Audit{Actor: &api.User{Name: "Ada"}, Origin: "user", Via: &viaAPI, TokenName: &laptop}, "Ada", "API: laptop"},
		{"Tlm itself", api.Audit{Origin: "system"}, "System", ""},
		{"an account since deleted", api.Audit{Origin: "user", Via: &viaAPI}, "—", "API"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actorText(&tt.entry); got != tt.who {
				t.Errorf("actorText = %q, want %q", got, tt.who)
			}
			if got := viaText(&tt.entry); got != tt.via {
				t.Errorf("viaText = %q, want %q", got, tt.via)
			}
		})
	}
}

func TestChangesText(t *testing.T) {
	got := changesText(map[string]json.RawMessage{
		"name":         json.RawMessage(`[null, "Shop"]`),
		"local_access": json.RawMessage(`[true, false]`),
		"slug":         json.RawMessage(`"shop"`),
	})
	want := "local_access: true → false; name: — → Shop; slug: shop"
	if got != want {
		t.Errorf("changesText = %q, want %q", got, want)
	}
}

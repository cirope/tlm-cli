package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newAPICmd(a *app) *cobra.Command {
	var (
		method string
		fields []string
		input  string
	)
	c := &cobra.Command{
		Use:   "api <path>",
		Short: "A raw, authenticated request to Tlm's API",
		Long: "A raw request under /api/v1, signed in as you, for anything without a\n" +
			"command of its own. JSON answers are printed as is.\n\n" +
			"-f key=value adds a query parameter to a GET, or a field of the JSON body\n" +
			"otherwise. --input sends a file (- for stdin) as the body.\n\n" +
			"The endpoints: https://tlm.hullop.com/docs/api",
		Example: "  tlm api /me\n" +
			"  tlm api /orgs/acme/projects -f limit=100\n" +
			"  tlm api /orgs/acme/projects/shop/issues/2C7ZffP787kc -X PATCH -f status=resolved",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			method = strings.ToUpper(method)
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				return usageError{fmt.Errorf("unknown method %q: use GET, POST, PUT, PATCH or DELETE", method)}
			}
			path := "/" + strings.TrimPrefix(strings.TrimPrefix(args[0], "/"), "api/v1/")
			if strings.ContainsAny(path, "?#") {
				return usageError{errors.New("pass query parameters with -f key=value, not in the path")}
			}
			kv := map[string]string{}
			for _, f := range fields {
				k, v, ok := strings.Cut(f, "=")
				if !ok || k == "" {
					return usageError{fmt.Errorf("-f %q: expected key=value", f)}
				}
				kv[k] = v
			}
			if input != "" && len(kv) > 0 && method != http.MethodGet {
				return usageError{errors.New("-f and --input don't go together; put the fields in the file")}
			}
			var (
				q    api.Query
				body []byte
			)
			switch {
			case method == http.MethodGet:
				if input != "" {
					return usageError{errors.New("a GET has no body; drop --input")}
				}
				q = kv
			case input != "":
				r := a.env.Stdin
				if input != "-" {
					f, err := os.Open(input) //nolint:gosec // The person names the file to send.
					if err != nil {
						return usageError{err}
					}
					defer func() { _ = f.Close() }()
					r = f
				}
				b, err := io.ReadAll(io.LimitReader(r, 10<<20))
				if err != nil {
					return err
				}
				body = b
			case len(kv) > 0:
				b, err := json.Marshal(kv)
				if err != nil {
					return err
				}
				body = b
			}
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			raw, err := cl.SendRaw(cmd.Context(), method, path, q, body)
			if err != nil {
				return err
			}
			if len(raw) == 0 {
				return nil
			}
			if json.Valid(raw) {
				return a.printJSON(raw)
			}
			a.out("%s", render.SafeText(strings.TrimRight(string(raw), "\n")))
			return nil
		},
	}
	f := c.Flags()
	f.StringVarP(&method, "method", "X", http.MethodGet, "the HTTP method")
	f.StringArrayVarP(&fields, "field", "f", nil, "key=value: a query parameter for GET, a JSON body field otherwise")
	f.StringVar(&input, "input", "", "a file to send as the body (- for stdin)")
	return c
}

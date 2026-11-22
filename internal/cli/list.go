package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/spf13/cobra"
)

// listFlags are --limit, --cursor and --all.
type listFlags struct {
	limit  int
	cursor string
	all    bool
}

func (l *listFlags) add(c *cobra.Command) {
	c.Flags().IntVar(&l.limit, "limit", 0, "how many per page, 1 to 100 (default 50)")
	c.Flags().StringVar(&l.cursor, "cursor", "", "the page to show, from a More: line")
	c.Flags().BoolVar(&l.all, "all", false, "every page, not just one")
}

// query adds limit and cursor to q.
func (l *listFlags) query(q api.Query) (api.Query, error) {
	if q == nil {
		q = api.Query{}
	}
	if l.limit != 0 {
		if l.limit < 1 || l.limit > 100 {
			return nil, usageError{errors.New("--limit must be 1 to 100")}
		}
		q["limit"] = strconv.Itoa(l.limit)
	}
	if l.cursor != "" {
		q["cursor"] = l.cursor
	}
	return q, nil
}

// printList fetches path's list (one page, or every page with --all) and
// prints it: Tlm's JSON, the table, or the empty line. A More:
// hint for the next page goes to stderr.
func printList[T any](ctx context.Context, a *app, c *api.Client, l *listFlags, path string, q api.Query,
	table func([]T) string, empty string,
) error {
	q, err := l.query(q)
	if err != nil {
		return err
	}
	page, err := api.List[T](ctx, c, path, q, l.all)
	if err != nil {
		return err
	}
	if a.flags.json {
		return a.printJSON(page.JSON(l.all))
	}
	if len(page.Items) == 0 {
		a.out("%s", empty)
	} else {
		a.out("%s", table(page.Items))
	}
	if page.NextCursor != "" {
		fmt.Fprintf(a.env.Stderr, "More: --cursor %s\n", page.NextCursor)
	}
	return nil
}

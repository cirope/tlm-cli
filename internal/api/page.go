package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Page is one page of a list, or every page with all.
type Page[T any] struct {
	Items      []T
	NextCursor string // "" on the last page.

	raw   []byte            // The first page's answer, as is.
	items []json.RawMessage // Every item, as is.
}

// JSON is the list as Tlm's JSON: the page as it came, or with all
// every item as {"data": […]}.
func (p *Page[T]) JSON(all bool) []byte {
	if !all {
		return p.raw
	}
	var b bytes.Buffer
	b.WriteString(`{"data":[`)
	for i, it := range p.items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(it)
	}
	b.WriteString(`]}`)
	return b.Bytes()
}

type rawPage struct {
	Data       []json.RawMessage `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

// List fetches path's list: one page, or with all every page, following
// next_cursor. q may carry limit and cursor.
func List[T any](ctx context.Context, c *Client, path string, q Query, all bool) (*Page[T], error) {
	p := &Page[T]{}
	q = clone(q)
	seen := map[string]bool{}
	for {
		var rp rawPage
		raw, err := c.Get(ctx, path, q, &rp)
		if err != nil {
			return nil, err
		}
		if p.raw == nil {
			p.raw = raw
		}
		for _, it := range rp.Data {
			var v T
			if err := json.Unmarshal(it, &v); err != nil {
				return nil, &Error{Status: http.StatusOK, Code: "http_error", Message: "Tlm's list isn't the JSON expected (" + err.Error() + ")"}
			}
			p.Items = append(p.Items, v)
			p.items = append(p.items, it)
		}
		// A page without next_cursor (an older server, a proxy) is the
		// last one, not a loop back to the first.
		next := ""
		if rp.NextCursor != nil {
			next = *rp.NextCursor
		}
		if !all || next == "" {
			p.NextCursor = next
			if all {
				p.NextCursor = ""
			}
			return p, nil
		}
		if seen[next] {
			return nil, &Error{Status: http.StatusOK, Code: "http_error", Message: "Tlm sent the same page cursor twice; stopping"}
		}
		seen[next] = true
		q["cursor"] = next
	}
}

func clone(q Query) Query {
	out := Query{}
	for k, v := range q {
		out[k] = v
	}
	return out
}

package api

import (
	"context"
	"net/http"
	"net/url"
)

// Seg escapes one path segment: a slug, an id.
func Seg(s string) string { return url.PathEscape(s) }

// OrgPath is /orgs/:org.
func OrgPath(org string) string { return "/orgs/" + Seg(org) }

// ProjectPath is /orgs/:org/projects/:project.
func ProjectPath(org, project string) string {
	return OrgPath(org) + "/projects/" + Seg(project)
}

// Me is GET /me.
func (c *Client) Me(ctx context.Context) (*Me, []byte, error) {
	var me Me
	raw, err := c.Get(ctx, "/me", nil, &me)
	if err != nil {
		return nil, nil, err
	}
	return &me, raw, nil
}

// RevokeToken is DELETE /me/token: the token making the call.
func (c *Client) RevokeToken(ctx context.Context) error {
	_, err := c.Send(ctx, http.MethodDelete, "/me/token", nil, nil, nil)
	return err
}

// Projects lists an organization's projects.
func (c *Client) Projects(ctx context.Context, org string, q Query, all bool) (*Page[Project], error) {
	return List[Project](ctx, c, OrgPath(org)+"/projects", q, all)
}

// Project is one project, with its DSN.
func (c *Client) Project(ctx context.Context, org, project string) (*Project, []byte, error) {
	var p Project
	raw, err := c.Get(ctx, ProjectPath(org, project), nil, &p)
	if err != nil {
		return nil, nil, err
	}
	return &p, raw, nil
}

// CreateProject creates a project; the answer carries its DSN.
func (c *Client) CreateProject(ctx context.Context, org, name string) (*Project, []byte, error) {
	var p Project
	raw, err := c.Send(ctx, http.MethodPost, OrgPath(org)+"/projects", nil, map[string]string{"name": name}, &p)
	if err != nil {
		return nil, nil, err
	}
	return &p, raw, nil
}

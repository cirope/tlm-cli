package api

import "encoding/json"

// User is a person, as /me and the device flow show them. Name is the
// first name; LastName may be empty.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastName string `json:"last_name"`
	Email    string `json:"email"`
}

// FullName is the first and last name, as given.
func (u User) FullName() string { return fullName(u.Name, u.LastName) }

func fullName(first, last string) string {
	if first == "" || last == "" {
		return first + last
	}
	return first + " " + last
}

func unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Me is GET /me: who the token is.
type Me struct {
	User          User           `json:"user"`
	Token         TokenInfo      `json:"token"`
	Organizations []Organization `json:"organizations"`
}

// TokenInfo is the token's name, access ("write" or "read"), how it was
// approved ("local" or "sso") and the organization it's narrowed to (""
// when not narrowed).
type TokenInfo struct {
	Name         string `json:"name"`
	Access       string `json:"access"`
	Approval     string `json:"approval"`
	Organization string `json:"organization"`
}

// Organization is one of the person's organizations, with their role.
type Organization struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Project is a project as lists and changes show it; DSN and Muted come
// with one project.
type Project struct {
	ID              string   `json:"id"`
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	Platforms       []string `json:"platforms"`
	UnresolvedCount int      `json:"unresolved_count"`
	CreatedAt       string   `json:"created_at"`
	URL             string   `json:"url"`
	DSN             *string  `json:"dsn"`
	Muted           *bool    `json:"muted"`
}

// UploadToken is a project's token for source map uploads; Secret is
// set only in the answer to creating one.
type UploadToken struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Hint       string  `json:"hint"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedBy  *User   `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	Secret     string  `json:"secret"`
}

// Origin is an allowed domain: pending, allowed or blocked.
type Origin struct {
	Origin    string `json:"origin"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// Issue is an issue as lists, one issue and changes show it.
// EventsInRange is only in lists; LatestEvent only in one issue.
type Issue struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Subtitle      string  `json:"subtitle"`
	Culprit       string  `json:"culprit"`
	Level         string  `json:"level"`
	Platform      string  `json:"platform"`
	Status        string  `json:"status"`
	Assignee      *User   `json:"assignee"`
	EventsCount   int     `json:"events_count"`
	UsersCount    int     `json:"users_count"`
	EventsInRange *int    `json:"events_in_range"`
	FirstSeenAt   string  `json:"first_seen_at"`
	LastSeenAt    string  `json:"last_seen_at"`
	FirstRelease  *string `json:"first_release"`
	LastRelease   *string `json:"last_release"`
	URL           string  `json:"url"`
	LatestEvent   *Event  `json:"latest_event"`
}

// Event is one event of an issue; Payload only when shown in full, and
// null for events not kept.
type Event struct {
	UUID        string        `json:"uuid"`
	OccurredAt  string        `json:"occurred_at"`
	ReceivedAt  string        `json:"received_at"`
	Environment *string       `json:"environment"`
	Release     *string       `json:"release"`
	Kept        bool          `json:"kept"`
	Payload     *EventPayload `json:"payload"`
}

// EventPayload is what the app sent, as Tlm stored it. Only the
// parts the CLI shows are typed.
type EventPayload struct {
	Exception *struct {
		Type    string  `json:"type"`
		Message string  `json:"message"`
		Frames  []Frame `json:"frames"`
	} `json:"exception"`
	Request *struct {
		Method string `json:"method"`
		URL    string `json:"url"`
	} `json:"request"`
	Tags map[string]any `json:"tags"`
}

// Frame is one stack frame.
type Frame struct {
	File     string `json:"file"`
	Line     *int   `json:"line"`
	Column   *int   `json:"column"`
	Function string `json:"function"`
	Module   string `json:"module"`
	InApp    bool   `json:"in_app"`
}

// Member is someone in an organization.
type Member struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastName string `json:"last_name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	// LocalAccess is whether they can enter without single sign-on.
	LocalAccess bool `json:"local_access"`
}

// FullName is the first and last name, as given.
func (m Member) FullName() string { return fullName(m.Name, m.LastName) }

// Org is an organization, with the caller's role in it.
type Org struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

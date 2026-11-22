package api

import "encoding/json"

// Vital is one Web Vital over a speed query: its p75 (ms, or unitless
// for CLS; nil with no samples), the shares of rated page views that
// were good, need work or poor, and how many page views have it.
type Vital struct {
	P75       *float64 `json:"p75"`
	Good      float64  `json:"good"`
	NeedsWork float64  `json:"needs_work"`
	Poor      float64  `json:"poor"`
	Samples   int      `json:"samples"`
}

// SpeedOverview is GET …/speed: Web Vitals at p75 for one environment,
// period and device.
type SpeedOverview struct {
	Environment  *string          `json:"environment"`
	Environments []string         `json:"environments"`
	Period       string           `json:"period"`
	Device       *string          `json:"device"`
	Views        float64          `json:"views"`
	Dropped      float64          `json:"dropped"`
	Vitals       map[string]Vital `json:"vitals"`
}

// SpeedPage is one row of GET …/speed/pages, slowest first.
type SpeedPage struct {
	Route      string   `json:"route"`
	Navigation string   `json:"navigation"` // hard or soft.
	Views      float64  `json:"views"`
	LCP        *float64 `json:"lcp"`
	Nav        *float64 `json:"nav"`
	INP        *float64 `json:"inp"`
}

// SpeedRequest is one row of GET …/speed/requests, slowest p75 first.
type SpeedRequest struct {
	Method string   `json:"method"`
	Route  string   `json:"route"`
	Calls  float64  `json:"calls"`
	Failed float64  `json:"failed"` // Share with status 0 or 5xx.
	P75    *float64 `json:"p75"`
	Start  *float64 `json:"start"`
}

// SpeedPageDetail is GET …/speed/page?route=…: one page.
type SpeedPageDetail struct {
	Route      string           `json:"route"`
	Views      float64          `json:"views"`
	Vitals     map[string]Vital `json:"vitals"`
	Devices    []SpeedDevice    `json:"devices"`
	SlowVisits []SlowVisit      `json:"slow_visits"`
}

// SpeedDevice is a page's views from one kind of device.
type SpeedDevice struct {
	Value string   `json:"value"`
	Views float64  `json:"views"`
	Share float64  `json:"share"`
	LCP   *float64 `json:"lcp"`
	Nav   *float64 `json:"nav"`
}

// SlowVisit is a page view with a poor LCP or navigation.
type SlowVisit struct {
	Session string   `json:"session"`
	ID      string   `json:"id"`
	At      string   `json:"at"`
	Device  *string  `json:"device"`
	Browser *string  `json:"browser"`
	Country *string  `json:"country"`
	Release *string  `json:"release"`
	LCP     *float64 `json:"lcp"`
	Nav     *float64 `json:"nav"`
}

// SpeedRequestDetail is GET …/speed/request?method=…&route=…: one
// endpoint, the pages that call it and its slowest calls.
type SpeedRequestDetail struct {
	Method    string             `json:"method"`
	Route     string             `json:"route"`
	Pages     []SpeedRequestPage `json:"pages"`
	SlowCalls []SlowCall         `json:"slow_calls"`
}

// SpeedRequestPage is a page calling an endpoint, with the page's own
// LCP and navigation to set the call against.
type SpeedRequestPage struct {
	Route  string   `json:"route"`
	Calls  float64  `json:"calls"`
	Failed float64  `json:"failed"`
	P75    *float64 `json:"p75"`
	Start  *float64 `json:"start"`
	LCP    *float64 `json:"lcp"`
	Nav    *float64 `json:"nav"`
}

// SlowCall is one slow call to an endpoint.
type SlowCall struct {
	Session  string   `json:"session"`
	ID       string   `json:"id"`
	At       string   `json:"at"`
	Page     *string  `json:"page"`
	Device   *string  `json:"device"`
	Browser  *string  `json:"browser"`
	Country  *string  `json:"country"`
	Status   *int     `json:"status"`
	Duration *float64 `json:"duration"`
}

// Signal is a sign of life: which servers and browsers report, one per
// environment and source.
type Signal struct {
	Environment *string        `json:"environment"`
	Source      string         `json:"source"` // server or browser.
	Status      string         `json:"status"` // active, quiet or idle.
	LastSeenAt  *string        `json:"last_seen_at"`
	SDKName     *string        `json:"sdk_name"`
	SDKVersion  *string        `json:"sdk_version"`
	Release     *string        `json:"release"`
	Server      *string        `json:"server"`
	Runtime     map[string]any `json:"runtime"`
	HeartbeatAt *string        `json:"heartbeat_at"`
}

// Audit is one entry of an organization's audit log: an Event (create,
// update or destroy) on an item. Actor is nil for Tlm itself (Origin
// "system") or an account since deleted; Via is "api" for changes made
// with a token, named by TokenName. Changes are [before, after] pairs.
type Audit struct {
	ID        string                     `json:"id"`
	ItemType  string                     `json:"item_type"`
	ItemID    string                     `json:"item_id"`
	Event     string                     `json:"event"`
	Actor     *User                      `json:"actor"`
	Origin    string                     `json:"origin"`
	Via       *string                    `json:"via"`
	TokenName *string                    `json:"token_name"`
	Changes   map[string]json.RawMessage `json:"changes"`
	CreatedAt string                     `json:"created_at"`
}

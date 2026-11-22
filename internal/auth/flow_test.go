package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cirope/tlm-cli/internal/api"
)

// fakeDevice answers PollLogin from a script ("" = approved, a code =
// that RFC 8628 error, "502"/"429" = that status, "net" = a network
// error) and records when each poll happened.
type fakeDevice struct {
	code   api.DeviceCode
	script []string
	polls  []time.Time
	start  time.Time
	names  []string
}

func (f *fakeDevice) StartLogin(_ context.Context, name string) (*api.DeviceCode, error) {
	f.start = time.Now()
	f.names = append(f.names, name)
	c := f.code
	return &c, nil
}

func (f *fakeDevice) PollLogin(_ context.Context, deviceCode api.Token) (*api.Grant, error) {
	if deviceCode != f.code.DeviceCode {
		return nil, errors.New("wrong device code")
	}
	f.polls = append(f.polls, time.Now())
	next := f.script[0]
	if len(f.script) > 1 {
		f.script = f.script[1:]
	}
	switch next {
	case "":
		return &api.Grant{Token: "tlmp_test", User: api.User{Email: "ada@example.com"}}, nil
	case "502":
		return nil, &api.Error{Status: http.StatusBadGateway, Code: "http_error", Message: "HTTP 502 Bad Gateway"}
	case "429":
		return nil, &api.Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "Slow down.", RetryAfter: 30 * time.Second}
	case "net":
		return nil, &api.Error{Code: "network", Message: "couldn't reach"}
	case "forbidden":
		return nil, &api.Error{Status: http.StatusForbidden, Code: "forbidden", Message: "No."}
	}
	return nil, &api.Error{Status: http.StatusBadRequest, Code: next, Message: next}
}

// offsets returns each poll's time since the flow started, in seconds.
func (f *fakeDevice) offsets() []int {
	var out []int
	for _, p := range f.polls {
		out = append(out, int(p.Sub(f.start)/time.Second))
	}
	return out
}

func newFake(script ...string) *fakeDevice {
	return &fakeDevice{
		code: api.DeviceCode{
			DeviceCode: "dc_test", UserCode: "WDJB-MJHT",
			VerificationURI:         "https://tlm.test/login/device",
			VerificationURIComplete: "https://tlm.test/login/device?code=WDJBMJHT",
			Interval:                5, ExpiresIn: 600,
		},
		script: script,
	}
}

func login(t *testing.T, f *fakeDevice) (*api.Grant, error) {
	t.Helper()
	var shown *api.DeviceCode
	g, err := Login(t.Context(), f, "tlm-cli dev on test-host", func(c *api.DeviceCode) { shown = c }, nil)
	if shown == nil || shown.UserCode != "WDJB-MJHT" {
		t.Errorf("shown = %+v, want the code shown before polling", shown)
	}
	return g, err
}

func TestLoginPollsUntilApproved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("authorization_pending", "authorization_pending", "")
		g, err := login(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if g.Token.Reveal() != "tlmp_test" {
			t.Error("wrong token")
		}
		if got := f.offsets(); !slices.Equal(got, []int{5, 10, 15}) {
			t.Errorf("polled at %v s, want every 5 s", got)
		}
		if !slices.Equal(f.names, []string{"tlm-cli dev on test-host"}) {
			t.Errorf("client names = %q", f.names)
		}
	})
}

func TestLoginSlowsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("slow_down", "authorization_pending", "")
		if _, err := login(t, f); err != nil {
			t.Fatal(err)
		}
		// slow_down adds 5 seconds to the interval for good.
		if got := f.offsets(); !slices.Equal(got, []int{5, 15, 25}) {
			t.Errorf("polled at %v s, want 5, 15, 25", got)
		}
	})
}

// A busy server or a proxy's hiccup waits longer instead of ending
// the login; a 429 waits at least its Retry-After.
func TestLoginWaitsOutServerTrouble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("502", "net", "429", "")
		if _, err := login(t, f); err != nil {
			t.Fatal(err)
		}
		// 502: +5 (10). Network: same interval (10). 429: +5 (15), but
		// Retry-After asks 30.
		if got := f.offsets(); !slices.Equal(got, []int{5, 15, 25, 55}) {
			t.Errorf("polled at %v s, want 5, 15, 25, 55", got)
		}
	})
}

func TestLoginDefaultsAMissingInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("")
		f.code.Interval = 0
		if _, err := login(t, f); err != nil {
			t.Fatal(err)
		}
		if got := f.offsets(); !slices.Equal(got, []int{5}) {
			t.Errorf("polled at %v s, want 5", got)
		}
	})
}

func TestLoginEnds(t *testing.T) {
	tests := []struct {
		name   string
		script []string
		want   error
	}{
		{"denied", []string{"authorization_pending", "access_denied"}, ErrDenied},
		{"expired by the server", []string{"expired_token"}, ErrExpired},
		{"expired by the clock", []string{"authorization_pending"}, ErrExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newFake(tt.script...)
				_, err := login(t, f)
				if !errors.Is(err, tt.want) {
					t.Errorf("err = %v, want %v", err, tt.want)
				}
				if len(f.polls) > 120 {
					t.Errorf("polled %d times after the code expired", len(f.polls))
				}
			})
		})
	}
}

func TestLoginStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("authorization_pending")
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(7*time.Second, cancel)
		_, err := Login(ctx, f, "h", func(*api.DeviceCode) {}, nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if len(f.polls) != 1 {
			t.Errorf("polled %d times, want 1 before the cancel at 7 s", len(f.polls))
		}
	})
}

func TestLoginOtherRefusalsEndTheFlow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake("forbidden")
		_, err := login(t, f)
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "forbidden" {
			t.Errorf("err = %v, want the server's error", err)
		}
	})
}

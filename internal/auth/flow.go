// Package auth signs this machine in to Tlm with the device flow
// (RFC 8628) and keeps the resulting tokens.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cirope/tlm-cli/internal/api"
)

// Errors that end signing in.
var (
	ErrDenied  = errors.New("the login was denied in the browser")
	ErrExpired = errors.New("the code expired before it was approved; run tlm login again")
)

const (
	defaultInterval = 5 * time.Second
	slowDownStep    = 5 * time.Second
	defaultExpiry   = 10 * time.Minute
)

// Device is the part of the API the device flow needs.
type Device interface {
	StartLogin(ctx context.Context, clientName string) (*api.DeviceCode, error)
	PollLogin(ctx context.Context, deviceCode api.Token) (*api.Grant, error)
}

// Login asks for a device code, calls show so the person can approve it,
// then polls until it is approved, denied, expired, or ctx is done.
// sleep waits between polls (nil: api.Sleep).
func Login(ctx context.Context, d Device, clientName string, show func(*api.DeviceCode), sleep func(context.Context, time.Duration) error) (*api.Grant, error) {
	if sleep == nil {
		sleep = api.Sleep
	}
	code, err := d.StartLogin(ctx, clientName)
	if err != nil {
		return nil, err
	}
	show(code)

	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	expiry := time.Duration(code.ExpiresIn) * time.Second
	if expiry <= 0 {
		expiry = defaultExpiry
	}
	deadline := time.Now().Add(expiry)
	wait := interval

	for {
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, ErrExpired
		}
		grant, err := d.PollLogin(ctx, code.DeviceCode)
		if err == nil {
			return grant, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var e *api.Error
		if !errors.As(err, &e) {
			return nil, fmt.Errorf("waiting for approval: %w", err)
		}
		switch {
		case e.Code == "network":
			// A blip while waiting: try again at the next interval.
		case e.Status == http.StatusTooManyRequests || e.Status >= 500:
			// A busy server or a proxy hiccup: wait longer.
			interval += slowDownStep
		case e.Code == "authorization_pending":
		case e.Code == "slow_down":
			interval += slowDownStep
		case e.Code == "access_denied":
			return nil, ErrDenied
		case e.Code == "expired_token":
			return nil, ErrExpired
		default:
			return nil, err
		}
		wait = max(interval, e.RetryAfter)
	}
}

package api

import (
	"context"
	"net/http"
)

// DeviceCode is the answer to POST /login/device. DeviceCode is a secret:
// never printed, only sent back while polling.
type DeviceCode struct {
	DeviceCode              Token  `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// UnmarshalJSON reads the device code into a Token, which only marshals
// redacted.
func (d *DeviceCode) UnmarshalJSON(b []byte) error {
	var raw struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := unmarshal(b, &raw); err != nil {
		return err
	}
	*d = DeviceCode{Token(raw.DeviceCode), raw.UserCode, raw.VerificationURI, raw.VerificationURIComplete, raw.ExpiresIn, raw.Interval}
	return nil
}

// Grant is the answer to an approved POST /login/token.
type Grant struct {
	Token Token
	User  User
}

// UnmarshalJSON reads the token into a Token.
func (g *Grant) UnmarshalJSON(b []byte) error {
	var raw struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}
	if err := unmarshal(b, &raw); err != nil {
		return err
	}
	*g = Grant{Token(raw.Token), raw.User}
	return nil
}

// StartLogin starts a device login (POST /login/device).
func (c *Client) StartLogin(ctx context.Context, clientName string) (*DeviceCode, error) {
	var d DeviceCode
	if _, err := c.Send(ctx, http.MethodPost, "/login/device", nil, map[string]string{"client_name": clientName}, &d); err != nil {
		return nil, err
	}
	if d.DeviceCode == "" || d.UserCode == "" || d.VerificationURI == "" {
		return nil, &Error{Status: http.StatusOK, Code: "http_error", Message: "Tlm's login answer is missing fields; is this a Tlm URL?"}
	}
	return &d, nil
}

// PollLogin asks whether the device login was approved (POST
// /login/token). Until then it fails with an *Error whose Code is RFC
// 8628's: authorization_pending, slow_down, access_denied or
// expired_token.
func (c *Client) PollLogin(ctx context.Context, deviceCode Token) (*Grant, error) {
	var g Grant
	if _, err := c.Send(ctx, http.MethodPost, "/login/token", nil, map[string]string{"device_code": deviceCode.Reveal()}, &g); err != nil {
		return nil, err
	}
	if g.Token == "" {
		return nil, &Error{Status: http.StatusOK, Code: "http_error", Message: "Tlm's login answer has no token."}
	}
	return &g, nil
}

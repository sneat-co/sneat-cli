package sneatauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client calls the Firebase Auth REST API (Identity Toolkit + Secure Token).
type Client struct {
	http            *http.Client
	apiKey          string
	identityBase    string
	secureTokenBase string
}

// Options configures a Client. When AuthEmulatorHost is set the REST bases are
// rewritten to the local Auth emulator.
type Options struct {
	APIKey           string
	AuthEmulatorHost string
	HTTPClient       *http.Client
}

// New builds a Client for prod or the Auth emulator.
func New(o Options) *Client {
	identity := "https://identitytoolkit.googleapis.com/v1"
	secure := "https://securetoken.googleapis.com/v1"
	if o.AuthEmulatorHost != "" {
		identity = "http://" + o.AuthEmulatorHost + "/identitytoolkit.googleapis.com/v1"
		secure = "http://" + o.AuthEmulatorHost + "/securetoken.googleapis.com/v1"
	}
	return newWithBases(identity, secure, o.APIKey, o.HTTPClient)
}

func newWithBases(identityBase, secureTokenBase, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{http: hc, apiKey: apiKey, identityBase: identityBase, secureTokenBase: secureTokenBase}
}

// Result is a normalized Firebase auth response.
type Result struct {
	IDToken      string
	RefreshToken string
	UID          string
	Email        string
	ExpiresIn    time.Duration
}

// SignInWithPassword signs a user in with email + password.
func (c *Client) SignInWithPassword(ctx context.Context, email, password string) (Result, error) {
	body, _ := json.Marshal(map[string]any{"email": email, "password": password, "returnSecureToken": true})
	u := c.identityBase + "/accounts:signInWithPassword?key=" + url.QueryEscape(c.apiKey)
	var out struct {
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		LocalID      string `json:"localId"`
		Email        string `json:"email"`
		ExpiresIn    string `json:"expiresIn"`
	}
	if err := c.doJSON(ctx, u, "application/json", bytes.NewReader(body), &out); err != nil {
		return Result{}, err
	}
	return Result{
		IDToken: out.IDToken, RefreshToken: out.RefreshToken, UID: out.LocalID,
		Email: out.Email, ExpiresIn: parseSeconds(out.ExpiresIn),
	}, nil
}

// Refresh exchanges a refresh token for a fresh ID token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Result, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	u := c.secureTokenBase + "/token?key=" + url.QueryEscape(c.apiKey)
	var out struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		ExpiresIn    string `json:"expires_in"`
	}
	if err := c.doJSON(ctx, u, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &out); err != nil {
		return Result{}, err
	}
	return Result{
		IDToken: out.IDToken, RefreshToken: out.RefreshToken, UID: out.UserID,
		ExpiresIn: parseSeconds(out.ExpiresIn),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, endpoint, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error.Message != "" {
			return fmt.Errorf("firebase auth: %s", e.Error.Message)
		}
		return fmt.Errorf("firebase auth: http %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func parseSeconds(s string) time.Duration {
	n, _ := strconv.Atoi(s)
	return time.Duration(n) * time.Second
}

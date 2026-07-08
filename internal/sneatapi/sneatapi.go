// Package sneatapi is a thin client for the sneat-go HTTP API (mutations),
// authenticated with the user's Firebase ID token.
package sneatapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sneat-co/contactus/backend/dto4contactus"
	"golang.org/x/oauth2"
)

// TokenSource yields the bearer token for API calls (an oauth2 token whose
// AccessToken is the Firebase ID token).
type TokenSource interface {
	Token() (*oauth2.Token, error)
}

// Client calls the sneat-go API under a base URL like https://api.sneat.cloud/v0/.
type Client struct {
	http    *http.Client
	baseURL string
	ts      TokenSource
}

// New builds a Client. baseURL is normalized to end with "/".
func New(baseURL string, ts TokenSource, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &Client{http: hc, baseURL: baseURL, ts: ts}
}

// CreateContact POSTs contactus/create_contact and returns the raw response.
func (c *Client) CreateContact(ctx context.Context, req dto4contactus.CreateContactRequest) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "contactus/create_contact", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteContact calls DELETE contactus/delete_contact.
func (c *Client) DeleteContact(ctx context.Context, req dto4contactus.ContactRequest) error {
	return c.do(ctx, http.MethodDelete, "contactus/delete_contact", req, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.ts != nil {
		tok, err := c.ts.Token()
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sneat api %s %s: http %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

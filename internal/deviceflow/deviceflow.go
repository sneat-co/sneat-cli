// Package deviceflow connects the shared RFC 8628 client flow to Sneat's
// Firebase session model.
package deviceflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/strongo/deviceauth"
	"golang.org/x/oauth2"
)

const (
	DefaultIssuer   = "https://auth.sneat.co"
	ClientID        = "sneat-cli"
	customTokenType = "urn:ietf:params:oauth:token-type:firebase-custom-token"
)

var Scopes = []string{"openid", "profile", "sneat:spaces:read", "sneat:spaces:write"}

// DeviceInfo returns platform metadata that the authorization service may show
// to the user before approving this CLI login.
func DeviceInfo(version string) deviceauth.DeviceInfo {
	hostname, _ := os.Hostname()
	return deviceauth.DeviceInfo{
		Name:          strings.TrimSpace(hostname),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		ClientVersion: strings.TrimSpace(version),
	}
}

// CustomTokenExchanger exchanges the one-use custom token for a normal
// Firebase session. It intentionally receives no durable device credential.
type CustomTokenExchanger interface {
	SignInWithCustomToken(context.Context, string) (sneatauth.Result, error)
}

// Options supplies the process dependencies needed by the flow.
type Options struct {
	Issuer      string
	HTTPClient  *http.Client
	OpenBrowser func(string) error
	Exchange    CustomTokenExchanger
	DeviceInfo  deviceauth.DeviceInfo
	Store       session.SessionStore
	Project     string
}

// Flow runs browser-approved login for Sneat CLI.
type Flow struct {
	options Options
	client  *deviceauth.Client
	store   deviceauth.Store
}

// New validates and returns a device flow.
func New(options Options) (*Flow, error) {
	if options.Issuer == "" {
		options.Issuer = DefaultIssuer
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.Exchange == nil {
		return nil, errors.New("device flow: Firebase custom-token exchanger is required")
	}
	if options.Store == nil {
		return nil, errors.New("device flow: credential store is required")
	}
	client, err := deviceauth.NewClient(deviceauth.ClientConfig{
		Issuer:         options.Issuer,
		ClientID:       ClientID,
		Scopes:         Scopes,
		RequiredScopes: Scopes,
		KeyringService: "sneat-cli",
		KeyringAccount: "firebase-session",
	})
	if err != nil {
		return nil, err
	}
	if options.DeviceInfo.OS == "" {
		options.DeviceInfo.OS = runtime.GOOS
	}
	if options.DeviceInfo.Arch == "" {
		options.DeviceInfo.Arch = runtime.GOARCH
	}
	return &Flow{
		options: options,
		client:  client,
		store:   session.NewDeviceAuthStore(options.Store, options.Project),
	}, nil
}

// Run performs RFC 8628 authorization, exchanges its one-use custom token,
// and verifies that auth.sneat.co bound the resulting Firebase identity to
// Sneat CLI's audience.
func (f *Flow) Run(ctx context.Context, output, errorOutput io.Writer) (sneatauth.Result, error) {
	loginContext := context.WithValue(ctx, oauth2.HTTPClient, f.options.HTTPClient)
	auth, err := f.client.DeviceLoginAndStore(loginContext, deviceauth.DeviceLoginOptions{
		DeviceInfo: f.options.DeviceInfo, OpenBrowser: f.options.OpenBrowser, Output: output, ErrorOutput: errorOutput,
		TokenTransformer: func(ctx context.Context, token *oauth2.Token) (*oauth2.Token, error) {
			if !strings.EqualFold(token.TokenType, customTokenType) {
				return nil, errors.New("authorization server returned an unexpected token type")
			}
			session, err := f.options.Exchange.SignInWithCustomToken(ctx, token.AccessToken)
			if err != nil {
				return nil, fmt.Errorf("exchange device login for Firebase session: %w", err)
			}
			return &oauth2.Token{AccessToken: session.IDToken, TokenType: "Bearer", RefreshToken: session.RefreshToken, Expiry: time.Now().Add(session.ExpiresIn)}, nil
		},
	}, f.store)
	if err != nil {
		return sneatauth.Result{}, err
	}
	return sneatauth.Result{
		IDToken: auth.SessionToken.AccessToken, RefreshToken: auth.SessionToken.RefreshToken,
		UID: auth.Identity.Subject, Email: auth.Identity.Email,
		ExpiresIn: time.Until(auth.SessionToken.Expiry), Warnings: auth.Warnings,
	}, nil
}

// Logout revokes the remote OAuth grant before deleting the selected local
// session. deviceauth deliberately preserves local credentials on failure.
func (f *Flow) Logout(ctx context.Context) error {
	if f == nil || f.client == nil || f.store == nil {
		return errors.New("device flow is not configured")
	}
	return f.client.Logout(ctx, f.store)
}

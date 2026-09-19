// Package deviceflow connects the shared RFC 8628 client flow to Sneat's
// Firebase session model.
package deviceflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strings"

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
}

// Flow runs browser-approved login for Sneat CLI.
type Flow struct{ options Options }

// New validates and returns a device flow.
func New(options Options) (*Flow, error) {
	if options.Issuer == "" {
		options.Issuer = DefaultIssuer
	}
	if _, err := issuerURL(options.Issuer); err != nil {
		return nil, err
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.Exchange == nil {
		return nil, errors.New("device flow: Firebase custom-token exchanger is required")
	}
	if options.DeviceInfo.OS == "" {
		options.DeviceInfo.OS = runtime.GOOS
	}
	if options.DeviceInfo.Arch == "" {
		options.DeviceInfo.Arch = runtime.GOARCH
	}
	return &Flow{options: options}, nil
}

// Run performs RFC 8628 authorization, exchanges its one-use custom token,
// and verifies that auth.sneat.co bound the resulting Firebase identity to
// Sneat CLI's audience.
func (f *Flow) Run(ctx context.Context, output, errorOutput io.Writer) (sneatauth.Result, error) {
	issuer, err := issuerURL(f.options.Issuer)
	if err != nil {
		return sneatauth.Result{}, err
	}
	loginContext := context.WithValue(ctx, oauth2.HTTPClient, f.options.HTTPClient)
	result, err := deviceauth.Login(loginContext, deviceauth.LoginOptions{
		OAuthConfig: oauth2.Config{
			ClientID: ClientID,
			Scopes:   Scopes,
			Endpoint: oauth2.Endpoint{
				DeviceAuthURL: issuer.String() + "/oauth/device/code",
				TokenURL:      issuer.String() + "/oauth/token",
				AuthStyle:     oauth2.AuthStyleInParams,
			},
		},
		DeviceInfo:  f.options.DeviceInfo,
		OpenBrowser: f.options.OpenBrowser,
		Output:      output,
		ErrorOutput: errorOutput,
	})
	if err != nil {
		return sneatauth.Result{}, err
	}
	if !strings.EqualFold(result.Token.TokenType, customTokenType) {
		return sneatauth.Result{}, errors.New("device flow: authorization server returned an unexpected token type")
	}

	session, err := f.options.Exchange.SignInWithCustomToken(ctx, result.Token.AccessToken)
	if err != nil {
		return sneatauth.Result{}, fmt.Errorf("exchange device login for Firebase session: %w", err)
	}
	identity, err := fetchIdentity(ctx, f.options.HTTPClient, issuer, session.IDToken)
	if err != nil {
		return sneatauth.Result{}, err
	}
	if identity.Audience != ClientID || identity.Subject == "" {
		return sneatauth.Result{}, errors.New("device flow: identity is not authorized for sneat-cli")
	}
	if session.UID != "" && session.UID != identity.Subject {
		return sneatauth.Result{}, errors.New("device flow: Firebase identity does not match authorization identity")
	}
	session.UID = identity.Subject
	return session, nil
}

type identity struct {
	Subject  string `json:"sub"`
	Audience string `json:"aud"`
	Scope    string `json:"scope"`
}

func fetchIdentity(ctx context.Context, client *http.Client, issuer *url.URL, token string) (identity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer.String()+"/oauth/userinfo", nil)
	if err != nil {
		return identity{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return identity{}, fmt.Errorf("validate device identity: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return identity{}, fmt.Errorf("validate device identity: auth server returned HTTP %d", response.StatusCode)
	}
	var result identity
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return identity{}, fmt.Errorf("decode device identity: %w", err)
	}
	return result, nil
}

func issuerURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("--auth-host must be an absolute authorization server URL")
	}
	host := strings.ToLower(parsed.Hostname())
	loopback := host == "localhost"
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		loopback = address.IsLoopback()
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback) {
		return nil, errors.New("--auth-host must use HTTPS (HTTP is allowed only for loopback development)")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	return parsed, nil
}

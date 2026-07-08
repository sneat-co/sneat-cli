// Package browserauth signs a user in through the browser using the Firebase
// JS SDK served from a local loopback page, capturing the resulting tokens.
package browserauth

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed signin.html
var signinHTML string

// Result is the captured Firebase session from the browser sign-in.
type Result struct {
	IDToken      string
	RefreshToken string
	UID          string
	Email        string
	ExpiresIn    time.Duration
}

// Flow runs one browser sign-in. OpenBrowser is injected for testability.
type Flow struct {
	APIKey           string
	AuthDomain       string
	Project          string
	AuthEmulatorHost string
	OpenBrowser      func(url string) error
}

// renderPage returns the sign-in HTML with the Firebase config injected.
func (f Flow) renderPage() ([]byte, error) {
	cfg, err := json.Marshal(map[string]string{
		"apiKey":           f.APIKey,
		"authDomain":       f.AuthDomain,
		"project":          f.Project,
		"authEmulatorHost": f.AuthEmulatorHost,
	})
	if err != nil {
		return nil, err
	}
	return []byte(strings.Replace(signinHTML, "__SNEAT_CONFIG__", string(cfg), 1)), nil
}

type callbackPayload struct {
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
	UID          string `json:"uid"`
	Email        string `json:"email"`
	ExpiresIn    int    `json:"expiresIn"`
}

// Run serves the loopback page, opens the browser, and waits for the callback.
func (f Flow) Run(ctx context.Context) (Result, error) {
	page, err := f.renderPage()
	if err != nil {
		return Result{}, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, err
	}

	resultCh := make(chan Result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		var p callbackPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
		resultCh <- Result{
			IDToken: p.IDToken, RefreshToken: p.RefreshToken, UID: p.UID,
			Email: p.Email, ExpiresIn: time.Duration(p.ExpiresIn) * time.Second,
		}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Use "localhost" (a Firebase default-authorized domain) rather than the
	// listener's 127.0.0.1 literal, which is NOT authorized by default and
	// triggers auth/unauthorized-domain in signInWithPopup.
	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://localhost:%d/", port)
	if f.OpenBrowser == nil {
		return Result{}, errors.New("browserauth: OpenBrowser is nil")
	}
	if err := f.OpenBrowser(url); err != nil {
		return Result{}, err
	}

	select {
	case res := <-resultCh:
		return res, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

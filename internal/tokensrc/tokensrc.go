// Package tokensrc adapts the stored session into an oauth2.TokenSource that
// yields the Firebase ID token and refreshes it when near expiry.
package tokensrc

import (
	"context"
	"time"

	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"golang.org/x/oauth2"
)

// refreshSkew is how far before expiry a refresh is triggered.
const refreshSkew = time.Minute

// SessionStore is the subset of session.Store this package needs.
type SessionStore interface {
	Load() (session.Session, error)
	Save(session.Session) error
}

// Refresher exchanges a refresh token for a fresh ID token.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (sneatauth.Result, error)
}

// Source implements oauth2.TokenSource backed by the persisted session.
type Source struct {
	ctx   context.Context
	store SessionStore
	auth  Refresher
	now   func() time.Time
}

// New builds a Source. now defaults to time.Now when nil.
func New(ctx context.Context, store SessionStore, auth Refresher, now func() time.Time) *Source {
	if now == nil {
		now = time.Now
	}
	return &Source{ctx: ctx, store: store, auth: auth, now: now}
}

// Token returns the current ID token, refreshing it first if near expiry.
func (s *Source) Token() (*oauth2.Token, error) {
	sess, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if s.now().After(sess.ExpiresAt.Add(-refreshSkew)) {
		res, err := s.auth.Refresh(s.ctx, sess.RefreshToken)
		if err != nil {
			return nil, err
		}
		sess.IDToken = res.IDToken
		if res.RefreshToken != "" {
			sess.RefreshToken = res.RefreshToken
		}
		sess.ExpiresAt = s.now().Add(res.ExpiresIn)
		if err := s.store.Save(sess); err != nil {
			return nil, err
		}
	}
	return &oauth2.Token{AccessToken: sess.IDToken, Expiry: sess.ExpiresAt}, nil
}

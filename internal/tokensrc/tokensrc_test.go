package tokensrc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

type fakeStore struct {
	sess  session.Session
	saved *session.Session
	err   error
}

func (f *fakeStore) Load() (session.Session, error) { return f.sess, f.err }
func (f *fakeStore) Save(s session.Session) error   { f.saved = &s; return nil }

type fakeRefresher struct {
	res    sneatauth.Result
	err    error
	called bool
}

func (f *fakeRefresher) Refresh(context.Context, string) (sneatauth.Result, error) {
	f.called = true
	return f.res, f.err
}

var now = time.Unix(10000, 0)

func at(offset time.Duration) time.Time { return now.Add(offset) }

func TestToken_FreshReturnedWithoutRefresh(t *testing.T) {
	store := &fakeStore{sess: session.Session{IDToken: "idt", ExpiresAt: at(time.Hour)}}
	ref := &fakeRefresher{}
	src := New(context.Background(), store, ref, func() time.Time { return now })

	tok, err := src.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "idt" {
		t.Fatalf("token = %q", tok.AccessToken)
	}
	if ref.called {
		t.Fatalf("refresh should not be called for a fresh token")
	}
}

func TestToken_RefreshesWhenNearExpiry(t *testing.T) {
	store := &fakeStore{sess: session.Session{IDToken: "old", RefreshToken: "rft", ExpiresAt: at(30 * time.Second)}}
	ref := &fakeRefresher{res: sneatauth.Result{IDToken: "new", RefreshToken: "rft2", ExpiresIn: time.Hour}}
	src := New(context.Background(), store, ref, func() time.Time { return now })

	tok, err := src.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !ref.called {
		t.Fatalf("expected refresh")
	}
	if tok.AccessToken != "new" {
		t.Fatalf("token = %q, want new", tok.AccessToken)
	}
	if store.saved == nil || store.saved.IDToken != "new" || store.saved.RefreshToken != "rft2" {
		t.Fatalf("refreshed session not saved: %+v", store.saved)
	}
	if !store.saved.ExpiresAt.Equal(at(time.Hour)) {
		t.Fatalf("expiresAt = %v", store.saved.ExpiresAt)
	}
}

func TestToken_LoadError(t *testing.T) {
	src := New(context.Background(), &fakeStore{err: session.ErrNoSession}, &fakeRefresher{}, func() time.Time { return now })
	if _, err := src.Token(); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoSession", err)
	}
}

func TestToken_RefreshError(t *testing.T) {
	store := &fakeStore{sess: session.Session{ExpiresAt: at(-time.Hour)}}
	ref := &fakeRefresher{err: errors.New("boom")}
	src := New(context.Background(), store, ref, func() time.Time { return now })
	if _, err := src.Token(); err == nil {
		t.Fatalf("expected refresh error")
	}
}

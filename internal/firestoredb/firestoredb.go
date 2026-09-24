// Package firestoredb opens a DALgo-wrapped Firestore database authenticated as
// the signed-in user (Firebase ID token), and provides typed reads.
package firestoredb

import (
	"context"
	"errors"
	"sync"

	"cloud.google.com/go/firestore"
	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo2firestore"
	"github.com/dal-go/record"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

// ErrNotFound is returned when a requested document does not exist.
var ErrNotFound = errors.New("not found")

// DB is an open Firestore connection plus its DALgo wrapper.
type DB struct {
	client *firestore.Client
	dal    dal.DB
}

// Open connects to Firestore as the user (via ts) or to the emulator when
// FirestoreEmulatorHost is set (the client reads FIRESTORE_EMULATOR_HOST).
func Open(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (*DB, error) {
	var opts []option.ClientOption
	if cfg.FirestoreEmulatorHost == "" {
		opts = append(opts, option.WithTokenSource(ts))
	}
	client, err := firestore.NewClient(ctx, cfg.Project, opts...)
	if err != nil {
		return nil, err
	}
	return &DB{client: client, dal: dalgo2firestore.NewDatabase(cfg.Project, client)}, nil
}

// Close releases the underlying Firestore client.
func (d *DB) Close() error { return d.client.Close() }

// RunReadonlyTransaction runs fn in a read-only transaction against the
// underlying database, for callers (internal/aichat/data today) that need
// query/nested-key reads GetDoc does not cover -- each module reader knows
// its own collection layout and query shape; this package should not.
//
// m3: this replaces a prior DAL() accessor that returned the full dal.DB
// (capable of read-write transactions too, an escape hatch wider than any
// caller outside this package needs) with the narrowest capability an
// external read-only caller actually uses.
//
// COVERAGE NOTE: only reachable through a real *DB, which only Open builds
// (a live Firestore client) -- there is no fake/seam here to unit-test
// against without one. Covered by the existing emulator-gated suite
// (firestoredb_emulator_test.go, `go test -tags emulator`, see its own doc
// comment) whenever FIRESTORE_EMULATOR_HOST is set; every exerciser of this
// package's *Session/*DB plumbing (internal/aichat/data's Firestore
// readers, this package's own ListContacts/GetContact/spaces.go) shares
// this same constraint -- it is the "Firestore collection paths aren't
// verified against a live/emulated space" limitation already documented in
// spec/features/chat-ai/README.md's Out of Scope section, not something
// this line can fix in isolation.
func (d *DB) RunReadonlyTransaction(ctx context.Context, fn func(ctx context.Context, tx dal.ReadTransaction) error) error {
	return d.dal.RunReadonlyTransaction(ctx, fn)
}

// GetDoc reads the document at collection/id into data (a pointer to a struct
// or a *map[string]any) using DALgo.
func (d *DB) GetDoc(ctx context.Context, collection, id string, data any) error {
	rec := record.NewRecordWithData(record.NewKeyWithID(collection, id), data)
	return d.dal.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		return tx.Get(ctx, rec)
	})
}

// Session lazily opens ONE Firestore client and reuses it for every read a
// reader built on top of it makes, instead of Open/Close per call (m4: one
// Firestore client per session -- a fresh gRPC client per keystroke-driven
// chat turn is wasteful and was the pre-fix-round-2 behaviour of every
// reader in this package and internal/aichat/data).
//
// A failed Open is not cached: a transient failure (a network blip on the
// FIRST read) must not permanently poison the reader for the rest of the
// session -- the next call simply retries Open.
type Session struct {
	cfg config.Config
	ts  oauth2.TokenSource
	mu  sync.Mutex
	db  *DB
}

// NewSession builds a Session that opens its Firestore client on first use.
func NewSession(cfg config.Config, ts oauth2.TokenSource) *Session {
	return &Session{cfg: cfg, ts: ts}
}

// DB returns the session's shared connection, opening it on first call.
//
// COVERAGE NOTE: the s.db != nil fast path is exercised indirectly by every
// caller that reads twice in one session (e.g. TestReaders_ShareOneFirestoreSession);
// the first-open path calls Open, which needs a real Firestore client -- see
// RunReadonlyTransaction's own COVERAGE NOTE for why that is out of reach
// for a pure unit test here.
func (s *Session) DB(ctx context.Context) (*DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	db, err := Open(ctx, s.cfg, s.ts)
	if err != nil {
		return nil, err
	}
	s.db = db
	return db, nil
}

// Close releases the session's Firestore client, if one was ever opened.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

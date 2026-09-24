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

// firestoreConn is the subset of *firestore.Client this package depends on:
// just enough to release the connection. A narrow seam (instead of storing
// the concrete SDK type) lets Open's wiring be unit-tested with a fake
// connection, with no real Firestore project required.
type firestoreConn interface {
	Close() error
}

// readOnlyRunner is the subset of dal.DB this package depends on. dal.DB is
// deliberately sealed (only dalgo.NewDB/dalgo2firestore.NewDatabase can
// produce one), so a test cannot construct a real one directly; depending on
// this narrower, unsealed interface instead lets a fake stand in for it.
type readOnlyRunner interface {
	RunReadonlyTransaction(ctx context.Context, fn dal.ROTxWorker, options ...dal.TransactionOption) error
}

// DB is an open Firestore connection plus its DALgo wrapper.
type DB struct {
	client firestoreConn
	dal    readOnlyRunner
}

// newFirestoreConn is Open's seam over the two Firestore SDK constructors:
// building the client and wrapping it for DALgo. Its default value is the
// real implementation; tests override the var to exercise Open's own
// plumbing (error propagation, Session caching, reader wiring) without a
// live Firestore project. The default body itself needs no real network to
// run -- cloud.google.com/go/firestore's client construction is lazy -- so
// it is exercised directly (not overridden) by TestOpen_RealConstructor.
var newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
	var opts []option.ClientOption
	if cfg.FirestoreEmulatorHost == "" {
		opts = append(opts, option.WithTokenSource(ts))
	}
	client, err := firestore.NewClient(ctx, cfg.Project, opts...)
	if err != nil {
		return nil, nil, err
	}
	return client, dalgo2firestore.NewDatabase(cfg.Project, client), nil
}

// Open connects to Firestore as the user (via ts) or to the emulator when
// FirestoreEmulatorHost is set (the client reads FIRESTORE_EMULATOR_HOST).
func Open(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (*DB, error) {
	client, database, err := newFirestoreConn(ctx, cfg, ts)
	if err != nil {
		return nil, err
	}
	return &DB{client: client, dal: database}, nil
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

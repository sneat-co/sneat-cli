package data

import (
	"context"

	"github.com/dal-go/dalgo/dal"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// readOnlyRunner is the subset of *firestoredb.DB this package's
// Firestore-backed readers depend on: running a read-only transaction.
// Depending on this narrower interface (rather than the concrete *DB)
// lets a test fake stand in for a real Firestore connection.
type readOnlyRunner interface {
	RunReadonlyTransaction(ctx context.Context, fn func(ctx context.Context, tx dal.ReadTransaction) error) error
}

// sessionProvider is the subset of *firestoredb.Session this package's
// Firestore-backed readers depend on. *firestoredb.Session does not satisfy
// this interface directly -- its DB method returns the concrete
// *firestoredb.DB, not this narrower readOnlyRunner -- so firestoreSession
// below adapts it; a fake sessionProvider stands in for tests.
type sessionProvider interface {
	DB(ctx context.Context) (readOnlyRunner, error)
	Close() error
}

// firestoreSession adapts a real *firestoredb.Session to sessionProvider.
// *firestoredb.DB already implements readOnlyRunner, so this is a pure type
// narrowing at the call site, not a behavioural wrapper.
type firestoreSession struct{ s *firestoredb.Session }

func (a firestoreSession) DB(ctx context.Context) (readOnlyRunner, error) { return a.s.DB(ctx) }
func (a firestoreSession) Close() error                                   { return a.s.Close() }

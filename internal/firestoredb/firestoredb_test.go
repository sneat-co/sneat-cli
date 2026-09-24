package firestoredb

import (
	"context"
	"errors"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
)

// TestOpen_RealConstructor exercises newFirestoreConn's real, unmocked body
// (both the emulator-set and emulator-unset branches): cloud.google.com/go/
// firestore's client construction is lazy and does not dial out, so this
// runs fast and needs no live Firestore project, emulator, or credentials.
func TestOpen_RealConstructor(t *testing.T) {
	ctx := context.Background()
	for _, cfg := range []config.Config{
		{Project: "p1"},
		{Project: "p1", FirestoreEmulatorHost: "localhost:1"},
	} {
		db, err := Open(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("Open(%+v) = %v, want nil error", cfg, err)
		}
		if db == nil || db.client == nil || db.dal == nil {
			t.Fatalf("Open(%+v) returned a DB with a nil field: %+v", cfg, db)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close() = %v, want nil", err)
		}
	}
}

// TestOpen_RealConstructor_ClientError exercises newFirestoreConn's real
// body's own error branch (firestore.NewClient failing) directly, not via
// an overridden seam: an empty Project ID is rejected synchronously by the
// SDK itself ("projectID was empty"), with no network or credentials
// needed, closing the one gap TestOpen_RealConstructor's happy-path calls
// leave (they never make firestore.NewClient itself fail).
func TestOpen_RealConstructor_ClientError(t *testing.T) {
	_, err := Open(context.Background(), config.Config{Project: ""}, nil)
	if err == nil {
		t.Fatal("Open() with an empty Project = nil error, want the SDK's own validation error")
	}
}

// TestOpen_ConstructorError propagates a seam failure straight through Open.
func TestOpen_ConstructorError(t *testing.T) {
	wantErr := errors.New("boom")
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	db, err := Open(context.Background(), config.Config{Project: "p1"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Open() err = %v, want %v", err, wantErr)
	}
	if db != nil {
		t.Fatalf("Open() db = %+v, want nil on error", db)
	}
}

func fakeDB(runner *fakeRunner) *DB {
	return &DB{client: &fakeConn{}, dal: runner}
}

// TestDB_Close forwards to the underlying connection and surfaces its error.
func TestDB_Close(t *testing.T) {
	conn := &fakeConn{}
	db := &DB{client: conn, dal: &fakeRunner{}}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if conn.closeCalls != 1 {
		t.Fatalf("closeCalls = %d, want 1", conn.closeCalls)
	}

	wantErr := errors.New("close failed")
	db2 := &DB{client: &fakeConn{closeErr: wantErr}, dal: &fakeRunner{}}
	if err := db2.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close() = %v, want %v", err, wantErr)
	}
}

// TestDB_RunReadonlyTransaction forwards to db.dal and back.
func TestDB_RunReadonlyTransaction(t *testing.T) {
	runner := &fakeRunner{tx: &fakeReadTransaction{}}
	db := fakeDB(runner)

	called := false
	err := db.RunReadonlyTransaction(context.Background(), func(ctx context.Context, tx dal.ReadTransaction) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunReadonlyTransaction() = %v, want nil", err)
	}
	if !called || runner.runs != 1 {
		t.Fatalf("callback not invoked exactly once: called=%v runs=%d", called, runner.runs)
	}
}

// TestDB_GetDoc covers the found, not-found and transaction-error paths.
func TestDB_GetDoc(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
			rec.SetError(nil)
			return nil
		}}
		db := fakeDB(&fakeRunner{tx: tx})
		var out map[string]any
		if err := db.GetDoc(context.Background(), "users", "u1", &out); err != nil {
			t.Fatalf("GetDoc() = %v, want nil", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
			rec.SetError(record.ErrRecordNotFound)
			return nil
		}}
		db := fakeDB(&fakeRunner{tx: tx})
		var out map[string]any
		err := db.GetDoc(context.Background(), "users", "u1", &out)
		if err != nil {
			t.Fatalf("GetDoc() = %v, want nil (not-found is not a transaction error)", err)
		}
	})

	t.Run("transaction error", func(t *testing.T) {
		wantErr := errors.New("rpc failed")
		db := fakeDB(&fakeRunner{runErr: wantErr})
		var out map[string]any
		err := db.GetDoc(context.Background(), "users", "u1", &out)
		if !errors.Is(err, wantErr) {
			t.Fatalf("GetDoc() = %v, want %v", err, wantErr)
		}
	})
}

// TestSession_DB_OpensOnceAndCaches covers the first-open path, the cached
// fast path, and that a failed first open is NOT cached (m4's contract).
func TestSession_DB_OpensOnceAndCaches(t *testing.T) {
	calls := 0
	conn := &fakeConn{}
	runner := &fakeRunner{}
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		calls++
		return conn, runner, nil
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	s := NewSession(config.Config{Project: "p1"}, nil)
	db1, err := s.DB(context.Background())
	if err != nil {
		t.Fatalf("DB() first call = %v, want nil", err)
	}
	db2, err := s.DB(context.Background())
	if err != nil {
		t.Fatalf("DB() second call = %v, want nil", err)
	}
	if db1 != db2 {
		t.Fatalf("DB() returned two different connections, want the same cached one")
	}
	if calls != 1 {
		t.Fatalf("newFirestoreConn called %d times, want 1 (cached after first open)", calls)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if conn.closeCalls != 1 {
		t.Fatalf("underlying conn closed %d times, want 1", conn.closeCalls)
	}
}

// TestSession_DB_FailedOpenIsNotCached is m4's other half: a transient
// failure on the first read must not permanently poison the session.
func TestSession_DB_FailedOpenIsNotCached(t *testing.T) {
	wantErr := errors.New("transient")
	calls := 0
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		calls++
		if calls == 1 {
			return nil, nil, wantErr
		}
		return &fakeConn{}, &fakeRunner{}, nil
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	s := NewSession(config.Config{Project: "p1"}, nil)
	if _, err := s.DB(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("DB() first call = %v, want %v", err, wantErr)
	}
	db, err := s.DB(context.Background())
	if err != nil {
		t.Fatalf("DB() retry = %v, want nil (a failed open must not be cached)", err)
	}
	if db == nil {
		t.Fatal("DB() retry returned nil db")
	}
	if calls != 2 {
		t.Fatalf("newFirestoreConn called %d times, want 2 (retried after the first failure)", calls)
	}
}

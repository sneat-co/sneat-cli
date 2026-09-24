package firestoredb

import (
	"context"
	"errors"
	"testing"

	"github.com/dal-go/record"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
)

// TestListSpaces_ReturnsSpacesFromUserDoc drives ListSpaces end to end
// (Open -> GetDoc -> spacesFromUser) through the newFirestoreConn seam, so
// no real Firestore project is needed.
func TestListSpaces_ReturnsSpacesFromUserDoc(t *testing.T) {
	tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
		m, ok := rec.Data().(*map[string]any)
		if !ok {
			t.Fatalf("Data() = %T, want *map[string]any", rec.Data())
		}
		*m = map[string]any{"spaces": map[string]any{"s1": map[string]any{"type": "family"}}}
		rec.SetError(nil)
		return nil
	}}
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return &fakeConn{}, &fakeRunner{tx: tx}, nil
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	spaces, err := NewSpacesReader(config.Config{Project: "p1"}, nil).ListSpaces(context.Background(), "u1")
	if err != nil {
		t.Fatalf("ListSpaces() = %v, want nil", err)
	}
	if len(spaces) != 1 || spaces["s1"] == nil {
		t.Fatalf("spaces = %+v", spaces)
	}
}

// TestListSpaces_OpenError propagates a failed Open without panicking on a
// nil *DB (ListSpaces must return before calling db.Close()).
func TestListSpaces_OpenError(t *testing.T) {
	wantErr := errors.New("open failed")
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	_, err := NewSpacesReader(config.Config{Project: "p1"}, nil).ListSpaces(context.Background(), "u1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListSpaces() = %v, want %v", err, wantErr)
	}
}

// TestListSpaces_GetDocError propagates a read failure.
func TestListSpaces_GetDocError(t *testing.T) {
	wantErr := errors.New("get failed")
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return &fakeConn{}, &fakeRunner{runErr: wantErr}, nil
	}
	t.Cleanup(func() { newFirestoreConn = orig })

	_, err := NewSpacesReader(config.Config{Project: "p1"}, nil).ListSpaces(context.Background(), "u1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListSpaces() = %v, want %v", err, wantErr)
	}
}

func TestSpacesFromUser(t *testing.T) {
	got := spacesFromUser(map[string]any{
		"spaces": map[string]any{"s1": map[string]any{"type": "family"}},
	})
	if len(got) != 1 || got["s1"] == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSpacesFromUser_MissingOrWrongType(t *testing.T) {
	for _, user := range []map[string]any{
		{},
		{"spaces": "not-a-map"},
		{"spaces": nil},
	} {
		got := spacesFromUser(user)
		if got == nil {
			t.Fatalf("nil map for %+v", user)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %+v", got)
		}
	}
}

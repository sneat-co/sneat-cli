package firestoredb

import (
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

// TestSession_Close_NoOpWhenNeverOpened is m4's cheap regression: closing a
// Session that never opened a client (e.g. a reader that was constructed
// but never read from) must not panic or error -- Close is always safe to
// call unconditionally at session shutdown.
func TestSession_Close_NoOpWhenNeverOpened(t *testing.T) {
	s := NewSession(config.Config{Project: "p1"}, nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() on a never-opened session = %v, want nil", err)
	}
	// Idempotent: a second Close must also be a safe no-op.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() = %v, want nil", err)
	}
}

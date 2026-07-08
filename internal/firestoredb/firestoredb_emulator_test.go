//go:build emulator

// Integration tests that require a running Firestore emulator:
//
//	firebase emulators:start --only firestore --project sneat-eur3-1
//	FIRESTORE_EMULATOR_HOST=localhost:8080 go test -tags emulator ./internal/firestoredb/...
package firestoredb

import (
	"context"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestOpenGetDoc_AgainstEmulator(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("set FIRESTORE_EMULATOR_HOST to run")
	}
	ctx := context.Background()
	cfg := config.Config{Project: "sneat-eur3-1", FirestoreEmulatorHost: host}

	// Seed a user doc directly.
	raw, err := firestore.NewClient(ctx, cfg.Project)
	if err != nil {
		t.Fatalf("seed client: %v", err)
	}
	_, err = raw.Collection("users").Doc("u1").Set(ctx, map[string]any{
		"spaces": map[string]any{"s1": map[string]any{"type": "family", "title": "Home"}},
	})
	_ = raw.Close()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	spaces, err := NewSpacesReader(cfg, nil).ListSpaces(ctx, "u1")
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	if len(spaces) != 1 || spaces["s1"] == nil {
		t.Fatalf("spaces = %+v", spaces)
	}
}

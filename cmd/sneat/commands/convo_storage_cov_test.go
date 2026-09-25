package commands

import (
	"testing"

	"github.com/dal-go/dalgo/adapters/dalgo2memory"
)

// resolveSandboxDB is env-driven and has no filesystem/network dependency at
// construction time (dalgo2openvaultdb.NewDB only validates its arguments and
// wires an http.Client; it makes no network call), so every branch is
// reachable with plain t.Setenv.

func TestResolveSandboxDB_DefaultIsMemory(t *testing.T) {
	t.Setenv(envSneatStorage, "")
	db, err := resolveSandboxDB()
	if err != nil {
		t.Fatalf("resolveSandboxDB: %v", err)
	}
	if db == nil {
		t.Fatal("expected a non-nil DB")
	}
	if db.Adapter().Name() != dalgo2memory.New(dalgo2memory.FirestoreProfile()).Adapter().Name() {
		t.Errorf("expected an in-memory adapter, got %q", db.Adapter().Name())
	}
}

func TestResolveSandboxDB_ExplicitMemory(t *testing.T) {
	t.Setenv(envSneatStorage, "memory")
	db, err := resolveSandboxDB()
	if err != nil {
		t.Fatalf("resolveSandboxDB: %v", err)
	}
	if db == nil {
		t.Fatal("expected a non-nil DB")
	}
}

func TestResolveSandboxDB_OpenVaultDB_Defaults(t *testing.T) {
	t.Setenv(envSneatStorage, "openvaultdb")
	t.Setenv(envOpenvaultdbURL, "")
	t.Setenv(envOpenvaultdbDB, "")
	t.Setenv(envOpenvaultdbToken, "")
	db, err := resolveSandboxDB()
	if err != nil {
		t.Fatalf("resolveSandboxDB: %v", err)
	}
	if db == nil {
		t.Fatal("expected a non-nil DB")
	}
	if db.Adapter().Name() != "openvaultdb" {
		t.Errorf("expected the openvaultdb adapter, got %q", db.Adapter().Name())
	}
}

func TestResolveSandboxDB_OpenVaultDB_CustomURLDBAndToken(t *testing.T) {
	t.Setenv(envSneatStorage, "openvaultdb")
	t.Setenv(envOpenvaultdbURL, "http://example.invalid:9999")
	t.Setenv(envOpenvaultdbDB, "custom-db")
	t.Setenv(envOpenvaultdbToken, "secret-token")
	db, err := resolveSandboxDB()
	if err != nil {
		t.Fatalf("resolveSandboxDB: %v", err)
	}
	if db == nil {
		t.Fatal("expected a non-nil DB")
	}
	if db.ID() != "custom-db" {
		t.Errorf("expected DB ID %q, got %q", "custom-db", db.ID())
	}
}

func TestResolveSandboxDB_UnsupportedStorageErrors(t *testing.T) {
	t.Setenv(envSneatStorage, "bogus-driver")
	db, err := resolveSandboxDB()
	if err == nil {
		t.Fatal("expected an error for an unsupported SNEAT_STORAGE value")
	}
	if db != nil {
		t.Errorf("expected a nil DB on error, got %v", db)
	}
	wantSubstr := `unsupported SNEAT_STORAGE value "bogus-driver"`
	if got := err.Error(); got != wantSubstr+" (supported: memory, openvaultdb)" {
		t.Errorf("unexpected error text: %q", got)
	}
}

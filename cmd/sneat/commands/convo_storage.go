package commands

import (
	"fmt"
	"os"

	"github.com/dal-go/dalgo/adapters/dalgo2memory"
	"github.com/dal-go/dalgo/dal"
	dalgo2openvaultdb "github.com/dal-go/dalgo2openvaultdb"
)

// Env vars selecting the datastore behind the convo sandbox. Facades stay
// storage-agnostic: they always go through facade.GetSneatDB; only this
// wiring decides which DALgo driver backs it.
const (
	envSneatStorage     = "SNEAT_STORAGE"     // "" | "memory" | "openvaultdb"
	envOpenvaultdbURL   = "OPENVAULTDB_URL"   // default http://127.0.0.1:6832
	envOpenvaultdbDB    = "OPENVAULTDB_DB"    // default sneat-dev
	envOpenvaultdbToken = "OPENVAULTDB_TOKEN" // bearer token for `ovdb serve --auth` servers
)

// resolveSandboxDB constructs the DALgo DB for convo sandbox commands based
// on SNEAT_STORAGE. Default is the in-memory DB (fresh per invocation);
// "openvaultdb" targets a running `ovdb serve` instance and persists between
// runs.
//
// This builds the in-memory DB via dalgo2memory.New(FirestoreProfile())
// directly rather than sneat-go-core's sneatcoretesting.NewInMemoryTestDB:
// this is a live CLI command path (the convo sandbox), not a test, and
// sneatcoretesting is documented as test-only infrastructure — its own
// package doc scopes it to that use. The chosen profile still emulates the
// fleet's production backend (Firestore), matching sneatcoretesting's
// default.
func resolveSandboxDB() (dal.DB, error) {
	switch storage := os.Getenv(envSneatStorage); storage {
	case "", "memory":
		return dalgo2memory.New(dalgo2memory.FirestoreProfile()), nil
	case "openvaultdb":
		url := os.Getenv(envOpenvaultdbURL)
		if url == "" {
			url = "http://127.0.0.1:6832"
		}
		dbID := os.Getenv(envOpenvaultdbDB)
		if dbID == "" {
			dbID = "sneat-dev"
		}
		var opts []dalgo2openvaultdb.Option
		if token := os.Getenv(envOpenvaultdbToken); token != "" {
			opts = append(opts, dalgo2openvaultdb.WithBearerToken(token))
		}
		return dalgo2openvaultdb.NewDB(url, dbID, opts...)
	default:
		return nil, fmt.Errorf("unsupported %s value %q (supported: memory, openvaultdb)", envSneatStorage, storage)
	}
}

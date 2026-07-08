// Package firestoredb opens a DALgo-wrapped Firestore database authenticated as
// the signed-in user (Firebase ID token), and provides typed reads.
package firestoredb

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo2firestore"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

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

// GetDoc reads the document at collection/id into data (a pointer to a struct
// or a *map[string]any) using DALgo.
func (d *DB) GetDoc(ctx context.Context, collection, id string, data any) error {
	rec := dal.NewRecordWithData(dal.NewKeyWithID(collection, id), data)
	return d.dal.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		return tx.Get(ctx, rec)
	})
}

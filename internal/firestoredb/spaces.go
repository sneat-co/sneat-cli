package firestoredb

import (
	"context"

	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
)

// SpacesReader reads a user's spaces from Firestore as that user.
type SpacesReader struct {
	cfg config.Config
	ts  oauth2.TokenSource
}

// NewSpacesReader builds a reader; each call opens its own short-lived client.
func NewSpacesReader(cfg config.Config, ts oauth2.TokenSource) *SpacesReader {
	return &SpacesReader{cfg: cfg, ts: ts}
}

// ListSpaces returns the `spaces` map from the users/{uid} document.
func (r *SpacesReader) ListSpaces(ctx context.Context, uid string) (map[string]any, error) {
	db, err := Open(ctx, r.cfg, r.ts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	user := map[string]any{}
	if err := db.GetDoc(ctx, "users", uid, &user); err != nil {
		return nil, err
	}
	return spacesFromUser(user), nil
}

// spacesFromUser extracts the `spaces` map from a user document, defaulting to
// an empty (non-nil) map so callers always emit a JSON object.
func spacesFromUser(user map[string]any) map[string]any {
	spaces, _ := user["spaces"].(map[string]any)
	if spaces == nil {
		spaces = map[string]any{}
	}
	return spaces
}

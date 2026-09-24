package data

import (
	"context"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"golang.org/x/oauth2"
)

// firestoreContacts adapts internal/firestoredb.ContactsReader (this repo's
// existing, already-used contact read path) to this package's
// ContactsReader shape, adding FindByName as a client-side substring filter
// over List -- contactus has no server-side name-search query this MVP
// slice can call instead.
type firestoreContacts struct {
	r *firestoredb.ContactsReader
}

// NewFirestoreContacts builds a ContactsReader over the existing
// firestoredb.ContactsReader.
func NewFirestoreContacts(cfg config.Config, ts oauth2.TokenSource) ContactsReader {
	return &firestoreContacts{r: firestoredb.NewContactsReader(cfg, ts)}
}

// Close releases the reader's Firestore client, if one was ever opened
// (firestoredb.ContactsReader already holds one lazily-opened, reused
// client per m4 -- this just forwards to it).
func (a *firestoreContacts) Close() error { return a.r.Close() }

func (a *firestoreContacts) List(ctx context.Context, spaceID string) ([]Contact, error) {
	cs, err := a.r.ListContacts(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(cs))
	for _, c := range cs {
		out = append(out, Contact{ID: c.ID, SpaceID: spaceID, Name: contactName(c)})
	}
	return out, nil
}

func (a *firestoreContacts) FindByName(ctx context.Context, spaceID, query string) ([]Contact, error) {
	all, err := a.List(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	q := normalizeForSearch(query)
	var out []Contact
	for _, c := range all {
		if containsFold(c.Name, q) {
			out = append(out, c)
		}
	}
	return out, nil
}

// contactName mirrors cmd/sneat/main.go's contactDisplayName: an explicit
// title, else the full name, else empty.
func contactName(c firestoredb.Contact) string {
	d := c.Contact
	if d == nil {
		return ""
	}
	if d.Title != "" {
		return d.Title
	}
	if d.Names != nil {
		if n := d.Names.GetFullName(); n != "" {
			return n
		}
	}
	return ""
}

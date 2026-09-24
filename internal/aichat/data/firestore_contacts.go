package data

import (
	"context"
	"sort"

	"github.com/strongo/strongoapp/with"

	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// firestoreContacts adapts internal/firestoredb.ContactsReader (this repo's
// existing, already-used contact read path) to this package's
// ContactsReader shape, adding FindByName as a client-side substring filter
// over List -- contactus has no server-side name-search query this MVP
// slice can call instead.
type firestoreContacts struct {
	r *firestoredb.ContactsReader
}

// NewFirestoreContacts builds a ContactsReader over session, an
// ALREADY-OWNED *firestoredb.Session shared with the other readers (m6;
// see NewFirestoreHappenings's doc comment in firestore_readers.go).
func NewFirestoreContacts(session *firestoredb.Session) ContactsReader {
	return &firestoreContacts{r: firestoredb.NewContactsReaderFromSession(session)}
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
		out = append(out, toDataContact(spaceID, c))
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

// Get returns one contact by ID (m11: mirrors firestoreHappenings.Get's role
// for candidateHappeningRows -- lets an ambiguous contact-choice list's
// candidates be enriched with real fields via one lookup each).
func (a *firestoreContacts) Get(ctx context.Context, spaceID, contactID string) (Contact, error) {
	c, err := a.r.GetContact(ctx, spaceID, contactID)
	if err != nil {
		return Contact{}, err
	}
	return toDataContact(spaceID, c), nil
}

// toDataContact maps a raw firestoredb.Contact (dbo4contactus.ContactDbo) to
// this package's Contact (m11): names, DoB, gender, the flat "relatedAs"
// relationship label, and email/phone channels -- everything
// dbo4contactus.ContactDbo actually carries that is safe and useful to show
// a person, reused rather than re-derived. Name mirrors cmd/sneat/main.go's
// contactDisplayName: an explicit title, else the full name, else empty.
func toDataContact(spaceID string, c firestoredb.Contact) Contact {
	out := Contact{ID: c.ID, SpaceID: spaceID}
	d := c.Contact
	if d == nil {
		return out
	}
	out.Name = d.Title
	if d.Names != nil {
		out.FirstName = d.Names.FirstName
		out.LastName = d.Names.LastName
		out.NickName = d.Names.NickName
		out.FullName = d.Names.FullName
		if out.Name == "" {
			out.Name = d.Names.GetFullName()
		}
	}
	out.Gender = d.Gender
	out.DoB = d.DoB
	out.RelatedAs = d.RelatedAs
	out.Emails = commChannelKeys(d.Emails)
	out.Phones = commChannelKeys(d.Phones)
	return out
}

// commChannelKeys turns a dbo4contactus emails/phones map (keyed by the
// address/number itself) into a display list, primary channel first (there
// is at most one, per with.validateCommunicationChannelsField), then the
// rest in a stable, deterministic order -- Firestore/Go map iteration order
// is not, and this is display data a test can assert on.
func commChannelKeys(m map[string]*with.CommunicationChannelProps) []string {
	if len(m) == 0 {
		return nil
	}
	var primary string
	rest := make([]string, 0, len(m))
	for k, p := range m {
		if p != nil && p.IsPrimary && primary == "" {
			primary = k
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	if primary == "" {
		return rest
	}
	return append([]string{primary}, rest...)
}

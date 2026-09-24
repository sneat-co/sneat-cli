package firestoredb

import (
	"context"
	"reflect"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
)

// Contact pairs a contact's document ID with its typed DBO for JSON output.
type Contact struct {
	ID      string                    `json:"id"`
	Contact *dbo4contactus.ContactDbo `json:"contact"`
}

// ContactsReader reads a space's contacts from Firestore as the user.
type ContactsReader struct {
	session *Session
}

// NewContactsReader builds a reader over its OWN lazily-opened, reused
// Firestore client (m4) rather than a fresh client per call. Kept for
// existing single-reader callers (cmd/sneat/main.go); a caller building
// several readers for one chat session (internal/aichat/data, via
// internal/chatapp) should share ONE Session across all of them instead --
// see NewContactsReaderFromSession (m6).
func NewContactsReader(cfg config.Config, ts oauth2.TokenSource) *ContactsReader {
	return NewContactsReaderFromSession(NewSession(cfg, ts))
}

// NewContactsReaderFromSession builds a reader over an ALREADY-OWNED
// Session (m6: "one Firestore client per chat session shared by all three
// readers") -- the caller opens (and eventually closes) the Session once
// and shares it across every reader built from it, instead of each reader
// opening its own client.
func NewContactsReaderFromSession(session *Session) *ContactsReader {
	return &ContactsReader{session: session}
}

// Close releases the reader's Firestore client, if one was ever opened.
func (r *ContactsReader) Close() error { return r.session.Close() }

// contactsCollectionRef points at spaces/{spaceID}/ext/contactus/contacts.
func contactsCollectionRef(spaceID string) dal.CollectionRef {
	spaceKey := record.NewKeyWithID("spaces", spaceID)
	moduleKey := record.NewKeyWithParentAndID(spaceKey, "ext", "contactus")
	return dal.NewCollectionRef("contacts", "", moduleKey)
}

// newContactRecord builds an empty, incomplete-key envelope for one query
// result row. Named (rather than an inline closure in ListContacts) so it
// has its own direct unit test: a real backend's query executor invokes it
// per decoded document, but this package's own fake QueryExecutor (used in
// unit tests) supplies already-built records and never calls it, so a test
// driving ListContacts through the fake alone cannot reach it.
func newContactRecord() record.Record {
	return record.NewRecordWithIncompleteKey("contacts", reflect.String, &dbo4contactus.ContactDbo{})
}

// ListContacts returns the space's flat, active top-level contacts.
func (r *ContactsReader) ListContacts(ctx context.Context, spaceID string) ([]Contact, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return nil, err
	}

	query := dal.NewQueryBuilder(dal.From(contactsCollectionRef(spaceID))).
		WhereField("status", dal.Equal, "active").
		WhereField("parentID", dal.Equal, "").
		SelectIntoRecord(newContactRecord)

	var records []record.Record
	err = db.dal.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		records, err = dal.ExecuteQueryAndReadAllToRecords(ctx, query, tx)
		return err
	})
	if err != nil {
		return nil, err
	}

	contacts := make([]Contact, 0, len(records))
	for _, rec := range records {
		id, _ := rec.Key().ID.(string)
		dbo, _ := rec.Data().(*dbo4contactus.ContactDbo)
		contacts = append(contacts, Contact{ID: id, Contact: dbo})
	}
	return contacts, nil
}

// GetContact reads a single contact by ID.
func (r *ContactsReader) GetContact(ctx context.Context, spaceID, contactID string) (Contact, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return Contact{}, err
	}

	spaceKey := record.NewKeyWithID("spaces", spaceID)
	moduleKey := record.NewKeyWithParentAndID(spaceKey, "ext", "contactus")
	key := record.NewKeyWithParentAndID(moduleKey, "contacts", contactID)
	dbo := &dbo4contactus.ContactDbo{}
	rec := record.NewRecordWithData(key, dbo)

	err = db.dal.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		return tx.Get(ctx, rec)
	})
	if err != nil {
		return Contact{}, err
	}
	if !rec.Exists() {
		return Contact{}, ErrNotFound
	}
	return Contact{ID: contactID, Contact: dbo}, nil
}

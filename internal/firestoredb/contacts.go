package firestoredb

import (
	"context"
	"reflect"

	"github.com/dal-go/dalgo/dal"
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
	cfg config.Config
	ts  oauth2.TokenSource
}

// NewContactsReader builds a reader; each call opens its own short-lived client.
func NewContactsReader(cfg config.Config, ts oauth2.TokenSource) *ContactsReader {
	return &ContactsReader{cfg: cfg, ts: ts}
}

// contactsCollectionRef points at spaces/{spaceID}/ext/contactus/contacts.
func contactsCollectionRef(spaceID string) dal.CollectionRef {
	spaceKey := dal.NewKeyWithID("spaces", spaceID)
	moduleKey := dal.NewKeyWithParentAndID(spaceKey, "ext", "contactus")
	return dal.NewCollectionRef("contacts", "", moduleKey)
}

// ListContacts returns the space's flat, active top-level contacts.
func (r *ContactsReader) ListContacts(ctx context.Context, spaceID string) ([]Contact, error) {
	db, err := Open(ctx, r.cfg, r.ts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	query := dal.NewQueryBuilder(dal.From(contactsCollectionRef(spaceID))).
		WhereField("status", dal.Equal, "active").
		WhereField("parentID", dal.Equal, "").
		SelectIntoRecord(func() dal.Record {
			return dal.NewRecordWithIncompleteKey("contacts", reflect.String, &dbo4contactus.ContactDbo{})
		})

	var records []dal.Record
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
	db, err := Open(ctx, r.cfg, r.ts)
	if err != nil {
		return Contact{}, err
	}
	defer func() { _ = db.Close() }()

	spaceKey := dal.NewKeyWithID("spaces", spaceID)
	moduleKey := dal.NewKeyWithParentAndID(spaceKey, "ext", "contactus")
	key := dal.NewKeyWithParentAndID(moduleKey, "contacts", contactID)
	dbo := &dbo4contactus.ContactDbo{}
	rec := dal.NewRecordWithData(key, dbo)

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

package data

import (
	"context"
	"errors"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo/recordset"
	"github.com/dal-go/record"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// fakeSession is a sessionProvider double: it hands back a configured
// readOnlyRunner (or open error) without any real Firestore connection.
type fakeSession struct {
	runner    readOnlyRunner
	openErr   error
	closeErr  error
	dbCalls   int
	closeCall int
}

func (f *fakeSession) DB(ctx context.Context) (readOnlyRunner, error) {
	f.dbCalls++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.runner, nil
}

func (f *fakeSession) Close() error {
	f.closeCall++
	return f.closeErr
}

// fakeRunner is a readOnlyRunner double.
type fakeRunner struct {
	tx     dal.ReadTransaction
	runErr error
	runs   int
}

func (f *fakeRunner) RunReadonlyTransaction(ctx context.Context, fn func(ctx context.Context, tx dal.ReadTransaction) error) error {
	f.runs++
	if f.runErr != nil {
		return f.runErr
	}
	return fn(ctx, f.tx)
}

// fakeReadTransaction is a minimal dal.ReadTransaction double, driven by
// per-test func fields (mirrors internal/firestoredb's own fakes_test.go).
type fakeReadTransaction struct {
	getFn         func(ctx context.Context, rec record.Record) error
	queryReaderFn func(ctx context.Context, query dal.Query) (dal.RecordsReader, error)
}

func (f *fakeReadTransaction) Get(ctx context.Context, rec record.Record) error {
	if f.getFn == nil {
		return errors.New("fakeReadTransaction: Get not configured")
	}
	return f.getFn(ctx, rec)
}

func (f *fakeReadTransaction) Exists(ctx context.Context, key *record.Key) (bool, error) {
	return false, errors.New("fakeReadTransaction: Exists not configured")
}

func (f *fakeReadTransaction) GetMulti(ctx context.Context, recs []record.Record) error {
	return errors.New("fakeReadTransaction: GetMulti not configured")
}

func (f *fakeReadTransaction) ExecuteQueryToRecordsReader(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
	if f.queryReaderFn == nil {
		return nil, errors.New("fakeReadTransaction: ExecuteQueryToRecordsReader not configured")
	}
	return f.queryReaderFn(ctx, query)
}

func (f *fakeReadTransaction) ExecuteQueryToRecordsetReader(ctx context.Context, query dal.Query, options ...recordset.Option) (dal.RecordsetReader, error) {
	return nil, errors.New("fakeReadTransaction: ExecuteQueryToRecordsetReader not configured")
}

func (f *fakeReadTransaction) Options() dal.TransactionOptions {
	return dal.NewTransactionOptions()
}

// fakeContactsReader is a contactsReader double.
type fakeContactsReader struct {
	listFn   func(ctx context.Context, spaceID string) ([]firestoredb.Contact, error)
	getFn    func(ctx context.Context, spaceID, contactID string) (firestoredb.Contact, error)
	closeErr error
	closed   int
}

func (f *fakeContactsReader) ListContacts(ctx context.Context, spaceID string) ([]firestoredb.Contact, error) {
	if f.listFn == nil {
		return nil, errors.New("fakeContactsReader: ListContacts not configured")
	}
	return f.listFn(ctx, spaceID)
}

func (f *fakeContactsReader) GetContact(ctx context.Context, spaceID, contactID string) (firestoredb.Contact, error) {
	if f.getFn == nil {
		return firestoredb.Contact{}, errors.New("fakeContactsReader: GetContact not configured")
	}
	return f.getFn(ctx, spaceID, contactID)
}

func (f *fakeContactsReader) Close() error {
	f.closed++
	return f.closeErr
}

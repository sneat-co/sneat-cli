package firestoredb

import (
	"context"
	"errors"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo/recordset"
	"github.com/dal-go/record"
)

// fakeConn is a firestoreConn double: it records whether Close was called
// and can be made to fail it, without opening a real gRPC connection.
type fakeConn struct {
	closeErr   error
	closeCalls int
}

func (f *fakeConn) Close() error {
	f.closeCalls++
	return f.closeErr
}

// fakeRunner is a readOnlyRunner double standing in for dal.DB (which is
// sealed and cannot be constructed directly outside the dalgo package).
type fakeRunner struct {
	tx      dal.ReadTransaction
	runErr  error
	fnErr   error // error the callback itself returns, propagated by RunReadonlyTransaction
	runs    int
	lastCtx context.Context
}

func (f *fakeRunner) RunReadonlyTransaction(ctx context.Context, fn dal.ROTxWorker, options ...dal.TransactionOption) error {
	f.runs++
	f.lastCtx = ctx
	if f.runErr != nil {
		return f.runErr
	}
	if err := fn(ctx, f.tx); err != nil {
		return err
	}
	return f.fnErr
}

// fakeReadTransaction is a minimal dal.ReadTransaction double. Each method a
// test needs is driven by a func field; unset fields fail loudly so an
// unexpected call is never silently a no-op.
type fakeReadTransaction struct {
	getFn         func(ctx context.Context, rec record.Record) error
	existsFn      func(ctx context.Context, key *record.Key) (bool, error)
	getMultiFn    func(ctx context.Context, recs []record.Record) error
	queryReaderFn func(ctx context.Context, query dal.Query) (dal.RecordsReader, error)
}

func (f *fakeReadTransaction) Get(ctx context.Context, rec record.Record) error {
	if f.getFn == nil {
		return errors.New("fakeReadTransaction: Get not configured")
	}
	return f.getFn(ctx, rec)
}

func (f *fakeReadTransaction) Exists(ctx context.Context, key *record.Key) (bool, error) {
	if f.existsFn == nil {
		return false, errors.New("fakeReadTransaction: Exists not configured")
	}
	return f.existsFn(ctx, key)
}

func (f *fakeReadTransaction) GetMulti(ctx context.Context, recs []record.Record) error {
	if f.getMultiFn == nil {
		return errors.New("fakeReadTransaction: GetMulti not configured")
	}
	return f.getMultiFn(ctx, recs)
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

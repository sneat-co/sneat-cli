package pipeline

import (
	"context"

	"github.com/strongo/aichat/ai/session"
)

// Executor performs one validated session.Action against the real Sneat
// backend. Implementations MUST mutate only through sneat-go HTTP endpoints
// (internal/sneatapi) -- never direct Firestore writes (⛔ writes go through
// sneat-go endpoints only).
//
// It returns the undo action when the API supports reversing this one
// (reschedule back to the old time, reopen a completed todo), so the caller
// can set session.State.Previous.Undo for a later "undo".
type Executor interface {
	Execute(ctx context.Context, action session.Action) (undo *session.Action, err error)
}

// FakeExecutor is an in-memory Executor for tests: it records every action it
// was asked to run and returns a caller-supplied undo/error per Kind.
type FakeExecutor struct {
	Executed []session.Action
	Undo     map[string]*session.Action
	Err      map[string]error
}

func (f *FakeExecutor) Execute(_ context.Context, action session.Action) (*session.Action, error) {
	f.Executed = append(f.Executed, action)
	if f.Err != nil {
		if err, ok := f.Err[action.Kind]; ok {
			return nil, err
		}
	}
	if f.Undo != nil {
		return f.Undo[action.Kind], nil
	}
	return nil, nil
}

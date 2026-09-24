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
// spaceID is the PIPELINE's own current space (B3 ruling: "executor takes
// the pipeline's spaceID parameter for every action") -- the single source
// of truth every mutation runs against. It is never read back out of
// action.Args["spaceID"] (model-controlled) or trusted blindly from
// action.Target.Keys["spaceID"]; an implementation that finds the two
// disagree must refuse the action rather than execute it in the wrong
// space.
//
// It returns the undo action when the API supports reversing this one
// (reschedule back to the old time, reopen a completed todo), so the caller
// can set session.State.Previous.Undo for a later "undo".
type Executor interface {
	Execute(ctx context.Context, spaceID string, action session.Action) (undo *session.Action, err error)
}

// FakeExecutor is an in-memory Executor for tests: it records every action it
// was asked to run (and the spaceID each call ran with, parallel to
// Executed -- B3) and returns a caller-supplied undo/error per Kind.
type FakeExecutor struct {
	Executed []session.Action
	SpaceIDs []string
	Undo     map[string]*session.Action
	Err      map[string]error
}

func (f *FakeExecutor) Execute(_ context.Context, spaceID string, action session.Action) (*session.Action, error) {
	f.Executed = append(f.Executed, action)
	f.SpaceIDs = append(f.SpaceIDs, spaceID)
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

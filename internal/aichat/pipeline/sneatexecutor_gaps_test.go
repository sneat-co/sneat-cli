package pipeline

import (
	"context"
	"net/http"
	"testing"

	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// errAPIServer builds a *sneatapi.Client (via newTestSneatAPI) that fails
// every request with a 500, for exercising each mutation's API-error
// propagation branch without a real backend.
func errAPIServer(t *testing.T) (*SneatExecutor, *[]string) {
	t.Helper()
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	return &SneatExecutor{Calendar: api, Todo: api}, calls
}

// TestSneatExecutor_NoAPIConfigured covers every mutation's own "no API
// configured" (or "no resolved target") guard branch.
func TestSneatExecutor_NoAPIConfigured(t *testing.T) {
	e := SneatExecutor{}
	ctx := context.Background()
	target := &session.EntityRef{Keys: map[string]string{"list": data.ListKindDo, "itemID": "i1"}}

	if _, err := e.setTodoDone(ctx, "sp1", session.Action{Target: target}, true); err == nil {
		t.Error("setTodoDone with no Todo API should error")
	}
	if err := e.deleteTodo(ctx, "sp1", session.Action{Target: target}); err == nil {
		t.Error("deleteTodo with no Todo API should error")
	}
	if _, err := e.addListItem(ctx, "sp1", session.Action{Args: map[string]string{"title": "Milk"}}, data.ListKindDo); err == nil {
		t.Error("addListItem with no Todo API should error")
	}
}

// TestSneatExecutor_MutationAPIErrors covers each mutation's API-error
// propagation branch: setTodoDone, deleteTodo, addListItem,
// revokeCancellation, cancelAdjustment.
func TestSneatExecutor_MutationAPIErrors(t *testing.T) {
	e, _ := errAPIServer(t)
	ctx := context.Background()
	target := &session.EntityRef{Keys: map[string]string{"list": data.ListKindBuy, "itemID": "i1"}}

	if _, err := e.setTodoDone(ctx, "sp1", session.Action{Target: target}, true); err == nil {
		t.Error("setTodoDone should propagate a 5xx as an error")
	}
	if err := e.deleteTodo(ctx, "sp1", session.Action{Target: target}); err == nil {
		t.Error("deleteTodo should propagate a 5xx as an error")
	}
	if _, err := e.addListItem(ctx, "sp1", session.Action{Args: map[string]string{"title": "Milk"}}, data.ListKindBuy); err == nil {
		t.Error("addListItem should propagate a 5xx as an error")
	}
	happeningTarget := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	if err := e.revokeCancellation(ctx, "sp1", session.Action{Target: happeningTarget}); err == nil {
		t.Error("revokeCancellation should propagate a 5xx as an error")
	}
	if err := e.cancelAdjustment(ctx, "sp1", session.Action{Target: happeningTarget}); err == nil {
		t.Error("cancelAdjustment should propagate a 5xx as an error")
	}
}

// TestSneatExecutor_AddListItem_TitleRequired covers addListItem's
// empty-title guard.
func TestSneatExecutor_AddListItem_TitleRequired(t *testing.T) {
	e, _ := errAPIServer(t)
	if _, err := e.addListItem(context.Background(), "sp1", session.Action{}, data.ListKindDo); err == nil {
		t.Error("addListItem with no title should error")
	}
}

// TestSneatExecutor_AddListItem_NoCreatedItems covers the "server accepted
// the request but returned no created items" branch: no undo, no error.
func TestSneatExecutor_AddListItem_NoCreatedItems(t *testing.T) {
	api, _ := newTestSneatAPI(t, nil) // default 200 OK, empty body -> no CreatedItems
	e := SneatExecutor{Todo: api}
	undo, err := e.addListItem(context.Background(), "sp1", session.Action{Args: map[string]string{"title": "Milk"}}, data.ListKindDo)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if undo != nil {
		t.Fatalf("undo = %+v, want nil when the server created nothing", undo)
	}
}

// TestSneatExecutor_Execute_UndoKinds covers Execute's revoke_cancellation
// and cancel_adjustment dispatch cases directly (a real UNDO action's own
// Kind, not one the taxonomy ever names).
func TestSneatExecutor_Execute_UndoKinds(t *testing.T) {
	e, calls := errAPIServer(t)
	// Both should reach the API (and fail, since errAPIServer always 500s) --
	// this just proves Execute's switch dispatches them at all.
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	_, _ = e.Execute(context.Background(), "sp1", session.Action{Kind: calendarRevokeCancellationKind, Target: target})
	_, _ = e.Execute(context.Background(), "sp1", session.Action{Kind: calendarCancelAdjustmentKind, Target: target})
	if len(*calls) != 2 {
		t.Fatalf("calls = %v, want 2 requests reaching the API", *calls)
	}
}

// TestListKeyFor_Executor covers the pipeline package's own listKeyFor
// (distinct from data's), both branches.
func TestListKeyFor_Executor(t *testing.T) {
	if listKeyFor(data.ListKindBuy) == listKeyFor(data.ListKindDo) {
		t.Fatal("buy and do list keys must differ")
	}
	if listKeyFor("bogus") != listKeyFor(data.ListKindDo) {
		t.Fatal("an unknown list kind should default to the do list")
	}
}

// TestSneatExecutor_Now_DefaultsToTimeNow covers the Now-func-unset default
// fallback (SneatExecutor's own now(), mirroring Pipeline's/Resolver's).
func TestSneatExecutor_Now_DefaultsToTimeNow(t *testing.T) {
	e := SneatExecutor{}
	if e.now().IsZero() {
		t.Fatal("now() with Now unset should default to time.Now(), not zero")
	}
}

// TestSummaryFor_RescheduleDefaultWhen covers summaryFor's "a new time"
// fallback when no "when" slot was captured.
func TestSummaryFor_RescheduleDefaultWhen(t *testing.T) {
	got := summaryFor(sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening, session.EntityRef{}, nil)
	if got == "" {
		t.Fatal("expected non-empty summary")
	}
}

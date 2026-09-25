package pipeline

import (
	"context"
	"testing"

	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestSupportedActionKinds_MatchesExecutor is the anti-drift test the
// founder's brief asked for (buy-add-prompt.md): every kind
// SupportedActionKinds() advertises as executor-backed MUST be a real key
// in SneatExecutor{}.handlers() (forward direction), and every key in
// handlers() MUST be accounted for -- either as a SupportedActionKinds()
// entry, or as one of the two documented undo-only kinds this map also
// carries but never offers to the model (backward direction). If either
// direction breaks, the instruction text and HandleAction's real behaviour
// have drifted apart again.
func TestSupportedActionKinds_MatchesExecutor(t *testing.T) {
	handlers := SneatExecutor{}.handlers()

	contactLookup := map[string]bool{}
	for _, k := range contactLookupActionKinds() {
		contactLookup[k.Kind] = true
	}

	for _, k := range executorActionKinds() {
		if _, ok := handlers[k.Kind]; !ok {
			t.Errorf("SupportedActionKinds advertises %q but SneatExecutor.handlers() has no case for it", k.Kind)
		}
	}

	undoOnly := map[string]bool{
		calendarRevokeCancellationKind: true,
		calendarCancelAdjustmentKind:   true,
	}
	advertised := map[string]bool{}
	for _, k := range executorActionKinds() {
		advertised[k.Kind] = true
	}
	for kind := range handlers {
		if advertised[kind] || undoOnly[kind] {
			continue
		}
		t.Errorf("SneatExecutor.handlers() has a case for %q that is neither advertised in SupportedActionKinds nor a documented undo-only kind -- update kinds.go", kind)
	}

	// The two contact lookups are handled by HandleAction directly (never
	// reach Executor), so they must NOT also be executor handler keys.
	for kind := range contactLookup {
		if _, ok := handlers[kind]; ok {
			t.Errorf("%q is a contact lookup (handled by findOrShowContact) but also has an executor handler -- it should never reach Execute", kind)
		}
	}
}

// TestNormalizeActionKind covers the alias table and the pass-through
// default.
func TestNormalizeActionKind(t *testing.T) {
	cases := map[string]string{
		"buy.add":            sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
		"to_buy.add":         sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
		"shopping.add":       sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
		"todo.add":           sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo,
		"calendar.add":       sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening,
		"todo.complete_todo": sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo, // unaliased, passes through
		"totally.unknown":    "totally.unknown",                                             // unknown kind, passes through unchanged
	}
	for in, want := range cases {
		if got := normalizeActionKind(in); got != want {
			t.Errorf("normalizeActionKind(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsSupportedActionKind covers both the alias-normalized and the
// directly-supported/unsupported cases.
func TestIsSupportedActionKind(t *testing.T) {
	if !IsSupportedActionKind("buy.add") {
		t.Error(`IsSupportedActionKind("buy.add") = false, want true (aliased to todo.add_to_buy)`)
	}
	if !IsSupportedActionKind(sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy) {
		t.Error("IsSupportedActionKind(todo.add_to_buy) = false, want true")
	}
	if IsSupportedActionKind(sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening) {
		t.Error("IsSupportedActionKind(calendar.add_happening) = true, want false (no executor case)")
	}
	if IsSupportedActionKind("totally.unknown") {
		t.Error(`IsSupportedActionKind("totally.unknown") = true, want false`)
	}
}

// TestUnsupportedActionText covers every branch: the calendar add/update
// happening pair, todo update, and the generic default for a kind the
// model invented outright.
func TestUnsupportedActionText(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening, "Adding or updating calendar events from chat isn't supported yet -- add it in the Sneat app."},
		{sneatdomain.ModuleCalendar + "." + sneatdomain.IntentUpdateHappening, "Adding or updating calendar events from chat isn't supported yet -- add it in the Sneat app."},
		{sneatdomain.ModuleTodo + "." + sneatdomain.IntentUpdateTodo, "Updating a todo's details from chat isn't supported yet -- add it in the Sneat app."},
		{"buy.add", "I can't do that from chat yet."},
	}
	for _, c := range cases {
		if got := unsupportedActionText(c.kind); got != c.want {
			t.Errorf("unsupportedActionText(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// TestHandleAction_UnsupportedKind_NoError is the founder's core bug fix
// regression (buy-add-prompt.md): a kind HandleAction cannot carry out must
// come back as plain Output text with err == nil -- never the raw
// "pipeline: no executor case for action kind ..." error that used to leak
// to the user as "system: error: ...".
func TestHandleAction_UnsupportedKind_NoError(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{Kind: "calendar.add_happening", Reference: "yoga"}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction returned an error for an unsupported kind, want nil: %v", err)
	}
	if out.Text == "" {
		t.Fatal("HandleAction returned an unsupported kind with no explanatory text")
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("an unsupported kind must never reach Executor: %+v", exec.Executed)
	}
}

// TestHandleAction_AliasKind_Executes is the reported bug's exact
// reproduction: the model emits "buy.add" (not a real kind) instead of
// "todo.add_to_buy" -- HandleAction must normalize it and actually add the
// item, not answer "I can't do that".
func TestHandleAction_AliasKind_Executes(t *testing.T) {
	exec := &FakeExecutor{Undo: map[string]*session.Action{}}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{Kind: "buy.add", Slots: map[string]string{"title": "milk"}}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if len(exec.Executed) != 1 || exec.Executed[0].Kind != sneatdomain.ModuleTodo+"."+sneatdomain.IntentAddToBuy {
		t.Fatalf("executed = %+v, want exactly one todo.add_to_buy", exec.Executed)
	}
	if out.Text == "" {
		t.Fatal("expected non-empty confirmation text")
	}
}

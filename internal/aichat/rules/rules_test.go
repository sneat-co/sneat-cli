package rules

import (
	"context"
	"testing"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func decide(t *testing.T, text string, st session.State) (decision.Decision, bool) {
	t.Helper()
	p := New()
	d, ok, err := p.Decide(context.Background(), decision.Request{Text: text, Taxonomy: sneatdomain.Taxonomy(), State: st})
	if err != nil {
		t.Fatalf("Decide(%q): %v", text, err)
	}
	return d, ok
}

func TestName(t *testing.T) {
	if New().Name() != Name {
		t.Fatalf("Name() = %q, want %q", New().Name(), Name)
	}
}

func TestPhraseTable(t *testing.T) {
	cases := []struct {
		text         string
		module       string
		intent       string
		presentation string
	}{
		{"show my calendar today", sneatdomain.ModuleCalendar, sneatdomain.IntentShowDay, sneatdomain.PresentationDayCalendar},
		{"Calendar today", sneatdomain.ModuleCalendar, sneatdomain.IntentShowDay, sneatdomain.PresentationDayCalendar},
		{"today", sneatdomain.ModuleCalendar, sneatdomain.IntentShowDay, sneatdomain.PresentationDayCalendar},
		{"this week", sneatdomain.ModuleCalendar, sneatdomain.IntentShowWeek, sneatdomain.PresentationWeekCalendar},
		{"what's happening this week?", sneatdomain.ModuleCalendar, sneatdomain.IntentShowWeek, sneatdomain.PresentationWeekCalendar},
		{"my todos", sneatdomain.ModuleTodo, sneatdomain.IntentListTodos, sneatdomain.PresentationTodoList},
		{"todo list", sneatdomain.ModuleTodo, sneatdomain.IntentListTodos, sneatdomain.PresentationTodoList},
		{"contacts", sneatdomain.ModuleContacts, sneatdomain.IntentListContacts, sneatdomain.PresentationContactsGrid},
		{"help", sneatdomain.ModuleGeneral, sneatdomain.IntentHelp, sneatdomain.PresentationText},
		{"explain what I can do here", sneatdomain.ModuleGeneral, sneatdomain.IntentHelp, sneatdomain.PresentationText},
	}
	for _, c := range cases {
		d, ok := decide(t, c.text, session.State{})
		if !ok {
			t.Errorf("%q: expected a decision, got abstain", c.text)
			continue
		}
		if d.Module.Value != c.module || d.Intent.Value != c.intent || d.Presentation != c.presentation {
			t.Errorf("%q: got module=%s intent=%s presentation=%s, want %s/%s/%s",
				c.text, d.Module.Value, d.Intent.Value, d.Presentation, c.module, c.intent, c.presentation)
		}
		if !d.CanHandleDeterministically || d.NeedsLLM {
			t.Errorf("%q: expected deterministic, no-LLM decision", c.text)
		}
	}
}

func TestAbstainsOnUnknownText(t *testing.T) {
	_, ok := decide(t, "move my dentist appointment to Friday", session.State{})
	if ok {
		t.Fatal("expected abstention for free text outside the phrase table")
	}
}

func TestConfirmRequiresPending(t *testing.T) {
	if _, ok := decide(t, "yes", session.State{}); ok {
		t.Fatal("\"yes\" without a Pending action must abstain")
	}
	pending := &session.Action{Kind: "calendar.reschedule_happening", Summary: "Move Dentist to Fri 16:00?"}
	d, ok := decide(t, "yes", session.State{Pending: pending})
	if !ok || d.Interaction != decision.InteractionConfirmation {
		t.Fatalf("d=%v ok=%v, want confirmation", d, ok)
	}
}

func TestRejectRequiresPending(t *testing.T) {
	if _, ok := decide(t, "cancel that", session.State{}); ok {
		t.Fatal("\"cancel that\" without a Pending action must abstain")
	}
	pending := &session.Action{Kind: "calendar.cancel_happening"}
	d, ok := decide(t, "cancel that", session.State{Pending: pending})
	if !ok || d.Interaction != decision.InteractionCancellation {
		t.Fatalf("d=%v ok=%v, want cancellation", d, ok)
	}
	d, ok = decide(t, "no", session.State{Pending: pending})
	if !ok || d.Interaction != decision.InteractionRejection {
		t.Fatalf("d=%v ok=%v, want rejection", d, ok)
	}
}

// TestRejectRequiresPending_NormalizesCasingAndPunctuation is m7 (from
// fix round r2, verified in fix round r4's review): rejectRule's
// interaction-kind branch does `text == "cancel that"` -- a LITERAL
// comparison that would silently fall through to the plain
// InteractionRejection branch (or, if rejectWords itself failed to match,
// abstain entirely) for anything but that exact lowercase phrase, unless
// the text reaching Match is ALREADY normalized. It is: Provider.Decide
// (strongo/aichat/ai/decision/rules) calls Normalize(req.Text) before ever
// invoking a Rule's Match -- lowercasing, collapsing whitespace, and
// trimming trailing ".!?" -- so "Cancel that!" (capitalized, with the
// trailing "!" a real chat message plausibly has) reaches rejectRule as
// "cancel that" and is still recognised as a CANCELLATION specifically,
// not just a generic rejection.
func TestRejectRequiresPending_NormalizesCasingAndPunctuation(t *testing.T) {
	pending := &session.Action{Kind: "calendar.cancel_happening"}
	d, ok := decide(t, "Cancel that!", session.State{Pending: pending})
	if !ok {
		t.Fatal("\"Cancel that!\" with a Pending action must not abstain")
	}
	if d.Interaction != decision.InteractionCancellation {
		t.Fatalf("Interaction = %v, want InteractionCancellation (normalization must reduce \"Cancel that!\" to \"cancel that\")", d.Interaction)
	}
}

func TestUndoRequiresPrevious(t *testing.T) {
	if _, ok := decide(t, "undo", session.State{}); ok {
		t.Fatal("\"undo\" without a Previous action must abstain")
	}
	prev := &session.Action{Kind: "todo.complete_todo"}
	d, ok := decide(t, "undo", session.State{Previous: prev})
	if !ok || d.Interaction != decision.InteractionUndo {
		t.Fatalf("d=%v ok=%v, want undo", d, ok)
	}
}

// TestFindContactRule is S8: each of findContactPrefixes' three prefixes
// resolves to a find_contact decision carrying the remainder as the
// Reference.Expression; a bare prefix with no name, and text matching none
// of the prefixes, both abstain.
func TestFindContactRule(t *testing.T) {
	cases := []struct{ text, wantName string }{
		{"find contact Alice", "alice"},
		{"show contact Bob", "bob"},
		{"contact Carol", "carol"},
	}
	for _, c := range cases {
		d, ok := decide(t, c.text, session.State{})
		if !ok {
			t.Fatalf("%q: expected a decision, got abstain", c.text)
		}
		if d.Module.Value != sneatdomain.ModuleContacts || d.Intent.Value != sneatdomain.IntentFindContact {
			t.Errorf("%q: module/intent = %s/%s, want contacts/find_contact", c.text, d.Module.Value, d.Intent.Value)
		}
		if d.Reference == nil || d.Reference.Expression != c.wantName {
			t.Errorf("%q: Reference = %+v, want Expression %q", c.text, d.Reference, c.wantName)
		}
	}

	if _, ok := decide(t, "something else entirely", session.State{}); ok {
		t.Error("text matching no findContactPrefixes entry must abstain")
	}

	// A bare prefix with no name must abstain rather than decide with an
	// empty Reference -- called directly against the rule's Match func
	// (bypassing rules.Provider's own text normalization, which trims
	// trailing whitespace and would otherwise strip the very space that
	// makes "find contact " match the prefix at all) so this exact branch
	// is exercised deterministically.
	if _, ok := findContactRule().Match("find contact ", session.State{}); ok {
		t.Error("a bare prefix with no name must abstain, not decide with an empty Reference")
	}
}

// TestValidatesAgainstTaxonomy guards against a rule naming a module/intent
// this package's own Taxonomy does not declare -- decision.Chain would
// silently downgrade such a decision to "invalid" and fall through.
func TestValidatesAgainstTaxonomy(t *testing.T) {
	texts := []string{
		"show my calendar today", "this week", "show upcoming", "my todos",
		"to buy", "contacts", "help",
	}
	tax := sneatdomain.Taxonomy()
	for _, text := range texts {
		d, ok := decide(t, text, session.State{})
		if !ok {
			t.Fatalf("%q: expected a decision", text)
		}
		if err := decision.Validate(d, tax); err != nil {
			t.Errorf("%q: decision fails taxonomy validation: %v", text, err)
		}
	}
}

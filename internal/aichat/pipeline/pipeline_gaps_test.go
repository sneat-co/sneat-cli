package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestEntityKindFor covers every branch: each module prefix, and the
// default (unknown kind) case.
func TestEntityKindFor(t *testing.T) {
	cases := map[string]string{
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentShowDay:     sneatdomain.EntityHappening,
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo:         sneatdomain.EntityTodo,
		sneatdomain.ModuleContacts + "." + sneatdomain.IntentFindContact: sneatdomain.EntityContact,
		"general.help": "",
		"":             "",
	}
	for kind, want := range cases {
		if got := entityKindFor(kind); got != want {
			t.Errorf("entityKindFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestAmbiguousChoiceOutput covers every entityKindFor branch: happening
// (with HappeningRows enrichment), todo (both list/buy presentations),
// contact (with ContactRows enrichment), and the default/unknown-kind
// fallback.
func TestAmbiguousChoiceOutput(t *testing.T) {
	ctx := context.Background()
	p := Pipeline{
		Readers: data.Readers{
			Happenings: &data.FakeHappenings{Items: []data.Happening{{ID: "h1", SpaceID: "sp1", Title: "Dentist", Start: fixedNow()}}},
			Contacts:   &data.FakeContacts{Items: []data.Contact{{ID: "c1", SpaceID: "sp1", Name: "Alice"}}},
		},
		Now: fixedNow,
	}

	t.Run("happening", func(t *testing.T) {
		refs := []session.EntityRef{{Type: sneatdomain.EntityHappening, Keys: map[string]string{"happeningID": "h1"}}}
		out := p.ambiguousChoiceOutput(ctx, "sp1", sneatdomain.ModuleCalendar+"."+sneatdomain.IntentCancelHappening, refs)
		if out.Presentation != sneatdomain.PresentationHappeningsList || len(out.HappeningRows) != 1 {
			t.Fatalf("out = %+v", out)
		}
	})

	t.Run("todo list", func(t *testing.T) {
		refs := []session.EntityRef{{Type: sneatdomain.EntityTodo, Keys: map[string]string{"list": data.ListKindDo}}}
		out := p.ambiguousChoiceOutput(ctx, "sp1", sneatdomain.ModuleTodo+"."+sneatdomain.IntentCompleteTodo, refs)
		if out.Presentation != sneatdomain.PresentationTodoList {
			t.Fatalf("Presentation = %q, want TodoList", out.Presentation)
		}
	})

	t.Run("todo buy list", func(t *testing.T) {
		refs := []session.EntityRef{{Type: sneatdomain.EntityTodo, Keys: map[string]string{"list": data.ListKindBuy}}}
		out := p.ambiguousChoiceOutput(ctx, "sp1", sneatdomain.ModuleTodo+"."+sneatdomain.IntentCompleteTodo, refs)
		if out.Presentation != sneatdomain.PresentationBuyList {
			t.Fatalf("Presentation = %q, want BuyList", out.Presentation)
		}
	})

	t.Run("contact", func(t *testing.T) {
		refs := []session.EntityRef{{Type: sneatdomain.EntityContact, Keys: map[string]string{"contactID": "c1"}}}
		out := p.ambiguousChoiceOutput(ctx, "sp1", sneatdomain.ModuleContacts+"."+sneatdomain.IntentFindContact, refs)
		if out.Presentation != sneatdomain.PresentationContactsGrid || len(out.ContactRows) != 1 {
			t.Fatalf("out = %+v", out)
		}
	})

	t.Run("unknown kind default", func(t *testing.T) {
		out := p.ambiguousChoiceOutput(ctx, "sp1", "general.help", nil)
		if out.Presentation != sneatdomain.PresentationHappeningsList {
			t.Fatalf("Presentation = %q, want the default HappeningsList", out.Presentation)
		}
	})
}

// TestCandidateHappeningRows covers the no-reader, Get-error, non-recurring
// and recurring-with-anchor branches.
func TestCandidateHappeningRows(t *testing.T) {
	ctx := context.Background()

	t.Run("no reader", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		if got := p.candidateHappeningRows(ctx, "sp1", nil); got != nil {
			t.Fatalf("got = %+v, want nil with no Happenings reader", got)
		}
	})

	t.Run("Get error drops all rows", func(t *testing.T) {
		p := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{}}, Now: fixedNow}
		refs := []session.EntityRef{{Keys: map[string]string{"happeningID": "missing"}}}
		if got := p.candidateHappeningRows(ctx, "sp1", refs); got != nil {
			t.Fatalf("got = %+v, want nil on a read failure", got)
		}
	})

	t.Run("non-recurring uses stored Start/End", func(t *testing.T) {
		start := fixedNow()
		p := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
			{ID: "h1", SpaceID: "sp1", Title: "Dentist", Start: start, End: start.Add(30 * time.Minute)},
		}}}, Now: fixedNow}
		refs := []session.EntityRef{{Keys: map[string]string{"happeningID": "h1"}}}
		got := p.candidateHappeningRows(ctx, "sp1", refs)
		if len(got) != 1 || !got[0].Start.Equal(start) {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("recurring resolves to its next anchor", func(t *testing.T) {
		// A weekly-Friday recurring happening; fixedNow is itself a Friday
		// (see its own doc comment), so recurringAnchor should resolve to
		// today at the template's time-of-day.
		templateStart := fixedNow().AddDate(0, 0, -7)
		slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
			Timing:   calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-18", Time: "09:00"}},
			Repeats:  calendariusdbo.RepeatPeriodWeekly,
			Weekdays: []calendariusdbo.WeekdayCode{calendariusdbo.Friday2},
		}}
		p := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
			{ID: "h1", SpaceID: "sp1", Title: "Yoga", Start: templateStart, End: templateStart.Add(time.Hour), Recurring: true, Slot: &slot},
		}}}, Now: fixedNow}
		refs := []session.EntityRef{{Keys: map[string]string{"happeningID": "h1"}}}
		got := p.candidateHappeningRows(ctx, "sp1", refs)
		if len(got) != 1 {
			t.Fatalf("got = %+v", got)
		}
		if !got[0].Recurring {
			t.Fatal("Recurring flag should propagate")
		}
	})
}

// TestConfirmPending_CancelPending_UndoPrevious cover their guard branches
// directly (no Pending/Previous, no Executor configured).
func TestConfirmPending_CancelPending_UndoPrevious(t *testing.T) {
	ctx := context.Background()

	t.Run("confirmPending: nothing pending", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		st := &session.State{}
		out, err := p.confirmPending(ctx, "sp1", st)
		if err != nil || out.Text == "" {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})

	t.Run("confirmPending: no executor configured", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		st := &session.State{Pending: &session.Action{Kind: "calendar.cancel_happening"}}
		if _, err := p.confirmPending(ctx, "sp1", st); err == nil {
			t.Fatal("expected an error with no Executor configured")
		}
	})

	t.Run("cancelPending: nothing pending", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		out := p.cancelPending(&session.State{})
		if out.Text == "" {
			t.Fatal("expected a \"nothing to cancel\" text")
		}
	})

	t.Run("cancelPending: clears Pending", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		st := &session.State{Pending: &session.Action{Kind: "x"}}
		out := p.cancelPending(st)
		if out.Text != "Cancelled." || st.Pending != nil {
			t.Fatalf("out=%+v st.Pending=%v", out, st.Pending)
		}
	})

	t.Run("undoPrevious: nothing to undo", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		out, err := p.undoPrevious(ctx, "sp1", &session.State{})
		if err != nil || out.Text == "" {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})

	t.Run("undoPrevious: no executor configured", func(t *testing.T) {
		p := Pipeline{Now: fixedNow}
		undo := &session.Action{Kind: "y"}
		st := &session.State{Previous: &session.Action{Kind: "x", Undo: undo}}
		if _, err := p.undoPrevious(ctx, "sp1", st); err == nil {
			t.Fatal("expected an error with no Executor configured")
		}
	})

	t.Run("undoPrevious: executor error propagates", func(t *testing.T) {
		wantErr := errors.New("boom")
		p := Pipeline{Executor: &FakeExecutor{Err: map[string]error{"y": wantErr}}, Now: fixedNow}
		undo := &session.Action{Kind: "y"}
		st := &session.State{Previous: &session.Action{Kind: "x", Undo: undo}}
		_, err := p.undoPrevious(ctx, "sp1", st)
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})

	t.Run("undoPrevious: success clears Previous", func(t *testing.T) {
		p := Pipeline{Executor: &FakeExecutor{}, Now: fixedNow}
		undo := &session.Action{Kind: "y"}
		st := &session.State{Previous: &session.Action{Kind: "x", Undo: undo}}
		out, err := p.undoPrevious(ctx, "sp1", st)
		if err != nil || out.Text != "Undone." || st.Previous != nil {
			t.Fatalf("out=%+v err=%v st.Previous=%v", out, err, st.Previous)
		}
	})
}

// TestSummaryFor_DefaultKind covers the default (unrecognized-kind) branch.
func TestSummaryFor_DefaultKind(t *testing.T) {
	got := summaryFor("some.other_kind", session.EntityRef{Title: "Thing"}, nil)
	if got == "" {
		t.Fatal("expected a non-empty fallback summary")
	}
}

// TestFakeExecutor_ErrMap covers FakeExecutor's per-Kind error branch and
// its SpaceIDs bookkeeping.
func TestFakeExecutor_ErrMap(t *testing.T) {
	wantErr := errors.New("boom")
	f := &FakeExecutor{Err: map[string]error{"x": wantErr}}
	_, err := f.Execute(context.Background(), "sp1", session.Action{Kind: "x"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if len(f.SpaceIDs) != 1 || f.SpaceIDs[0] != "sp1" {
		t.Fatalf("SpaceIDs = %v", f.SpaceIDs)
	}
}

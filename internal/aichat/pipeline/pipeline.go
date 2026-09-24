// Package pipeline implements the sneat-chat MVP processing pipeline
// (brief §5): deterministic handlers, then the decision chain, then entity
// resolution, then either deterministic execution or a main-LLM turn
// (llm.go: static+dynamic context via ai/ctxmgr, streaming via an
// ai.LLMProvider, and the <sneat-action> Splitter).
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/sneat-ai-backend/temporal"
	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// Output is one turn's answer: user-facing text plus, when the turn produced
// structured data, the presentation kind and the entities behind it (Phase 2
// renders these as a transcript.Block; this package only resolves and
// fetches).
type Output struct {
	Text         string
	Presentation string
	Entities     []session.EntityRef
	// NeedsLLM is true when no deterministic path answered the turn: the
	// caller must run the main LLM (with cached static context) and feed its
	// streamed answer's <sneat-action> block, if any, back through
	// HandleAction.
	NeedsLLM bool
	// Decision is the chain's decision when one was made (even one that ended
	// up NeedsLLM, e.g. its own CanHandleDeterministically=false) -- useful
	// for the caller's diagnostics and for building the main-LLM prompt.
	Decision *decision.Decision
	// Trace is the full decision.Chain trace for this turn -- every
	// provider's outcome/latency, not just which one decided (S11
	// coordinator ruling DIAGNOSTICS: previously discarded via `_` at the
	// Chain.Decide call site). The caller's diagnostics (chatapp's
	// logTurn/aidiag.Turn.Decision) logs it; it is never shown to the user.
	Trace decision.Trace
	// HappeningRows/TodoRows carry the semantic control model behind a
	// calendar/todo presentation (S9 follow-up, coordinator ruling): real
	// Start/End/Recurring/Done data a bare session.EntityRef cannot hold, so
	// chatapp's blockFor can call controls.NewDayCalendar/NewWeekCalendar/
	// NewHappeningsList/NewTodoList/NewBuyList directly instead of rebuilding
	// a generic, time-less ListBlock from Entities alone. Entities/LastShown
	// are still populated alongside these (resolution/sidebar/"+"/choice-
	// picking all key off EntityRef, not off these richer row types) --
	// HappeningRows/TodoRows are presentation-only, additive detail.
	HappeningRows []controls.HappeningRow
	TodoRows      []controls.TodoRow
	// WeekStart is the Monday a WeekCalendar presentation's week begins on,
	// needed by controls.NewWeekCalendar to build its Mon..Sun day sections.
	WeekStart time.Time
}

// Pipeline runs one chat turn end-to-end against real data in one space.
type Pipeline struct {
	Chain    decision.Chain
	Resolver Resolver
	Executor Executor
	Readers  data.Readers
	// Now returns the current time (injected so tests are deterministic).
	Now func() time.Time
	// TZ is the IANA timezone "today"/"this week" resolve in.
	TZ string
	// LLM answers a turn the deterministic chain could not (NeedsLLM). Nil
	// means "no main LLM configured" -- Stream reports that as an error
	// rather than panicking.
	LLM ai.LLMProvider
	// CtxMgr selects which static/dynamic context blocks go into an LLM
	// request (see llm.go/StreamRequest). Nil sends every available block
	// uncompacted -- fine for tests, not for a long-running session.
	CtxMgr *ctxmgr.Manager
	// Product identifies the consuming product ("sneat") in ai.ChatRequest.
	Product string
}

// Turn runs the deterministic chain against text and answers, or reports
// NeedsLLM when nothing decided.
func (p Pipeline) Turn(ctx context.Context, text string, st *session.State, spaceID string) (Output, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	req := decision.Request{
		Product:  "sneat",
		Text:     text,
		Taxonomy: sneatdomain.Taxonomy(),
		State:    *st,
		Now:      now(),
		TZ:       p.TZ,
	}
	// S11: the Trace (every provider's outcome/latency, not just who decided)
	// used to be discarded here via `_`. It is attached to every Output this
	// method returns below, so chatapp's diagnostics can log the full chain
	// behaviour regardless of which branch answers the turn.
	d, ok, trace := p.Chain.Decide(ctx, req)
	if !ok {
		// No provider (deterministic or Jev) classified this turn. Before
		// falling back to the main LLM, check the cheap deterministic case
		// none of them needs to: a bare number/ordinal picking an item from
		// the most recent structured list this session showed (S3: "Choice
		// lists set LastShown, picking by number... or Enter") -- e.g. after
		// an ambiguous "reschedule the dentist appointment" showed 2
		// matches, "2" focuses the second one instead of round-tripping
		// through the main LLM for something this deterministic.
		if out, handled := p.pickFromLastShown(text, st); handled {
			out.Trace = trace
			return out, nil
		}
		return Output{NeedsLLM: true, Trace: trace}, nil
	}

	out, err := p.turnFromDecision(ctx, d, st, spaceID)
	out.Trace = trace
	return out, err
}

func (p Pipeline) turnFromDecision(ctx context.Context, d decision.Decision, st *session.State, spaceID string) (Output, error) {
	switch d.Interaction {
	case decision.InteractionConfirmation:
		return p.confirmPending(ctx, spaceID, st)
	case decision.InteractionRejection, decision.InteractionCancellation:
		return p.cancelPending(st), nil
	case decision.InteractionUndo:
		return p.undoPrevious(ctx, spaceID, st)
	}

	if !d.CanHandleDeterministically {
		return Output{NeedsLLM: true, Decision: &d}, nil
	}

	return p.handleCommand(ctx, d, st, spaceID)
}

// HandleAction resolves and executes a semantic action a main-LLM turn asked
// for via its trailing <sneat-action> block (see splitter.go). It shares the
// same resolve-then-confirm-or-execute policy as a deterministic command, so
// the LLM path and the rules path cannot drift into two different
// confirmation behaviours for the same action kind.
func (p Pipeline) HandleAction(ctx context.Context, a Action, st *session.State, spaceID string) (Output, error) {
	if isContactLookup(a.Kind) {
		// S8: find_contact/show_contact are read-only lookups, never an
		// Executor action -- see findOrShowContact's doc comment.
		ref := decision.Reference{Kind: sneatdomain.EntityContact, Expression: a.Reference, Pronoun: a.Pronoun}
		return p.findOrShowContact(ctx, ref, st, spaceID)
	}
	if a.Reference == "" && !a.Pronoun {
		return p.runAction(ctx, spaceID, session.Action{Kind: a.Kind, Args: spaceScopedArgs(a.Slots, spaceID)}, st)
	}
	kind := entityKindFor(a.Kind)
	ref := decision.Reference{Kind: kind, Expression: a.Reference, Pronoun: a.Pronoun}
	return p.resolveAndAct(ctx, a.Kind, ref, a.Slots, st, spaceID)
}

// spaceScopedArgs copies slots and forces "spaceID" to the pipeline's own
// current space, discarding whatever value the model (rules or the main
// LLM) may have supplied for it. B3 ruling: every action executes in the
// pipeline's space; a model-provided spaceID is never trusted -- the same
// reason a Resolver never trusts a model-supplied entity ID.
func spaceScopedArgs(slots map[string]string, spaceID string) map[string]string {
	args := make(map[string]string, len(slots)+1)
	for k, v := range slots {
		if k == "spaceID" {
			continue
		}
		args[k] = v
	}
	args["spaceID"] = spaceID
	return args
}

func (p Pipeline) handleCommand(ctx context.Context, d decision.Decision, st *session.State, spaceID string) (Output, error) {
	kind := d.Module.Value + "." + d.Intent.Value
	// S8: find_contact/show_contact must be checked BEFORE the Presentation
	// switch below -- both intents share PresentationContactsGrid with the
	// plain "list every contact" intent (contacts.list_contacts), and the
	// switch dispatches on Presentation alone, so without this a find/show
	// decision with a Reference would be misrouted to listContacts (the
	// full, unfiltered list) instead of resolving ref.
	if isContactLookup(kind) && d.Reference != nil {
		return p.findOrShowContact(ctx, *d.Reference, st, spaceID)
	}
	switch d.Presentation {
	case sneatdomain.PresentationDayCalendar:
		return p.showDay(ctx, st, spaceID, d.Slots["when"])
	case sneatdomain.PresentationWeekCalendar:
		return p.showWeek(ctx, st, spaceID)
	case sneatdomain.PresentationHappeningsList:
		return p.showUpcoming(ctx, st, spaceID)
	case sneatdomain.PresentationTodoList:
		return p.listTodos(ctx, st, spaceID, data.ListKindDo)
	case sneatdomain.PresentationBuyList:
		return p.listTodos(ctx, st, spaceID, data.ListKindBuy)
	case sneatdomain.PresentationContactsGrid:
		return p.listContacts(ctx, st, spaceID)
	}
	if d.Module.Value == sneatdomain.ModuleGeneral && d.Intent.Value == sneatdomain.IntentHelp {
		return Output{Text: helpText}, nil
	}
	if d.Reference != nil {
		return p.resolveAndAct(ctx, kind, *d.Reference, d.Slots, st, spaceID)
	}
	return Output{NeedsLLM: true, Decision: &d}, nil
}

const helpText = "I can show your calendar (today, this week, upcoming), your todos and to-buy list, and your contacts. Say things like \"show my calendar today\", \"my todos\", or \"contacts\"."

// showDay shows one calendar day. when is a decision's "when" slot (m1:
// "honour decision date slots", e.g. "show my calendar tomorrow" ->
// slots={"when":"tomorrow"}) -- empty or unresolvable falls back to today, a
// deterministic rule's own default (see rules.showDayRule) as well as this
// method's zero-value behaviour, so a decision that never set the slot at
// all still shows today exactly as before.
func (p Pipeline) showDay(ctx context.Context, st *session.State, spaceID, when string) (Output, error) {
	day := p.now()
	emptyText := "You have no happenings today."
	if when != "" && when != "today" {
		if d, ok := temporal.ParseText(p.now(), when); ok {
			day = d
			emptyText = fmt.Sprintf("You have no happenings on %s.", d.Format("Mon Jan 2"))
		}
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	// AddDate, not +24h: a calendar day is not always 24 wall-clock hours in
	// the user's zone (DST transitions), coordinator ruling TIMEZONES.
	to := from.AddDate(0, 0, 1)
	return p.showWindow(ctx, st, spaceID, from, to, sneatdomain.PresentationDayCalendar, emptyText)
}

func (p Pipeline) showWeek(ctx context.Context, st *session.State, spaceID string) (Output, error) {
	now := p.now()
	// ISO week: Monday start. time.Weekday is Sunday=0..Saturday=6, so the
	// offset back to Monday is -6 on a Sunday and -(weekday-1) otherwise
	// (coordinator ruling TIMEZONES: week start Monday unless locale says
	// otherwise -- no locale signal is wired yet, so Monday is the default).
	weekday := int(now.Weekday())
	offset := -(weekday - 1)
	if weekday == 0 {
		offset = -6
	}
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, offset)
	to := from.AddDate(0, 0, 7)
	out, err := p.showWindow(ctx, st, spaceID, from, to, sneatdomain.PresentationWeekCalendar, "You have nothing scheduled this week.")
	out.WeekStart = from // S9 follow-up: controls.NewWeekCalendar needs the Monday to build its day sections.
	return out, err
}

func (p Pipeline) showUpcoming(ctx context.Context, st *session.State, spaceID string) (Output, error) {
	from := p.now()
	to := from.AddDate(0, 1, 0)
	return p.showWindow(ctx, st, spaceID, from, to, sneatdomain.PresentationHappeningsList, "Nothing upcoming.")
}

func (p Pipeline) showWindow(ctx context.Context, st *session.State, spaceID string, from, to time.Time, presentation, emptyText string) (Output, error) {
	if p.Readers.Happenings == nil {
		return Output{}, fmt.Errorf("pipeline: no happenings reader configured")
	}
	hs, err := p.Readers.Happenings.Window(ctx, spaceID, from, to)
	if err != nil {
		return Output{}, err
	}
	// S6: HappeningsReader.Window returns EVERY recurring happening
	// unconditionally (it cannot know, on its own, which days a rule hits --
	// see its documented limitation), so the presentation layer narrows to
	// the ones that actually occur in [from, to).
	hs = filterRecurringToWindow(hs, from, to)
	if len(hs) == 0 {
		st.LastShown = nil
		return Output{Text: emptyText, Presentation: presentation}, nil
	}
	refs := make([]session.EntityRef, 0, len(hs))
	rows := make([]controls.HappeningRow, 0, len(hs))
	for _, h := range hs {
		ref := session.EntityRef{Type: sneatdomain.EntityHappening, Title: h.Title,
			Keys: map[string]string{"spaceID": h.SpaceID, "happeningID": h.ID}}
		refs = append(refs, ref)
		rows = append(rows, controls.HappeningRow{Ref: ref, Title: h.Title, Start: h.Start, End: h.End, Recurring: h.Recurring})
	}
	st.LastShown = refs
	return Output{Text: fmt.Sprintf("%d happening(s).", len(hs)), Presentation: presentation, Entities: refs, HappeningRows: rows}, nil
}

// filterRecurringToWindow keeps a non-recurring happening as-is (Window
// already filtered those by Start) and keeps a recurring one only when its
// rule actually produces an occurrence somewhere in [from, to) (S6: "day/
// week views show recurring happenings only on days their rule hits
// (weekdays/weeks)"). Only a weekly rule with explicit Weekdays is expanded
// -- daily/monthly/yearly recurrence is excluded rather than shown on the
// template's possibly-wrong stale date (see HappeningsReader's documented
// limitation: this MVP slice has no full occurrence-expansion engine).
func filterRecurringToWindow(hs []data.Happening, from, to time.Time) []data.Happening {
	out := hs[:0]
	for _, h := range hs {
		if !h.Recurring || recurrenceHitsWindow(h, from, to) {
			out = append(out, h)
		}
	}
	return out
}

// recurrenceHitsWindow reports whether h's weekly recurrence rule produces
// at least one occurrence in the day-aligned range [from, to).
func recurrenceHitsWindow(h data.Happening, from, to time.Time) bool {
	if h.Slot == nil || h.Slot.Repeats != dbo4calendarius.RepeatPeriodWeekly || len(h.Slot.Weekdays) == 0 {
		return false
	}
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		if weekdayCodeMatches(h.Slot.Weekdays, d.Weekday()) {
			return true
		}
	}
	return false
}

func (p Pipeline) listTodos(ctx context.Context, st *session.State, spaceID, list string) (Output, error) {
	if p.Readers.Todos == nil {
		return Output{}, fmt.Errorf("pipeline: no todos reader configured")
	}
	items, err := p.Readers.Todos.List(ctx, spaceID, list)
	if err != nil {
		return Output{}, err
	}
	presentation := sneatdomain.PresentationTodoList
	if list == data.ListKindBuy {
		presentation = sneatdomain.PresentationBuyList
	}
	if len(items) == 0 {
		st.LastShown = nil
		return Output{Text: "Nothing on that list.", Presentation: presentation}, nil
	}
	refs := make([]session.EntityRef, 0, len(items))
	rows := make([]controls.TodoRow, 0, len(items))
	for _, it := range items {
		ref := session.EntityRef{Type: sneatdomain.EntityTodo, Title: it.Title,
			Keys: map[string]string{"spaceID": it.SpaceID, "list": it.List, "itemID": it.ID}}
		refs = append(refs, ref)
		rows = append(rows, controls.TodoRow{Ref: ref, Title: it.Title, Done: it.Done})
	}
	st.LastShown = refs
	return Output{Text: fmt.Sprintf("%d item(s).", len(items)), Presentation: presentation, Entities: refs, TodoRows: rows}, nil
}

func (p Pipeline) listContacts(ctx context.Context, st *session.State, spaceID string) (Output, error) {
	if p.Readers.Contacts == nil {
		return Output{}, fmt.Errorf("pipeline: no contacts reader configured")
	}
	cs, err := p.Readers.Contacts.List(ctx, spaceID)
	if err != nil {
		return Output{}, err
	}
	if len(cs) == 0 {
		st.LastShown = nil
		return Output{Text: "That space has no contacts.", Presentation: sneatdomain.PresentationContactsGrid}, nil
	}
	refs := make([]session.EntityRef, 0, len(cs))
	for _, c := range cs {
		refs = append(refs, session.EntityRef{Type: sneatdomain.EntityContact, Title: c.Name,
			Keys: map[string]string{"spaceID": c.SpaceID, "contactID": c.ID}})
	}
	st.LastShown = refs
	return Output{Text: fmt.Sprintf("%d contact(s).", len(cs)), Presentation: sneatdomain.PresentationContactsGrid, Entities: refs}, nil
}

// resolveAndAct resolves ref, then either stages a Pending confirmation
// (destructive/ambiguous kinds), asks the user to pick among several
// candidates, or asks them to clarify when nothing matched. It never
// executes a destructive action without confirmation (brief §17).
func (p Pipeline) resolveAndAct(ctx context.Context, kind string, ref decision.Reference, slots map[string]string, st *session.State, spaceID string) (Output, error) {
	res, err := p.Resolver.Resolve(ctx, ref, *st, spaceID)
	if err != nil {
		return Output{}, err
	}
	switch res.Outcome {
	case OutcomeNone:
		return Output{Text: fmt.Sprintf("I couldn't find a %s matching %q.", ref.Kind, ref.Expression)}, nil
	case OutcomeMany:
		// S3: an ambiguous reference's candidates become the choice list a
		// later "2"/"the second one" picks from (pickFromLastShown) -- without
		// this, presenting a choice and then answering it by number would
		// silently fall through to the main LLM every time.
		st.LastShown = res.Candidates
		return Output{
			Text:         fmt.Sprintf("I found %d matches -- which one did you mean?", len(res.Candidates)),
			Presentation: sneatdomain.PresentationHappeningsList,
			Entities:     res.Candidates,
		}, nil
	}

	target := res.Candidates[0]
	// B3: a resolved target (focused/selected/sidebar-pinned, or fetched
	// fresh) whose OWN recorded space disagrees with the pipeline's current
	// space is refused outright -- e.g. a happening focused before the user
	// switched spaces must not let "cancel it" mutate the OLD space just
	// because the reference still resolves locally. SneatExecutor repeats
	// this check (defense in depth for a Pending action confirmed later, or
	// any other Execute caller), but resolveAndAct is where a fresh
	// resolution should be told "no" before ever building an action.
	if targetSpace := target.Keys["spaceID"]; targetSpace != "" && targetSpace != spaceID {
		return Output{Text: fmt.Sprintf("%q belongs to another space.", target.Title)}, nil
	}
	// B3: slots come from a decision/model (rules or the main LLM) -- strip
	// any spaceID it supplied, same as HandleAction's no-reference branch.
	args := spaceScopedArgs(slots, spaceID)
	summary := summaryFor(kind, target, slots)
	switch kind {
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening:
		if resolved, ok := p.rescheduleSummary(ctx, spaceID, target, slots["when"]); ok {
			summary = resolved
		}
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening:
		// B1: the confirmation must show the RESOLVED occurrence, computed
		// before asking, the same way rescheduleSummary does -- not a bare
		// "Cancel it?" that never lets the user see which day they're about
		// to cancel.
		if resolved, ok := p.cancelSummary(ctx, spaceID, target, slots["when"]); ok {
			summary = resolved
		}
	}
	action := session.Action{Kind: kind, Target: &target, Args: args, Summary: summary}
	if isDestructive(kind) {
		st.Pending = &action
		return Output{Text: action.Summary + " (yes/no)"}, nil
	}
	return p.runAction(ctx, spaceID, action, st)
}

// cancelSummary builds cancelHappening's confirmation text with the FULLY
// RESOLVED occurrence, computed BEFORE asking (B1 ruling: "confirmation
// computed BEFORE asking"), e.g. 'Cancel Fri Sep 25 "Yoga" (this occurrence
// only)?' for a recurring happening -- a single (non-recurring) happening
// has no occurrence distinct from itself, so it keeps the plain "Cancel
// %q?" text. ok is false when the happening can't be read; the caller
// falls back to summaryFor's plain text rather than a blank confirmation.
func (p Pipeline) cancelSummary(ctx context.Context, spaceID string, target session.EntityRef, when string) (string, bool) {
	if p.Readers.Happenings == nil {
		return "", false
	}
	current, err := p.Readers.Happenings.Get(ctx, spaceID, target.Keys["happeningID"])
	if err != nil {
		return "", false
	}
	title := target.Title
	if title == "" {
		title = "it"
	}
	if !current.Recurring {
		return fmt.Sprintf("Cancel %q?", title), true
	}
	occ := resolveOccurrenceDate(p.now(), current, when)
	return fmt.Sprintf("Cancel %s %q (this occurrence only)?", occ.Format("Mon Jan 2"), title), true
}

// pickFromLastShown handles a bare number/ordinal ("2", "the second one")
// picking an item from the most recent structured list/choice this session
// showed (S3), when nothing else classified the turn. It FOCUSES the picked
// entity rather than guessing at re-running whatever action was pending --
// this pipeline has no record of an intended action kind for an ambiguous
// reference beyond the Pending mechanism (which only holds an already-
// resolved, already-confirmed action), so the safe, still useful behaviour
// is to make the picked entity resolvable by a following pronoun ("it") the
// same way focusing it by hand would. handled is false for anything that
// isn't a recognised ordinal, or when there is nothing to pick from, so the
// caller's normal NeedsLLM fallback still applies.
func (p Pipeline) pickFromLastShown(text string, st *session.State) (Output, bool) {
	if len(st.LastShown) == 0 {
		return Output{}, false
	}
	pos, ok := parseOrdinal(text)
	if !ok {
		return Output{}, false
	}
	res, handled := resolveByPosition(pos, "", *st) // "" = any kind: this fallback has no reference kind to filter by
	if !handled {
		return Output{}, false
	}
	if res.Outcome != OutcomeOne {
		return Output{Text: fmt.Sprintf("There's no %s option -- I showed %d.", text, len(st.LastShown))}, true
	}
	ref := res.Candidates[0]
	st.Focus(&ref)
	title := ref.Title
	if title == "" {
		title = "that one"
	}
	return Output{Text: fmt.Sprintf("Got it: %q.", title)}, true
}

// rescheduleSummary builds the reschedule confirmation text with the FULLY
// RESOLVED date/time/timezone/occurrence, computed BEFORE asking (S4/B2
// ruling: "confirmation shows fully resolved date/time/TZ/occurrence
// computed before asking") -- not the raw "when" slot text ("Friday
// 16:00"), which would force the user to do the date math themselves just
// to confirm. It re-reads the target's current slot -- the same data
// rescheduleHappening itself reads at execution time -- so the confirmation
// and the eventual mutation resolve the anchor date/timezone identically.
// ok is false when the happening can't be read or "when" can't be parsed;
// the caller falls back to the plain summaryFor text rather than a blank
// confirmation.
func (p Pipeline) rescheduleSummary(ctx context.Context, spaceID string, target session.EntityRef, when string) (string, bool) {
	if p.Readers.Happenings == nil || when == "" {
		return "", false
	}
	current, err := p.Readers.Happenings.Get(ctx, spaceID, target.Keys["happeningID"])
	if err != nil || current.Slot == nil {
		return "", false
	}
	anchor := recurringAnchor(p.now(), current)
	newStart, ok := parseWhen(p.now(), anchor, when)
	if !ok {
		return "", false
	}
	loc := newStart.Location()
	if current.Slot.TimeZone != "" {
		if l, lerr := time.LoadLocation(current.Slot.TimeZone); lerr == nil {
			loc = l
		}
	}
	title := target.Title
	if title == "" {
		title = "it"
	}
	occurrence := ""
	if current.Recurring {
		occurrence = " (this occurrence only, not the whole series)"
	}
	return fmt.Sprintf("Move %q to %s%s?", title, newStart.In(loc).Format("Mon Jan 2 15:04 MST"), occurrence), true
}

func (p Pipeline) runAction(ctx context.Context, spaceID string, action session.Action, st *session.State) (Output, error) {
	if p.Executor == nil {
		return Output{}, fmt.Errorf("pipeline: no executor configured")
	}
	undo, err := p.Executor.Execute(ctx, spaceID, action)
	if err != nil {
		return Output{}, err
	}
	action.Undo = undo
	st.Previous = &action
	st.PreviousAt = p.now()
	text := "Done."
	if undo != nil {
		// m10: a non-destructive action (destructive ones already got a
		// confirmation prompt before execution, via resolveAndAct's
		// isDestructive check -- this only fires for the ones that ran
		// immediately, most often an LLM-originated action) still leaves an
		// undo hint, so "undo" is discoverable without having to guess it is
		// even possible.
		text += " Say \"undo\" to reverse it."
	}
	return Output{Text: text}, nil
}

func (p Pipeline) confirmPending(ctx context.Context, spaceID string, st *session.State) (Output, error) {
	if st.Pending == nil {
		return Output{Text: "There's nothing pending to confirm."}, nil
	}
	action := *st.Pending
	st.Pending = nil
	return p.runAction(ctx, spaceID, action, st)
}

func (p Pipeline) cancelPending(st *session.State) Output {
	if st.Pending == nil {
		return Output{Text: "There's nothing pending to cancel."}
	}
	st.Pending = nil
	return Output{Text: "Cancelled."}
}

func (p Pipeline) undoPrevious(ctx context.Context, spaceID string, st *session.State) (Output, error) {
	if st.Previous == nil || st.Previous.Undo == nil {
		return Output{Text: "There's nothing I can undo."}, nil
	}
	undo := *st.Previous.Undo
	if p.Executor == nil {
		return Output{}, fmt.Errorf("pipeline: no executor configured")
	}
	if _, err := p.Executor.Execute(ctx, spaceID, undo); err != nil {
		return Output{}, err
	}
	st.Previous = nil
	return Output{Text: "Undone."}, nil
}

func (p Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// summaryFor builds the human confirmation text for a destructive action.
func summaryFor(kind string, target session.EntityRef, slots map[string]string) string {
	title := target.Title
	if title == "" {
		title = "it"
	}
	switch kind {
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening:
		when := slots["when"]
		if when == "" {
			when = "a new time"
		}
		return fmt.Sprintf("Move %q to %s?", title, when)
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening:
		return fmt.Sprintf("Cancel %q?", title)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo:
		return fmt.Sprintf("Delete %q?", title)
	default:
		return fmt.Sprintf("%s %q?", kind, title)
	}
}

// isDestructive reports whether kind requires confirmation before running
// (brief §17: destructive/ambiguous operations use confirmation).
func isDestructive(kind string) bool {
	switch kind {
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening,
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening,
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo:
		return true
	default:
		return false
	}
}

// entityKindFor maps an action Kind ("calendar.reschedule_happening") to the
// session.EntityRef.Type its reference resolves against.
func entityKindFor(kind string) string {
	switch {
	case len(kind) >= len(sneatdomain.ModuleCalendar) && kind[:len(sneatdomain.ModuleCalendar)] == sneatdomain.ModuleCalendar:
		return sneatdomain.EntityHappening
	case len(kind) >= len(sneatdomain.ModuleTodo) && kind[:len(sneatdomain.ModuleTodo)] == sneatdomain.ModuleTodo:
		return sneatdomain.EntityTodo
	case len(kind) >= len(sneatdomain.ModuleContacts) && kind[:len(sneatdomain.ModuleContacts)] == sneatdomain.ModuleContacts:
		return sneatdomain.EntityContact
	default:
		return ""
	}
}

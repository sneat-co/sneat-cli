package pipeline

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sneat-co/sneat-ai-backend/temporal"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// Resolver turns a decision.Reference (or a splitter.Action's equivalent
// reference) into the real entity/entities it names. Jev/the main LLM may
// identify "my dentist appointment tomorrow", but only Resolver decides what
// that actually points at -- it never trusts a model-supplied entity ID
// because none exists in a Reference to begin with.
type Resolver struct {
	Readers data.Readers
	// Now returns the current time, used to resolve a temporal word
	// ("tomorrow", "Friday") found in a happening reference's expression
	// (S4). Nil means time.Now, matching Pipeline's own convention.
	Now func() time.Time
}

func (r Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Outcome is the result of resolving one reference.
type Outcome int

const (
	// OutcomeNone means no candidate was found -- the caller should ask the
	// user to clarify rather than guess.
	OutcomeNone Outcome = iota
	// OutcomeOne means exactly one candidate was found -- the caller may
	// proceed (subject to its own confirmation policy for destructive/
	// ambiguous actions).
	OutcomeOne
	// OutcomeMany means several plausible candidates were found -- the caller
	// should present a choice control, never guess.
	OutcomeMany
)

// Result is what Resolve found.
type Result struct {
	Outcome    Outcome
	Candidates []session.EntityRef // len 0, 1, or >1 matching Outcome
}

// Resolve resolves ref against real data in spaceID.
//
// A pronoun reference ("it", "that") resolves from session state in RANK
// TIERS, not a flat merge of session.State.Candidates() (S3): the focused
// entity wins outright when it matches ref.Kind, without regard to what
// else the session holds; failing that, the current selection wins outright
// (ambiguous only among several selected entities of that kind); only when
// neither is set does resolution fall through to the sole last-shown
// entity, the previous action's target, and sidebar pins, where several
// same-kind matches ARE ambiguous. A flat merge would let, say, a sidebar
// pin of the same kind make an otherwise-unambiguous focused entity look
// ambiguous, which is never what "it"/"that" means while something is
// focused or selected.
//
// A numeric/ordinal expression ("2", "the second one") instead picks
// positionally from st.LastShown -- the most recent structured list/choice
// this session showed -- regardless of ref.Kind, since a choice list is
// already a single, already-filtered set of candidates of one kind; there
// is nothing left to disambiguate once a valid position is named.
//
// Any other expression reference searches the matching reader by title/name.
func (r Resolver) Resolve(ctx context.Context, ref decision.Reference, st session.State, spaceID string) (Result, error) {
	if ref.Pronoun {
		return resolvePronoun(ref.Kind, st), nil
	}
	if pos, ok := parseOrdinal(ref.Expression); ok {
		if res, handled := resolveByPosition(pos, st); handled {
			return res, nil
		}
		// Not a valid position into LastShown (e.g. out of range, or nothing
		// was shown) -- fall through to a literal title/name search below,
		// same as any other expression (a happening genuinely titled "2" is
		// vanishingly unlikely but not this resolver's business to forbid).
	}
	if ref.Expression == "" {
		return Result{Outcome: OutcomeNone}, nil
	}

	switch ref.Kind {
	case sneatdomain.EntityHappening:
		if r.Readers.Happenings == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no happenings reader configured")
		}
		terms, window := significantTerms(r.now(), ref.Expression)
		hs, err := r.Readers.Happenings.FindByTitle(ctx, spaceID, strings.Join(terms, " "))
		if err != nil {
			return Result{}, err
		}
		hs = filterByWindow(hs, window)
		return classify(toEntityRefs(hs, func(h data.Happening) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityHappening, Title: h.Title,
				Keys: map[string]string{"spaceID": h.SpaceID, "happeningID": h.ID}}
		})), nil
	case sneatdomain.EntityTodo:
		if r.Readers.Todos == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no todos reader configured")
		}
		ts, err := r.Readers.Todos.FindByTitle(ctx, spaceID, ref.Expression)
		if err != nil {
			return Result{}, err
		}
		return classify(toEntityRefs(ts, func(td data.Todo) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityTodo, Title: td.Title,
				Keys: map[string]string{"spaceID": td.SpaceID, "list": td.List, "itemID": td.ID}}
		})), nil
	case sneatdomain.EntityContact:
		if r.Readers.Contacts == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no contacts reader configured")
		}
		cs, err := r.Readers.Contacts.FindByName(ctx, spaceID, ref.Expression)
		if err != nil {
			return Result{}, err
		}
		return classify(toEntityRefs(cs, func(c data.Contact) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityContact, Title: c.Name,
				Keys: map[string]string{"spaceID": c.SpaceID, "contactID": c.ID}}
		})), nil
	default:
		return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: unknown reference kind %q", ref.Kind)
	}
}

// resolvePronoun implements the tiered rank order documented on Resolve.
func resolvePronoun(kind string, st session.State) Result {
	if st.Focused != nil && st.Focused.Type == kind {
		return Result{Outcome: OutcomeOne, Candidates: []session.EntityRef{*st.Focused}}
	}
	if len(st.Selection) > 0 {
		var sel []session.EntityRef
		for _, s := range st.Selection {
			if s.Type == kind {
				sel = append(sel, s)
			}
		}
		if len(sel) > 0 {
			return classify(sel)
		}
	}
	// Neither focus nor selection named this kind: fall back to the rest of
	// Candidates()'s rank order (sole last-shown, previous target, sidebar
	// pins newest-first), where several same-kind matches ARE ambiguous.
	var rest []session.EntityRef
	add := func(ref session.EntityRef) {
		if ref.Type == kind && !slices.ContainsFunc(rest, ref.Same) {
			rest = append(rest, ref)
		}
	}
	if len(st.LastShown) == 1 {
		add(st.LastShown[0])
	}
	if st.Previous != nil && st.Previous.Target != nil {
		add(*st.Previous.Target)
	}
	for i := len(st.Sidebar) - 1; i >= 0; i-- {
		add(st.Sidebar[i])
	}
	return classify(rest)
}

// parseOrdinal recognises a choice-picking expression ("2", "#2", "the
// second one", "second") and returns its 1-based position. Only a small
// fixed word table is matched (first..tenth) -- not a general number-words
// parser, per the no-giant-NLP-engine rule; a choice list this MVP renders
// is never long enough to need more.
func parseOrdinal(expr string) (pos int, ok bool) {
	text := strings.ToLower(strings.TrimSpace(expr))
	text = strings.TrimPrefix(text, "#")
	text = strings.TrimPrefix(text, "the ")
	text = strings.TrimSuffix(text, " one")
	text = strings.TrimSpace(text)
	// Strip a numeric ordinal suffix ("2nd" -> "2") ONLY when what's left is
	// itself digits -- an unconditional TrimSuffix(text, "nd") would also
	// mutilate the word "second" into "seco".
	if n := len(text); n > 2 {
		switch text[n-2:] {
		case "st", "nd", "rd", "th":
			if _, err := strconv.Atoi(text[:n-2]); err == nil {
				text = text[:n-2]
			}
		}
	}
	if n, err := strconv.Atoi(text); err == nil && n > 0 {
		return n, true
	}
	words := map[string]int{
		"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
		"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
	}
	if n, known := words[text]; known {
		return n, true
	}
	return 0, false
}

// resolveByPosition picks the pos'th (1-based) entity from st.LastShown --
// the choice list a prior structured presentation set (day/week calendar, a
// todo list, an ambiguous-reference choice). handled is false when there is
// no LastShown to pick from at all, so the caller falls through to a
// literal search instead of reporting a hard "no match" for what might just
// be a coincidentally numeric title.
func resolveByPosition(pos int, st session.State) (Result, bool) {
	if len(st.LastShown) == 0 {
		return Result{}, false
	}
	if pos < 1 || pos > len(st.LastShown) {
		return Result{Outcome: OutcomeNone}, true
	}
	return Result{Outcome: OutcomeOne, Candidates: []session.EntityRef{st.LastShown[pos-1]}}, true
}

// referenceStopwords are dropped from a happening reference expression
// before it becomes a title search -- articles/pronouns/prepositions that
// carry no title-matching signal. Deliberately small and fixed, not a
// general stopword list, per the no-giant-NLP-engine rule.
var referenceStopwords = map[string]bool{
	"my": true, "the": true, "a": true, "an": true, "that": true, "this": true,
	"on": true, "at": true, "for": true, "to": true, "with": true, "of": true,
}

// dayWindow is a [from, to) calendar-day range a temporal word in a
// reference expression narrows a happening search to.
type dayWindow struct{ from, to time.Time }

// significantTerms splits a happening reference expression ("my dentist
// appointment tomorrow") into its significant search words (stopwords
// removed) and, when one word names a temporal window ("tomorrow",
// "Friday" -- resolved via sneat-ai-backend's temporal package, per the
// reuse-existing-sneat-code rule), the day window it resolves to (S4). The
// temporal word itself is excluded from the returned terms: it narrows
// WHEN, it is not part of the title being searched for.
func significantTerms(now time.Time, expr string) (terms []string, win *dayWindow) {
	words := strings.Fields(expr)
	kept := make([]string, 0, len(words))
	for _, w := range words {
		lw := strings.ToLower(w)
		if referenceStopwords[lw] {
			continue
		}
		if win == nil {
			if d, ok := temporal.ParseText(now, lw); ok {
				from := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
				win = &dayWindow{from: from, to: from.AddDate(0, 0, 1)}
				continue // the temporal word narrows WHEN, not the title
			}
		}
		kept = append(kept, w)
	}
	return kept, win
}

// filterByWindow keeps only happenings whose Start falls in win, or that
// are recurring (a recurring happening's Start is its stored template date,
// not a resolved occurrence -- see HappeningsReader's documented
// limitation -- so it is never excluded on that basis, matching Window's
// own semantics). A nil window keeps everything.
func filterByWindow(hs []data.Happening, win *dayWindow) []data.Happening {
	if win == nil {
		return hs
	}
	out := hs[:0]
	for _, h := range hs {
		if h.Recurring || (!h.Start.IsZero() && !h.Start.Before(win.from) && h.Start.Before(win.to)) {
			out = append(out, h)
		}
	}
	return out
}

func classify(refs []session.EntityRef) Result {
	switch len(refs) {
	case 0:
		return Result{Outcome: OutcomeNone}
	case 1:
		return Result{Outcome: OutcomeOne, Candidates: refs}
	default:
		return Result{Outcome: OutcomeMany, Candidates: refs}
	}
}

func toEntityRefs[T any](items []T, conv func(T) session.EntityRef) []session.EntityRef {
	out := make([]session.EntityRef, 0, len(items))
	for _, it := range items {
		out = append(out, conv(it))
	}
	return out
}

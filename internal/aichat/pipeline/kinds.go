package pipeline

import "github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"

// ActionKindSpec documents one <sneat-action> kind the main LLM may emit.
// sneatActionInstruction (llm.go) is BUILT from SupportedActionKinds()
// rather than a hand-typed prose paragraph the real executor cases could
// silently drift out of sync with -- the root cause of the "buy.add" bug
// (founder, buy-add-prompt.md scratchpad): the old instruction never
// enumerated valid kinds at all, so the model guessed one HandleAction had
// no case for, and the resulting error ("pipeline: no executor case for
// action kind \"buy.add\"") leaked straight to the user as "system: error:
// ...".
type ActionKindSpec struct {
	// Kind is "<module>.<intent>", exactly what Action.Kind must equal.
	Kind string
	// Meaning is a one-line description, for the model.
	Meaning string
	// Slots documents the exact slots.<name> contract this kind reads, or
	// "" when it takes none (a reference/pronoun alone is enough).
	Slots string
}

// executorActionKinds are the kinds SneatExecutor.Execute's dispatch table
// (its handlers() map) actually runs when reached from HandleAction, MINUS
// the two undo-only kinds (calendarRevokeCancellationKind/
// calendarCancelAdjustmentKind) that map also carries -- those are never
// something a decision or the main LLM asks for directly (see their own
// doc comments in sneatexecutor.go), so they are deliberately never
// offered to the model here.
func executorActionKinds() []ActionKindSpec {
	return []ActionKindSpec{
		{
			Kind:    sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening,
			Meaning: "Move a happening to a new time (a recurring happening moves only this occurrence, never the whole series).",
			Slots:   `"when": the new time as free text, e.g. "Friday 16:00" or "16:00" (keeps the current date)`,
		},
		{
			Kind:    sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening,
			Meaning: "Cancel a happening (a recurring happening cancels only this occurrence, never the whole series).",
			Slots:   `"when": optional -- which occurrence to cancel, e.g. "Friday", when the happening is recurring`,
		},
		{
			Kind:    sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo,
			Meaning: "Mark a todo item done.",
		},
		{
			Kind:    sneatdomain.ModuleTodo + "." + sneatdomain.IntentReopenTodo,
			Meaning: "Mark a completed todo item not done again.",
		},
		{
			Kind:    sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo,
			Meaning: "Delete a todo item.",
		},
		{
			Kind:    sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo,
			Meaning: `Add a new item to the todo ("do") list. No reference -- this always creates a new item.`,
			Slots:   `"title": the item text. For several items, put them comma-separated in slots.title (e.g. "call dentist, pay rent") -- the app splits on commas into separate items; do NOT use the word "and" as a separator (a title like "fish and chips" or "salt and pepper" is one item, and using "and" to join several items would corrupt it), and never emit more than one action block to add several items`,
		},
		{
			Kind:    sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
			Meaning: `Add a new item to the to-buy (shopping) list. No reference -- this always creates a new item.`,
			Slots:   `"title": the item text. For several items, put them comma-separated in slots.title (e.g. "milk and bread" -> "milk, bread") -- the app splits on commas into separate items; do NOT use the word "and" as a separator (a title like "fish and chips" or "salt and pepper" is one item, and using "and" to join several items would corrupt it), and never emit more than one action block to add several items`,
		},
	}
}

// contactLookupActionKinds are the two read-only kinds HandleAction routes
// to findOrShowContact (see contacts.go's isContactLookup) instead of
// Executor -- there is nothing to execute, only a lookup to render.
func contactLookupActionKinds() []ActionKindSpec {
	return []ActionKindSpec{
		{Kind: sneatdomain.ModuleContacts + "." + sneatdomain.IntentFindContact, Meaning: `Look up one contact by name or relationship (e.g. "my wife").`},
		{Kind: sneatdomain.ModuleContacts + "." + sneatdomain.IntentShowContact, Meaning: "Show one contact's card by name or relationship."},
	}
}

// SupportedActionKinds is every kind HandleAction can actually carry out --
// the executor-backed ones plus the two contact lookups -- in the exact
// order sneatActionInstruction lists them to the model. This is the single
// source of truth both the instruction text (llm.go) and
// TestSupportedActionKinds_MatchesExecutor (kinds_test.go) read from, so
// the advertised list and HandleAction's real behaviour cannot drift apart
// the way they did before this bug fix.
func SupportedActionKinds() []ActionKindSpec {
	out := append([]ActionKindSpec{}, executorActionKinds()...)
	return append(out, contactLookupActionKinds()...)
}

// aliasActionKinds maps a few explicit, product-picked aliases -- kinds a
// main-LLM turn has been observed to reach for despite the instruction
// naming the real one (the founder's own reported case: "buy.add" instead
// of "todo.add_to_buy") -- to the kind HandleAction actually understands.
// Deliberately small and explicit, never fuzzy: a kind not listed here
// passes through normalizeActionKind unchanged, and an alias that still
// isn't a SupportedActionKinds entry (e.g. "calendar.add", which names a
// real but unsupported taxonomy intent) falls through to
// unsupportedActionText same as any other unknown kind.
var aliasActionKinds = map[string]string{
	"buy.add":      sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
	"to_buy.add":   sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
	"shopping.add": sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy,
	"todo.add":     sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo,
	"calendar.add": sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening,
}

// normalizeActionKind resolves kind through aliasActionKinds, or returns it
// unchanged when it isn't a known alias.
func normalizeActionKind(kind string) string {
	if real, ok := aliasActionKinds[kind]; ok {
		return real
	}
	return kind
}

// supportedActionKindSet is SupportedActionKinds() as a membership set,
// built once at package init -- HandleAction (pipeline.go) checks every
// normalized kind against it before ever building a session.Action.
var supportedActionKindSet = func() map[string]bool {
	set := make(map[string]bool, len(executorActionKinds())+len(contactLookupActionKinds()))
	for _, k := range SupportedActionKinds() {
		set[k.Kind] = true
	}
	return set
}()

// isSupportedActionKind reports whether kind (already normalized) is one
// HandleAction can carry out.
func isSupportedActionKind(kind string) bool {
	return supportedActionKindSet[kind]
}

// IsSupportedActionKind reports whether kind is one HandleAction can carry
// out, after alias normalisation -- exported so a caller's own diagnostics
// (chatapp's debug log) can flag an unsupported/aliased kind a main-LLM
// turn emitted, without HandleAction itself needing a logger dependency
// this leaf package doesn't otherwise have.
func IsSupportedActionKind(kind string) bool {
	return isSupportedActionKind(normalizeActionKind(kind))
}

// unsupportedActionText answers a normalized-but-unsupported kind in plain
// text (S? ruling UNSUPPORTED-KIND, founder buy-add-prompt.md): naming the
// specific gap for a real taxonomy intent that simply has no chat-side
// executor yet, and a generic refusal for anything else (a kind the model
// invented outright).
func unsupportedActionText(kind string) string {
	switch kind {
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening,
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentUpdateHappening:
		return "Adding or updating calendar events from chat isn't supported yet -- add it in the Sneat app."
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentUpdateTodo:
		return "Updating a todo's details from chat isn't supported yet -- add it in the Sneat app."
	default:
		return "I can't do that from chat yet."
	}
}

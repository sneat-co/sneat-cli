// Package rules is Sneat's deterministic decision.Provider: a small phrase
// table built on strongo/aichat's ai/decision/rules engine, not a regex NLP
// engine. It runs first in the decision chain (founder addendum: "a chain of
// decision providers and the first that decided we use").
package rules

import (
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/decision/rules"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// Name identifies this provider in decision.Trace/Attempt and diagnostics.
const Name = "sneat-rules"

// New builds Sneat's deterministic rules provider.
func New() *rules.Provider {
	return rules.New(Name,
		showDayRule(),
		showWeekRule(),
		showUpcomingRule(),
		listTodosRule(),
		listToBuyRule(),
		listContactsRule(),
		helpRule(),
		// Confirmation/rejection/cancellation/undo only fire when the session
		// has a Pending (or, for undo, a Previous) action -- an unambiguous
		// "yes" with nothing pending is not this rule's job; it falls through
		// to the next provider / the main LLM.
		confirmRule(),
		rejectRule(),
		undoRule(),
	)
}

func decided(module, intent, presentation string) decision.Decision {
	return decision.Decision{
		Module:                     decision.Scored{Value: module, Confidence: 1},
		Intent:                     decision.Scored{Value: intent, Confidence: 1},
		Interaction:                decision.InteractionCommand,
		CanHandleDeterministically: true,
		NeedsLLM:                   false,
		Presentation:               presentation,
	}
}

func showDayRule() rules.Rule {
	match := rules.Phrases("show my calendar today", "calendar today", "today", "show today", "what's on today", "whats on today")
	return rules.Rule{Name: "calendar.show_day", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleCalendar, sneatdomain.IntentShowDay, sneatdomain.PresentationDayCalendar)
		d.Slots = map[string]string{"when": "today"}
		d.RequiredData = []string{sneatdomain.DataTodayHappenings}
		return d, true
	}}
}

func showWeekRule() rules.Rule {
	match := rules.Phrases("this week", "show this week", "what's happening this week", "whats happening this week", "show my week", "calendar this week")
	return rules.Rule{Name: "calendar.show_week", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleCalendar, sneatdomain.IntentShowWeek, sneatdomain.PresentationWeekCalendar)
		d.Slots = map[string]string{"when": "this_week"}
		d.RequiredData = []string{sneatdomain.DataWeekHappenings}
		return d, true
	}}
}

func showUpcomingRule() rules.Rule {
	match := rules.Phrases("show upcoming", "what's upcoming", "whats upcoming", "upcoming", "show my upcoming events")
	return rules.Rule{Name: "calendar.show_upcoming", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleCalendar, sneatdomain.IntentShowUpcoming, sneatdomain.PresentationHappeningsList)
		d.RequiredData = []string{sneatdomain.DataRelevantHappenings}
		return d, true
	}}
}

func listTodosRule() rules.Rule {
	match := rules.Phrases("my todos", "todo list", "show my todos", "list my todos", "todos")
	return rules.Rule{Name: "todo.list_todos", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleTodo, sneatdomain.IntentListTodos, sneatdomain.PresentationTodoList)
		d.RequiredData = []string{sneatdomain.DataTodos}
		return d, true
	}}
}

func listToBuyRule() rules.Rule {
	match := rules.Phrases("to buy", "shopping list", "my shopping list", "buy list", "what do i need to buy")
	return rules.Rule{Name: "todo.list_to_buy", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleTodo, sneatdomain.IntentListToBuy, sneatdomain.PresentationBuyList)
		d.RequiredData = []string{sneatdomain.DataTodos}
		return d, true
	}}
}

func listContactsRule() rules.Rule {
	match := rules.Phrases("contacts", "my contacts", "show my contacts", "list contacts", "list my contacts")
	return rules.Rule{Name: "contacts.list_contacts", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		d := decided(sneatdomain.ModuleContacts, sneatdomain.IntentListContacts, sneatdomain.PresentationContactsGrid)
		d.RequiredData = []string{sneatdomain.DataContacts}
		return d, true
	}}
}

func helpRule() rules.Rule {
	match := rules.Phrases("help", "explain what i can do here", "what can you do", "what can i do here")
	return rules.Rule{Name: "general.help", Match: func(text string, _ session.State) (decision.Decision, bool) {
		if !match(text) {
			return decision.Decision{}, false
		}
		return decided(sneatdomain.ModuleGeneral, sneatdomain.IntentHelp, sneatdomain.PresentationText), true
	}}
}

// confirmWords/rejectWords/undoWords are exact-phrase, not substring, so
// "yes please schedule it" does not accidentally confirm -- a real product
// would widen this table deliberately, not with a regex.
var (
	confirmWords = rules.Phrases("yes", "y", "ok", "okay", "confirm", "do it", "yep", "yeah")
	rejectWords  = rules.Phrases("no", "n", "cancel", "cancel that", "never mind", "nevermind", "stop")
	undoWords    = rules.Phrases("undo", "undo that", "undo it")
)

func confirmRule() rules.Rule {
	return rules.Rule{Name: "system.confirm", Match: func(text string, st session.State) (decision.Decision, bool) {
		if st.Pending == nil || !confirmWords(text) {
			return decision.Decision{}, false
		}
		d := decision.Decision{
			Module:                     decision.Scored{Value: sneatdomain.ModuleGeneral, Confidence: 1},
			Interaction:                decision.InteractionConfirmation,
			CanHandleDeterministically: true,
		}
		return d, true
	}}
}

func rejectRule() rules.Rule {
	return rules.Rule{Name: "system.reject", Match: func(text string, st session.State) (decision.Decision, bool) {
		if st.Pending == nil || !rejectWords(text) {
			return decision.Decision{}, false
		}
		interaction := decision.InteractionRejection
		if text == "cancel that" || text == "stop" {
			interaction = decision.InteractionCancellation
		}
		d := decision.Decision{
			Module:                     decision.Scored{Value: sneatdomain.ModuleGeneral, Confidence: 1},
			Interaction:                interaction,
			CanHandleDeterministically: true,
		}
		return d, true
	}}
}

func undoRule() rules.Rule {
	return rules.Rule{Name: "system.undo", Match: func(text string, st session.State) (decision.Decision, bool) {
		if st.Previous == nil || !undoWords(text) {
			return decision.Decision{}, false
		}
		d := decision.Decision{
			Module:                     decision.Scored{Value: sneatdomain.ModuleGeneral, Confidence: 1},
			Interaction:                decision.InteractionUndo,
			CanHandleDeterministically: true,
		}
		return d, true
	}}
}

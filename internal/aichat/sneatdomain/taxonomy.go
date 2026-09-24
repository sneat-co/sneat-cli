// Package sneatdomain is Sneat's decision vocabulary for the aichat pipeline:
// the modules, intents, presentations, data kinds and entity types
// strongo/aichat's decision.Taxonomy carries to any DecisionProvider (rules,
// the main LLM, or Jev) so it stays product-neutral.
//
// This package owns naming only. It has no behaviour: rules, resolvers and
// controls each interpret these strings for their own purpose.
package sneatdomain

import "github.com/strongo/aichat/ai/decision"

// Module names.
const (
	ModuleCalendar = "calendar"
	ModuleTodo     = "todo"
	ModuleContacts = "contacts"
	ModuleGeneral  = "general"
)

// Calendar intents.
const (
	IntentAddHappening        = "add_happening"
	IntentCancelHappening     = "cancel_happening"
	IntentRescheduleHappening = "reschedule_happening"
	IntentUpdateHappening     = "update_happening"
	IntentFindHappening       = "find_happening"
	IntentShowDay             = "show_day"
	IntentShowWeek            = "show_week"
	IntentShowUpcoming        = "show_upcoming"
)

// Todo intents.
const (
	IntentAddTodo      = "add_todo"
	IntentCompleteTodo = "complete_todo"
	IntentReopenTodo   = "reopen_todo"
	IntentUpdateTodo   = "update_todo"
	IntentDeleteTodo   = "delete_todo"
	IntentFindTodo     = "find_todo"
	IntentListTodos    = "list_todos"
	IntentAddToBuy     = "add_to_buy"
	IntentListToBuy    = "list_to_buy"
)

// Contacts intents.
const (
	IntentFindContact  = "find_contact"
	IntentListContacts = "list_contacts"
	IntentShowContact  = "show_contact"
)

// General intents.
const (
	IntentHelp = "help"
	IntentChat = "chat"
)

// Presentations: the semantic structured-control kind a decision or the main
// LLM's action block may ask for. The renderer (Phase 2) owns turning one of
// these into an actual transcript.Block.
const (
	PresentationHappeningCard  = "happening_card"
	PresentationDayCalendar    = "day_calendar"
	PresentationWeekCalendar   = "week_calendar"
	PresentationHappeningsList = "happenings_list"
	PresentationTodoList       = "todo_list"
	PresentationBuyList        = "buy_list"
	PresentationContactCard    = "contact_card"
	PresentationContactsGrid   = "contacts_grid"
	PresentationText           = "text"
)

// Data kinds: dynamic data a decision's RequiredData may name, for the
// Context Manager to fetch.
const (
	DataRelevantHappenings = "relevant_happenings"
	DataTodayHappenings    = "today_happenings"
	DataWeekHappenings     = "week_happenings"
	DataTodos              = "todos"
	DataContacts           = "contacts"
	DataFocusedEntities    = "focused_entities"
)

// Entity types: session.EntityRef.Type values this product defines.
const (
	EntityHappening = "happening"
	EntityTodo      = "todo"
	EntityContact   = "contact"
)

// Taxonomy is Sneat's decision.Taxonomy, sent with every decision request.
func Taxonomy() decision.Taxonomy {
	return decision.Taxonomy{
		Modules: []decision.ModuleSpec{
			{
				Name: ModuleCalendar,
				Intents: []string{
					IntentAddHappening, IntentCancelHappening, IntentRescheduleHappening,
					IntentUpdateHappening, IntentFindHappening, IntentShowDay, IntentShowWeek,
					IntentShowUpcoming,
				},
			},
			{
				Name: ModuleTodo,
				Intents: []string{
					IntentAddTodo, IntentCompleteTodo, IntentReopenTodo, IntentUpdateTodo,
					IntentDeleteTodo, IntentFindTodo, IntentListTodos, IntentAddToBuy, IntentListToBuy,
				},
			},
			{
				Name:    ModuleContacts,
				Intents: []string{IntentFindContact, IntentListContacts, IntentShowContact},
			},
			{
				Name:    ModuleGeneral,
				Intents: []string{IntentHelp, IntentChat},
			},
		},
		Presentations: []string{
			PresentationHappeningCard, PresentationDayCalendar, PresentationWeekCalendar,
			PresentationHappeningsList, PresentationTodoList, PresentationBuyList,
			PresentationContactCard, PresentationContactsGrid, PresentationText,
		},
		DataKinds: []string{
			DataRelevantHappenings, DataTodayHappenings, DataWeekHappenings, DataTodos,
			DataContacts, DataFocusedEntities,
		},
		EntityTypes: []string{EntityHappening, EntityTodo, EntityContact},
	}
}

package sneatdomain

import "testing"

// TestTaxonomy asserts Taxonomy() wires every declared module, intent,
// presentation, data kind and entity type constant into the returned
// decision.Taxonomy, so a future constant added to this package but left
// out of Taxonomy() fails the test instead of silently vanishing from the
// wire contract.
func TestTaxonomy(t *testing.T) {
	tx := Taxonomy()

	if len(tx.Modules) != 4 {
		t.Fatalf("expected 4 modules, got %d", len(tx.Modules))
	}

	wantModules := map[string][]string{
		ModuleCalendar: {
			IntentAddHappening, IntentCancelHappening, IntentRescheduleHappening,
			IntentUpdateHappening, IntentFindHappening, IntentShowDay, IntentShowWeek,
			IntentShowUpcoming,
		},
		ModuleTodo: {
			IntentAddTodo, IntentCompleteTodo, IntentReopenTodo, IntentUpdateTodo,
			IntentDeleteTodo, IntentFindTodo, IntentListTodos, IntentAddToBuy, IntentListToBuy,
		},
		ModuleContacts: {IntentFindContact, IntentListContacts, IntentShowContact},
		ModuleGeneral:  {IntentHelp, IntentChat},
	}

	seen := map[string]bool{}
	for _, m := range tx.Modules {
		seen[m.Name] = true
		want, ok := wantModules[m.Name]
		if !ok {
			t.Fatalf("unexpected module %q", m.Name)
		}
		if len(m.Intents) != len(want) {
			t.Fatalf("module %q: expected %d intents, got %d (%v)", m.Name, len(want), len(m.Intents), m.Intents)
		}
		for i, intent := range want {
			if m.Intents[i] != intent {
				t.Errorf("module %q intent[%d] = %q, want %q", m.Name, i, m.Intents[i], intent)
			}
		}
	}
	for name := range wantModules {
		if !seen[name] {
			t.Errorf("module %q missing from Taxonomy()", name)
		}
	}

	wantPresentations := []string{
		PresentationHappeningCard, PresentationDayCalendar, PresentationWeekCalendar,
		PresentationHappeningsList, PresentationTodoList, PresentationBuyList,
		PresentationContactCard, PresentationContactsGrid, PresentationText,
	}
	if len(tx.Presentations) != len(wantPresentations) {
		t.Fatalf("expected %d presentations, got %d", len(wantPresentations), len(tx.Presentations))
	}
	for i, p := range wantPresentations {
		if tx.Presentations[i] != p {
			t.Errorf("presentation[%d] = %q, want %q", i, tx.Presentations[i], p)
		}
	}

	wantDataKinds := []string{
		DataRelevantHappenings, DataTodayHappenings, DataWeekHappenings, DataTodos,
		DataContacts, DataFocusedEntities,
	}
	if len(tx.DataKinds) != len(wantDataKinds) {
		t.Fatalf("expected %d data kinds, got %d", len(wantDataKinds), len(tx.DataKinds))
	}
	for i, d := range wantDataKinds {
		if tx.DataKinds[i] != d {
			t.Errorf("dataKind[%d] = %q, want %q", i, tx.DataKinds[i], d)
		}
	}

	wantEntityTypes := []string{EntityHappening, EntityTodo, EntityContact}
	if len(tx.EntityTypes) != len(wantEntityTypes) {
		t.Fatalf("expected %d entity types, got %d", len(wantEntityTypes), len(tx.EntityTypes))
	}
	for i, e := range wantEntityTypes {
		if tx.EntityTypes[i] != e {
			t.Errorf("entityType[%d] = %q, want %q", i, tx.EntityTypes[i], e)
		}
	}
}

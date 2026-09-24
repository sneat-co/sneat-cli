package data

import (
	"context"
	"testing"
	"time"
)

// TestFakeHappenings covers Window's non-recurring/recurring/space-filter
// branches, FindByTitle's match/no-match, and Get's found/not-found.
func TestFakeHappenings(t *testing.T) {
	day := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	f := &FakeHappenings{Items: []Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist", Start: day},
		{ID: "h2", SpaceID: "sp1", Title: "Standup", Recurring: true, Start: day.AddDate(0, 0, -30)},
		{ID: "h3", SpaceID: "sp2", Title: "Other space", Start: day},
		{ID: "h4", SpaceID: "sp1", Title: "Out of window", Start: day.AddDate(0, 0, 10)},
	}}

	from := day.Add(-time.Hour)
	to := day.Add(time.Hour)
	got, err := f.Window(context.Background(), "sp1", from, to)
	if err != nil {
		t.Fatalf("Window() = %v, want nil", err)
	}
	gotIDs := map[string]bool{}
	for _, h := range got {
		gotIDs[h.ID] = true
	}
	if !gotIDs["h1"] || !gotIDs["h2"] || gotIDs["h3"] || gotIDs["h4"] {
		t.Fatalf("Window() = %+v, want h1 (in range) and h2 (recurring), not h3 (other space) or h4 (out of range)", got)
	}

	found, err := f.FindByTitle(context.Background(), "sp1", "dent")
	if err != nil || len(found) != 1 || found[0].ID != "h1" {
		t.Fatalf("FindByTitle(dent) = %+v, %v", found, err)
	}
	none, err := f.FindByTitle(context.Background(), "sp1", "zzz")
	if err != nil || len(none) != 0 {
		t.Fatalf("FindByTitle(zzz) = %+v, %v", none, err)
	}

	got1, err := f.Get(context.Background(), "sp1", "h1")
	if err != nil || got1.ID != "h1" {
		t.Fatalf("Get(h1) = %+v, %v", got1, err)
	}
	if _, err := f.Get(context.Background(), "sp1", "missing"); err == nil {
		t.Fatal("Get(missing) = nil error, want not-found")
	}
}

// TestFakeTodos covers List's space/list filter + not-done-first sort, and
// FindByTitle's match/no-match.
func TestFakeTodos(t *testing.T) {
	f := &FakeTodos{Items: []Todo{
		{ID: "t1", SpaceID: "sp1", List: ListKindDo, Title: "Buy milk", Done: true},
		{ID: "t2", SpaceID: "sp1", List: ListKindDo, Title: "Call mom", Done: false},
		{ID: "t3", SpaceID: "sp1", List: ListKindBuy, Title: "Wrong list"},
		{ID: "t4", SpaceID: "sp2", List: ListKindDo, Title: "Other space"},
	}}

	got, err := f.List(context.Background(), "sp1", ListKindDo)
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(got) != 2 || got[0].ID != "t2" || got[1].ID != "t1" {
		t.Fatalf("List() = %+v, want not-done (t2) before done (t1)", got)
	}

	found, err := f.FindByTitle(context.Background(), "sp1", "milk")
	if err != nil || len(found) != 1 || found[0].ID != "t1" {
		t.Fatalf("FindByTitle(milk) = %+v, %v", found, err)
	}
	none, err := f.FindByTitle(context.Background(), "sp1", "zzz")
	if err != nil || len(none) != 0 {
		t.Fatalf("FindByTitle(zzz) = %+v, %v", none, err)
	}
}

// TestFakeContacts covers List's space filter, FindByName's match/no-match,
// and Get's found/not-found.
func TestFakeContacts(t *testing.T) {
	f := &FakeContacts{Items: []Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice"},
		{ID: "c2", SpaceID: "sp2", Name: "Bob"},
	}}

	got, err := f.List(context.Background(), "sp1")
	if err != nil || len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("List() = %+v, %v", got, err)
	}

	found, err := f.FindByName(context.Background(), "sp1", "ali")
	if err != nil || len(found) != 1 || found[0].ID != "c1" {
		t.Fatalf("FindByName(ali) = %+v, %v", found, err)
	}
	none, err := f.FindByName(context.Background(), "sp1", "zzz")
	if err != nil || len(none) != 0 {
		t.Fatalf("FindByName(zzz) = %+v, %v", none, err)
	}

	got1, err := f.Get(context.Background(), "sp1", "c1")
	if err != nil || got1.ID != "c1" {
		t.Fatalf("Get(c1) = %+v, %v", got1, err)
	}
	if _, err := f.Get(context.Background(), "sp1", "missing"); err == nil {
		t.Fatal("Get(missing) = nil error, want not-found")
	}
}

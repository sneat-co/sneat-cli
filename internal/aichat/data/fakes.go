package data

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FakeHappenings is an in-memory HappeningsReader for tests.
type FakeHappenings struct {
	Items []Happening
}

func (f *FakeHappenings) Window(_ context.Context, spaceID string, from, to time.Time) ([]Happening, error) {
	var out []Happening
	for _, h := range f.Items {
		if h.SpaceID != spaceID {
			continue
		}
		if h.Recurring || (!h.Start.Before(from) && h.Start.Before(to)) {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *FakeHappenings) FindByTitle(_ context.Context, spaceID, query string) ([]Happening, error) {
	q := strings.ToLower(query)
	var out []Happening
	for _, h := range f.Items {
		if h.SpaceID == spaceID && strings.Contains(strings.ToLower(h.Title), q) {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *FakeHappenings) Get(_ context.Context, spaceID, happeningID string) (Happening, error) {
	for _, h := range f.Items {
		if h.SpaceID == spaceID && h.ID == happeningID {
			return h, nil
		}
	}
	return Happening{}, fmt.Errorf("happening %q not found", happeningID)
}

// FakeTodos is an in-memory TodosReader for tests.
type FakeTodos struct {
	Items []Todo
}

func (f *FakeTodos) List(_ context.Context, spaceID, list string) ([]Todo, error) {
	var out []Todo
	for _, it := range f.Items {
		if it.SpaceID == spaceID && it.List == list {
			out = append(out, it)
		}
	}
	sortNotDoneFirst(out)
	return out, nil
}

func (f *FakeTodos) FindByTitle(_ context.Context, spaceID, query string) ([]Todo, error) {
	q := strings.ToLower(query)
	var out []Todo
	for _, it := range f.Items {
		if it.SpaceID == spaceID && strings.Contains(strings.ToLower(it.Title), q) {
			out = append(out, it)
		}
	}
	return out, nil
}

// FakeContacts is an in-memory ContactsReader for tests.
type FakeContacts struct {
	Items []Contact
}

func (f *FakeContacts) List(_ context.Context, spaceID string) ([]Contact, error) {
	var out []Contact
	for _, c := range f.Items {
		if c.SpaceID == spaceID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *FakeContacts) FindByName(_ context.Context, spaceID, query string) ([]Contact, error) {
	q := strings.ToLower(query)
	var out []Contact
	for _, c := range f.Items {
		if c.SpaceID == spaceID && strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out, nil
}

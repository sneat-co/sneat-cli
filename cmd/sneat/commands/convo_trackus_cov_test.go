package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sneat-co/sneat-ext-contracts/contactus/contract4contactus"
)

// fakeContactusService is a controllable contract4contactus.ConvoService used
// to exercise seedSandboxContacts's error and dedup branches without needing
// two different real DB transaction kinds (read vs write) to fail.
type fakeContactusService struct {
	listContacts     []contract4contactus.Contact
	listContactsErr  error
	createContactErr error
	createCalls      []contract4contactus.CreateContactRequest
}

func (f *fakeContactusService) ListContacts(context.Context, string) ([]contract4contactus.Contact, error) {
	if f.listContactsErr != nil {
		return nil, f.listContactsErr
	}
	return f.listContacts, nil
}

func (f *fakeContactusService) CreateContact(_ context.Context, request contract4contactus.CreateContactRequest) (contract4contactus.Contact, error) {
	f.createCalls = append(f.createCalls, request)
	if f.createContactErr != nil {
		return contract4contactus.Contact{}, f.createContactErr
	}
	return contract4contactus.Contact{ID: "new-id", Name: request.Name}, nil
}

func (f *fakeContactusService) DeleteContact(context.Context, string, string) error {
	return nil
}

func withFakeContactusService(t *testing.T, fake *fakeContactusService) {
	t.Helper()
	prev := contactusServiceFactory
	contactusServiceFactory = func() contract4contactus.ConvoService { return fake }
	t.Cleanup(func() { contactusServiceFactory = prev })
}

func TestSeedSandboxContacts_ListContactsError_Wrapped(t *testing.T) {
	fake := &fakeContactusService{listContactsErr: errors.New("boom: list failed")}
	withFakeContactusService(t, fake)

	err := seedSandboxContacts(context.Background(), "user1", "space1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "failed to list sandbox contacts") {
		t.Errorf("expected wrapped list error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: list failed") {
		t.Errorf("expected underlying error text preserved, got: %v", err)
	}
	if len(fake.createCalls) != 0 {
		t.Errorf("must not attempt to create contacts after a list failure, got %d calls", len(fake.createCalls))
	}
}

func TestSeedSandboxContacts_AlreadyPresent_SkipsCreate(t *testing.T) {
	fake := &fakeContactusService{
		listContacts: []contract4contactus.Contact{
			{ID: "c1", Name: "Sarah Connor"},
			{ID: "c2", Name: "John Smith"},
		},
	}
	withFakeContactusService(t, fake)

	if err := seedSandboxContacts(context.Background(), "user1", "space1"); err != nil {
		t.Fatalf("seedSandboxContacts: %v", err)
	}
	if len(fake.createCalls) != 0 {
		t.Errorf("both demo contacts already present: expected 0 CreateContact calls, got %d (%v)", len(fake.createCalls), fake.createCalls)
	}
}

func TestSeedSandboxContacts_CreateContactError_Wrapped(t *testing.T) {
	fake := &fakeContactusService{
		listContacts:     nil,
		createContactErr: errors.New("boom: create failed"),
	}
	withFakeContactusService(t, fake)

	err := seedSandboxContacts(context.Background(), "user1", "space1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `failed to seed sandbox contact "Sarah Connor"`) {
		t.Errorf("expected wrapped create error naming the contact, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: create failed") {
		t.Errorf("expected underlying error text preserved, got: %v", err)
	}
	// Must have stopped at the first failing create, not attempted the second.
	if len(fake.createCalls) != 1 {
		t.Errorf("expected exactly 1 CreateContact attempt before failing, got %d", len(fake.createCalls))
	}
}

func TestSeedSandboxContacts_PartiallyPresent_CreatesOnlyMissing(t *testing.T) {
	fake := &fakeContactusService{
		listContacts: []contract4contactus.Contact{
			{ID: "c1", Name: "Sarah Connor"},
		},
	}
	withFakeContactusService(t, fake)

	if err := seedSandboxContacts(context.Background(), "user1", "space1"); err != nil {
		t.Fatalf("seedSandboxContacts: %v", err)
	}
	if len(fake.createCalls) != 1 {
		t.Fatalf("expected exactly 1 CreateContact call for the missing contact, got %d", len(fake.createCalls))
	}
	if fake.createCalls[0].Name != "John Smith" {
		t.Errorf("expected the missing contact John Smith to be created, got %q", fake.createCalls[0].Name)
	}
}

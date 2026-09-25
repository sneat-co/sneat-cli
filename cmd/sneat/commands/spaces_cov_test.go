package commands

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sneat-co/contactus/backend/dto4contactus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// saveFailStore loads fine but fails on Save, exercising spaceUse's second
// error branch (failingStore, in auth_test.go, fails Load too).
type saveFailStore struct{ load session.Session }

func (s *saveFailStore) Load() (session.Session, error) { return s.load, nil }
func (s *saveFailStore) Save(session.Session) error     { return errors.New("save boom") }
func (s *saveFailStore) Clear() error                   { return nil }

func TestSpaceUse_LoadError(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{}) // no session loaded -> Load fails
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "use", "family"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected Store.Load error")
	}
}

func TestSpaceUse_SaveError(t *testing.T) {
	env := testEnv(&saveFailStore{load: session.Session{UID: "u1"}}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "use", "family"})
	err := root.Execute()
	if err == nil || err.Error() != "save boom" {
		t.Fatalf("Execute error = %v, want save boom", err)
	}
}

func TestSpaceCurrent_PrintsCurrentSpace(t *testing.T) {
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1", CurrentSpace: "vaoyj"}}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Space(env))
	var buf bytes.Buffer
	root.SetArgs([]string{"space", "current", "--json"})
	root.SetOut(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("vaoyj")) {
		t.Fatalf("output = %q, want it to contain the current space", buf.String())
	}
}

func TestSpaceCurrent_LoadError(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "current"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected Store.Load error")
	}
}

func TestRunSpaceUI_StoreLoadError(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	env.Store = &fakeStore{} // no session
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected Store.Load error")
	}
	if called {
		t.Fatal("RunTUI must not be called")
	}
}

func TestRunSpaceUI_SpacesReaderError(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	readerErr := errors.New("no spaces reader")
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
	if called {
		t.Fatal("RunTUI must not be called")
	}
}

func TestRunSpaceUI_ContactsReaderError(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	readerErr := errors.New("no contacts reader")
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
	if called {
		t.Fatal("RunTUI must not be called")
	}
}

func TestRunSpaceUI_ContactWriterError(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	writerErr := errors.New("no contact writer")
	env.NewContactWriter = func(config.Config) (ContactWriter, error) { return nil, writerErr }
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); !errors.Is(err, writerErr) {
		t.Fatalf("Execute error = %v, want %v", err, writerErr)
	}
	if called {
		t.Fatal("RunTUI must not be called")
	}
}

func TestRunSpaceUI_ContactWriterSuccess_DeleterWorks(t *testing.T) {
	var gotUID string
	var deleteCalled bool
	w := &fakeContactWriter{}
	env := uiEnv(true, &gotUID, new(bool))
	env.NewContactWriter = func(config.Config) (ContactWriter, error) { return w, nil }
	env.RunTUI = func(_ SpacesReader, _ ContactsReader, deleter ContactDeleter, uid string) error {
		gotUID = uid
		if deleter == nil {
			t.Fatal("deleter must not be nil when NewContactWriter succeeds")
		}
		if err := deleter.DeleteContact(context.Background(), "sp1", "c1"); err != nil {
			t.Fatalf("deleter.DeleteContact: %v", err)
		}
		deleteCalled = true
		return nil
	}
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !deleteCalled || gotUID != "u1" {
		t.Fatalf("deleteCalled=%v uid=%q", deleteCalled, gotUID)
	}
	if w.deleteReq == nil || w.deleteReq.ContactID != "c1" || string(w.deleteReq.SpaceID) != "sp1" {
		t.Fatalf("delete req = %+v", w.deleteReq)
	}
}

func TestContactDeleter_DeleteContact(t *testing.T) {
	w := &fakeContactWriter{}
	d := contactDeleter{w: w}
	if err := d.DeleteContact(context.Background(), "spaceX", "contactY"); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}
	if w.deleteReq == nil {
		t.Fatal("DeleteContact not forwarded to writer")
	}
	if string(w.deleteReq.SpaceID) != "spaceX" || w.deleteReq.ContactID != "contactY" {
		t.Fatalf("delete req = %+v", w.deleteReq)
	}
}

func TestContactDeleter_DeleteContact_Error(t *testing.T) {
	wantErr := errors.New("delete boom")
	w := &errContactWriter{deleteErr: wantErr}
	d := contactDeleter{w: w}
	if err := d.DeleteContact(context.Background(), "sp", "c"); !errors.Is(err, wantErr) {
		t.Fatalf("DeleteContact error = %v, want %v", err, wantErr)
	}
}

// errContactWriter is a ContactWriter whose methods always fail, for
// exercising error branches other fakes (which always succeed) can't reach.
type errContactWriter struct {
	createErr error
	deleteErr error
}

func (e *errContactWriter) CreateContact(context.Context, dto4contactus.CreateContactRequest) (map[string]any, error) {
	return nil, e.createErr
}

func (e *errContactWriter) DeleteContact(context.Context, dto4contactus.ContactRequest) error {
	return e.deleteErr
}

func TestRunSpaceList_SpacesReaderConstructorError(t *testing.T) {
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	readerErr := errors.New("ctor boom")
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "list"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
}

package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/strongo/deviceauth"
)

type fakeCredentialStore struct {
	credential deviceauth.Credential
	loadErr    error
	saveErr    error
	deleteErr  error
}

func (f *fakeCredentialStore) Save(value deviceauth.Credential) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.credential = value
	return nil
}
func (f *fakeCredentialStore) Load() (deviceauth.Credential, error) {
	if f.loadErr != nil {
		return deviceauth.Credential{}, f.loadErr
	}
	return f.credential, nil
}
func (f *fakeCredentialStore) Delete() error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.credential = deviceauth.Credential{}
	return nil
}

func TestSecureStore_SaveLoadUsesCredentialStore(t *testing.T) {
	credentials := &fakeCredentialStore{}
	store := NewSecureStore(credentials, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	want := Session{Project: "sneat-eur3-1", UID: "u1", Email: "a@b.c", IDToken: "id-token", RefreshToken: "refresh-token", ExpiresAt: time.Unix(1000, 0), CurrentSpace: "family"}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if credentials.credential.AccessToken != "id-token" || credentials.credential.RefreshToken != "refresh-token" {
		t.Fatalf("credential = %+v", credentials.credential)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.IDToken != want.IDToken || got.RefreshToken != want.RefreshToken || got.CurrentSpace != want.CurrentSpace {
		t.Fatalf("Load = %+v", got)
	}
}

func TestSecureStore_MigratesOnlyAfterKeyringSave(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := NewStore(legacyPath)
	want := Session{UID: "u1", IDToken: "id-token", RefreshToken: "refresh-token"}
	if err := legacy.Save(want); err != nil {
		t.Fatal(err)
	}
	credentials := &fakeCredentialStore{loadErr: deviceauth.ErrCredentialNotFound, saveErr: errors.New("keyring unavailable")}
	store := NewSecureStore(credentials, legacyPath, filepath.Join(dir, "metadata.json"))
	if _, err := store.Load(); err == nil {
		t.Fatal("Load succeeded despite keyring failure")
	}
	if _, err := legacy.Load(); err != nil {
		t.Fatalf("legacy session was lost: %v", err)
	}
}

func TestSecureStore_ClearRetainsLocalDataWhenKeyringDeleteFails(t *testing.T) {
	credentials := &fakeCredentialStore{deleteErr: errors.New("keyring unavailable")}
	store := NewSecureStore(credentials, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Clear(); err == nil {
		t.Fatal("Clear succeeded despite keyring failure")
	}
}

func TestSecureStore_RollsBackKeyringWhenMetadataWriteFails(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := deviceauth.Credential{AccessToken: "old", RefreshToken: "old-refresh"}
	credentials := &fakeCredentialStore{credential: previous}
	store := NewSecureStore(credentials, filepath.Join(dir, "legacy.json"), filepath.Join(blockedParent, "metadata.json"))
	if err := store.Save(Session{IDToken: "new", RefreshToken: "new-refresh"}); err == nil {
		t.Fatal("Save succeeded despite metadata failure")
	}
	if credentials.credential.AccessToken != previous.AccessToken {
		t.Fatalf("keyring credential was not rolled back: %+v", credentials.credential)
	}
}

func TestInsecureSelectionIsVisibleToSecureStore(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	insecure := NewInsecureStore(legacyPath, metadataPath)
	want := Session{UID: "u1", IDToken: "id-token", RefreshToken: "refresh-token"}
	if err := insecure.Save(want); err != nil {
		t.Fatal(err)
	}
	secure := NewSecureStore(&fakeCredentialStore{}, legacyPath, metadataPath)
	got, err := secure.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.IDToken != want.IDToken {
		t.Fatalf("secure store ignored explicit insecure selection: %+v", got)
	}
}

package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// --- credentialStore ---

func TestCredentialStore_NoLoaderConfigured(t *testing.T) {
	store := NewLazySecureStore(nil, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{}); err == nil || !strings.Contains(err.Error(), "no credential store") {
		t.Fatalf("err = %v", err)
	}
}

func TestCredentialStore_LoaderError(t *testing.T) {
	store := NewLazySecureStore(func() (deviceauth.Store, error) { return nil, errors.New("load boom") },
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{}); err == nil || !strings.Contains(err.Error(), "load boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestCredentialStore_LoaderReturnsNilStore(t *testing.T) {
	store := NewLazySecureStore(func() (deviceauth.Store, error) { return nil, nil },
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{}); err == nil || !strings.Contains(err.Error(), "no credential store") {
		t.Fatalf("err = %v", err)
	}
}

func TestCredentialStore_LoaderSucceedsAndCaches(t *testing.T) {
	calls := 0
	credentials := &fakeCredentialStore{}
	store := NewLazySecureStore(func() (deviceauth.Store, error) {
		calls++
		return credentials, nil
	}, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{IDToken: "a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if calls != 1 {
		t.Fatalf("loader called %d times, want 1 (cached)", calls)
	}
}

// --- SecureStore.Save ---

func TestSecureStore_Save_CredentialStoreError(t *testing.T) {
	store := NewLazySecureStore(func() (deviceauth.Store, error) { return nil, errors.New("boom") },
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecureStore_Save_PreviousLoadOtherError(t *testing.T) {
	credentials := &fakeCredentialStore{loadErr: errors.New("other load error")}
	store := NewSecureStore(credentials, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{}); err == nil || !strings.Contains(err.Error(), "other load error") {
		t.Fatalf("err = %v", err)
	}
}

func TestSecureStore_Save_DeletesOnMetadataFailureWithNoPreviousCredential(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := &fakeCredentialStore{loadErr: deviceauth.ErrCredentialNotFound}
	store := NewSecureStore(credentials, filepath.Join(dir, "legacy.json"), filepath.Join(blockedParent, "metadata.json"))
	if err := store.Save(Session{IDToken: "new"}); err == nil {
		t.Fatal("Save succeeded despite metadata failure")
	}
	if credentials.credential.AccessToken != "" {
		t.Fatalf("keyring credential was not deleted: %+v", credentials.credential)
	}
}

// --- SecureStore.Load ---

func TestSecureStore_Load_MetadataError(t *testing.T) {
	dir := t.TempDir() // a directory, not a file: os.ReadFile fails non-ErrNotExist
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), dir)
	if _, err := store.Load(); err == nil {
		t.Fatal("expected metadata read error")
	}
}

func TestSecureStore_Load_CredentialStoreError(t *testing.T) {
	store := NewLazySecureStore(func() (deviceauth.Store, error) { return nil, errors.New("boom") },
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if _, err := store.Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecureStore_Load_NoCredentialAndNoLegacy(t *testing.T) {
	store := NewSecureStore(&fakeCredentialStore{loadErr: deviceauth.ErrCredentialNotFound},
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if _, err := store.Load(); err == nil {
		t.Fatal("expected error when neither keyring nor legacy has a session")
	}
}

func TestSecureStore_Load_MigratesSuccessfully(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := NewStore(legacyPath)
	want := Session{UID: "u1", IDToken: "id-token", RefreshToken: "refresh-token"}
	if err := legacy.Save(want); err != nil {
		t.Fatal(err)
	}
	credentials := &fakeCredentialStore{loadErr: deviceauth.ErrCredentialNotFound}
	store := NewSecureStore(credentials, legacyPath, filepath.Join(dir, "metadata.json"))
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.IDToken != want.IDToken {
		t.Fatalf("Load = %+v", got)
	}
	if _, err := legacy.Load(); !errors.Is(err, ErrNoSession) {
		t.Fatalf("legacy session was not cleared after migration: %v", err)
	}
}

func TestSecureStore_Load_LegacyClearErrorDuringMigration(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := NewStore(legacyPath)
	if err := legacy.Save(Session{UID: "u1", IDToken: "id-token", RefreshToken: "refresh-token"}); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &removeFileFn, func(path string) error {
		if path == legacyPath {
			return errors.New("remove boom")
		}
		return os.Remove(path)
	})
	credentials := &fakeCredentialStore{loadErr: deviceauth.ErrCredentialNotFound}
	store := NewSecureStore(credentials, legacyPath, filepath.Join(dir, "metadata.json"))
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "remove migrated legacy session") {
		t.Fatalf("err = %v", err)
	}
}

func TestSecureStore_Load_OtherCredentialError(t *testing.T) {
	credentials := &fakeCredentialStore{loadErr: errors.New("other")}
	store := NewSecureStore(credentials, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("err = %v", err)
	}
}

// --- InsecureStore direct ---

func TestInsecureStore_SaveLoadClear(t *testing.T) {
	dir := t.TempDir()
	insecure := NewInsecureStore(filepath.Join(dir, "legacy.json"), filepath.Join(dir, "metadata.json"))
	want := Session{UID: "u1", IDToken: "id-token"}
	if err := insecure.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := insecure.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.IDToken != want.IDToken {
		t.Fatalf("Load = %+v", got)
	}
	if err := insecure.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := insecure.Load(); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load after Clear = %v", err)
	}
}

func TestInsecureStore_Clear_AbsentIsNoError(t *testing.T) {
	dir := t.TempDir()
	insecure := NewInsecureStore(filepath.Join(dir, "legacy.json"), filepath.Join(dir, "metadata.json"))
	if err := insecure.Clear(); err != nil {
		t.Fatalf("Clear on absent = %v, want nil", err)
	}
}

func TestInsecureStore_Save_LegacySaveError(t *testing.T) {
	withSeam(t, &mkdirAllFn, func(string, os.FileMode) error { return errors.New("mkdir boom") })
	insecure := NewInsecureStore(filepath.Join(t.TempDir(), "sub", "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := insecure.Save(Session{}); err == nil {
		t.Fatal("expected legacy save error")
	}
}

func TestInsecureStore_Save_MarshalError(t *testing.T) {
	withSeam(t, &jsonMarshalFn, func(any) ([]byte, error) { return nil, errors.New("marshal boom") })
	dir := t.TempDir()
	insecure := NewInsecureStore(filepath.Join(dir, "legacy.json"), filepath.Join(dir, "metadata.json"))
	if err := insecure.Save(Session{}); err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestInsecureStore_Clear_LegacyClearError(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	insecure := NewInsecureStore(legacyPath, metadataPath)
	if err := insecure.Save(Session{UID: "u1"}); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &removeFileFn, func(path string) error {
		if path == legacyPath {
			return errors.New("remove boom")
		}
		return os.Remove(path)
	})
	if err := insecure.Clear(); err == nil {
		t.Fatal("expected legacy clear error")
	}
}

func TestInsecureStore_Clear_MetadataRemoveError(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	insecure := NewInsecureStore(legacyPath, metadataPath)
	if err := insecure.Save(Session{UID: "u1"}); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &removeFileFn, func(path string) error {
		if path == metadataPath {
			return errors.New("remove boom")
		}
		return os.Remove(path)
	})
	if err := insecure.Clear(); err == nil || !strings.Contains(err.Error(), "remove boom") {
		t.Fatalf("err = %v", err)
	}
}

// --- SecureStore.Clear ---

func TestSecureStore_Clear_MetadataError(t *testing.T) {
	dir := t.TempDir()
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), dir)
	if err := store.Clear(); err == nil {
		t.Fatal("expected metadata read error")
	}
}

func TestSecureStore_Clear_CredentialStoreError(t *testing.T) {
	store := NewLazySecureStore(func() (deviceauth.Store, error) { return nil, errors.New("boom") },
		filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Clear(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecureStore_Clear_Success(t *testing.T) {
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
}

func TestSecureStore_ClearPlaintextSession_LegacyClearError(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	if err := NewStore(legacyPath).Save(Session{UID: "u1"}); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &removeFileFn, func(path string) error {
		if path == legacyPath {
			return errors.New("remove boom")
		}
		return os.Remove(path)
	})
	store := NewSecureStore(&fakeCredentialStore{}, legacyPath, metadataPath)
	if err := store.Clear(); err == nil {
		t.Fatal("expected legacy clear error")
	}
}

func TestSecureStore_ClearPlaintextSession_MetadataRemoveError(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	if err := NewStore(metadataPath).Save(Session{UID: "meta"}); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &removeFileFn, func(path string) error {
		if path == metadataPath {
			return errors.New("remove boom")
		}
		return os.Remove(path)
	})
	store := NewSecureStore(&fakeCredentialStore{}, legacyPath, metadataPath)
	if err := store.Clear(); err == nil || !strings.Contains(err.Error(), "remove boom") {
		t.Fatalf("err = %v", err)
	}
}

// --- DeviceAuthStore ---

type erroringSessionStore struct {
	loadErr error
}

func (e *erroringSessionStore) Load() (Session, error) { return Session{}, e.loadErr }
func (e *erroringSessionStore) Save(Session) error     { return nil }
func (e *erroringSessionStore) Clear() error           { return nil }

func TestNewDeviceAuthStore(t *testing.T) {
	inner := &memoryStoreForDeviceAuth{}
	ds := NewDeviceAuthStore(inner, "proj-1")
	if ds == nil {
		t.Fatal("NewDeviceAuthStore returned nil")
	}
}

type memoryStoreForDeviceAuth struct {
	value Session
	has   bool
}

func (m *memoryStoreForDeviceAuth) Load() (Session, error) {
	if !m.has {
		return Session{}, ErrNoSession
	}
	return m.value, nil
}
func (m *memoryStoreForDeviceAuth) Save(s Session) error { m.value, m.has = s, true; return nil }
func (m *memoryStoreForDeviceAuth) Clear() error         { m.value, m.has = Session{}, false; return nil }

func TestDeviceAuthStore_Save_NilReceiverOrStore(t *testing.T) {
	var nilStore *DeviceAuthStore
	if err := nilStore.Save(deviceauth.Credential{}); err == nil {
		t.Fatal("expected error for nil receiver")
	}
	if err := (&DeviceAuthStore{}).Save(deviceauth.Credential{}); err == nil {
		t.Fatal("expected error for nil inner store")
	}
}

func TestDeviceAuthStore_Save_LoadErrorPropagates(t *testing.T) {
	ds := NewDeviceAuthStore(&erroringSessionStore{loadErr: errors.New("load boom")}, "proj")
	if err := ds.Save(deviceauth.Credential{}); err == nil || !strings.Contains(err.Error(), "load boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceAuthStore_Save_Success(t *testing.T) {
	inner := &memoryStoreForDeviceAuth{}
	ds := NewDeviceAuthStore(inner, "proj-1")
	cred := deviceauth.Credential{
		AccessToken: "id", RefreshToken: "refresh", AccountID: "u1", AccountName: "a@b.c",
		Issuer: "https://auth.sneat.co", ClientID: "sneat-cli", Scopes: []string{"openid"}, TokenType: "Bearer",
	}
	if err := ds.Save(cred); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if inner.value.Project != "proj-1" || inner.value.IDToken != "id" || inner.value.Issuer != "https://auth.sneat.co" {
		t.Fatalf("stored session = %+v", inner.value)
	}
}

func TestDeviceAuthStore_Save_PreservesCurrentSpace(t *testing.T) {
	inner := &memoryStoreForDeviceAuth{value: Session{CurrentSpace: "family"}, has: true}
	ds := NewDeviceAuthStore(inner, "proj-1")
	if err := ds.Save(deviceauth.Credential{AccountID: "u1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if inner.value.CurrentSpace != "family" {
		t.Fatalf("CurrentSpace = %q, want preserved", inner.value.CurrentSpace)
	}
}

func TestDeviceAuthStore_Load_NilReceiverOrStore(t *testing.T) {
	var nilStore *DeviceAuthStore
	if _, err := nilStore.Load(); err == nil {
		t.Fatal("expected error for nil receiver")
	}
	if _, err := (&DeviceAuthStore{}).Load(); err == nil {
		t.Fatal("expected error for nil inner store")
	}
}

func TestDeviceAuthStore_Load_NoSessionBecomesCredentialNotFound(t *testing.T) {
	ds := NewDeviceAuthStore(&memoryStoreForDeviceAuth{}, "proj")
	if _, err := ds.Load(); !errors.Is(err, deviceauth.ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound", err)
	}
}

func TestDeviceAuthStore_Load_OtherErrorPropagates(t *testing.T) {
	ds := NewDeviceAuthStore(&erroringSessionStore{loadErr: errors.New("load boom")}, "proj")
	if _, err := ds.Load(); err == nil || !strings.Contains(err.Error(), "load boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceAuthStore_Load_PasswordMigrationHasNoBinding(t *testing.T) {
	inner := &memoryStoreForDeviceAuth{value: Session{IDToken: "id"}, has: true}
	ds := NewDeviceAuthStore(inner, "proj")
	if _, err := ds.Load(); !errors.Is(err, deviceauth.ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound for unbound session", err)
	}
}

func TestDeviceAuthStore_Load_Success(t *testing.T) {
	inner := &memoryStoreForDeviceAuth{value: Session{
		IDToken: "id", RefreshToken: "refresh", UID: "u1", Email: "a@b.c",
		Issuer: "https://auth.sneat.co", ClientID: "sneat-cli", Scopes: []string{"openid"}, TokenType: "Bearer",
	}, has: true}
	ds := NewDeviceAuthStore(inner, "proj")
	cred, err := ds.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cred.AccessToken != "id" || cred.Issuer != "https://auth.sneat.co" || len(cred.Scopes) != 1 {
		t.Fatalf("cred = %+v", cred)
	}
}

func TestDeviceAuthStore_Delete(t *testing.T) {
	var nilStore *DeviceAuthStore
	if err := nilStore.Delete(); err == nil {
		t.Fatal("expected error for nil receiver")
	}
	if err := (&DeviceAuthStore{}).Delete(); err == nil {
		t.Fatal("expected error for nil inner store")
	}
	inner := &memoryStoreForDeviceAuth{value: Session{UID: "u1"}, has: true}
	ds := NewDeviceAuthStore(inner, "proj")
	if err := ds.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if inner.has {
		t.Fatal("session was not cleared")
	}
}

// --- saveMetadata ---

func TestSaveMetadata_EmptyPath(t *testing.T) {
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), "")
	if err := store.Save(Session{IDToken: "x"}); err == nil || !strings.Contains(err.Error(), "metadata path is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveMetadata_MarshalError(t *testing.T) {
	withSeam(t, &jsonMarshalFn, func(any) ([]byte, error) { return nil, errors.New("marshal boom") })
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), filepath.Join(t.TempDir(), "metadata.json"))
	if err := store.Save(Session{IDToken: "x"}); err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Fatalf("err = %v", err)
	}
}

// --- loadMetadata ---

func TestLoadMetadata_EmptyPath(t *testing.T) {
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), "")
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "metadata path is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadMetadata_ReadError(t *testing.T) {
	dir := t.TempDir()
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), dir)
	if _, err := store.Load(); err == nil {
		t.Fatal("expected read error for a directory path")
	}
}

func TestLoadMetadata_UnmarshalError(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(metadataPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewSecureStore(&fakeCredentialStore{}, filepath.Join(t.TempDir(), "legacy.json"), metadataPath)
	if _, err := store.Load(); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSecureStore_ClearHonorsPersistedInsecureSelectionWithoutKeyring(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.json")
	metadataPath := filepath.Join(dir, "metadata.json")
	insecure := NewInsecureStore(legacyPath, metadataPath)
	if err := insecure.Save(Session{UID: "u1", IDToken: "id-token", RefreshToken: "refresh-token"}); err != nil {
		t.Fatal(err)
	}
	keyringCalled := false
	secure := NewLazySecureStore(func() (deviceauth.Store, error) {
		keyringCalled = true
		return nil, errors.New("keyring unavailable")
	}, legacyPath, metadataPath)
	if err := secure.Clear(); err != nil {
		t.Fatal(err)
	}
	if keyringCalled {
		t.Fatal("Clear initialized keyring despite persisted insecure selection")
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy plaintext retained: %v", err)
	}
	if _, err := os.Stat(metadataPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("insecure metadata retained: %v", err)
	}
}

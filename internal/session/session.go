package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/strongo/deviceauth"
)

// ErrNoSession is returned by Load when no session file exists.
var ErrNoSession = errors.New("no session; run 'sneat auth login'")

// Session is the persisted authentication state.
type Session struct {
	Project      string    `json:"project"`
	UID          string    `json:"uid"`
	Email        string    `json:"email"`
	IDToken      string    `json:"idToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	// CurrentSpace is the default space for commands when --space is omitted.
	// It may be a real space id or a pseudo id ("family" / "private").
	CurrentSpace string `json:"currentSpace,omitempty"`
	// The following fields bind a device-auth session to its issuer/client.
	// They are not secrets; the ID and refresh tokens remain the sensitive data.
	Issuer    string   `json:"issuer,omitempty"`
	ClientID  string   `json:"clientId,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	TokenType string   `json:"tokenType,omitempty"`
}

// SessionStore is the common storage contract used by commands and deviceauth.
type SessionStore interface {
	Save(Session) error
	Load() (Session, error)
	Clear() error
}

// Store reads/writes a Session as a 0600 JSON file.
type Store struct{ path string }

// NewStore returns a Store backed by the given file path.
func NewStore(path string) *Store { return &Store{path: path} }

// DefaultPath returns <userConfigDir>/sneat/session.json.
func DefaultPath(userConfigDir func() (string, error)) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sneat", "session.json"), nil
}

// DefaultMetadataPath returns the non-secret metadata companion used by the
// keyring-backed store. It deliberately never contains a bearer or refresh
// token.
func DefaultMetadataPath(userConfigDir func() (string, error)) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sneat", "session-metadata.json"), nil
}

// Save writes the session as 0600 JSON, creating the parent dir (0700).
func (s *Store) Save(sess Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.path, data, 0o600)
}

// Load reads the session, returning ErrNoSession if the file is absent.
func (s *Store) Load() (Session, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Clear removes the session file; absence is not an error.
func (s *Store) Clear() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// statFile is a test seam over os.Stat.
var statFile = os.Stat

// SecureStore keeps Firebase tokens in an operating-system keyring via
// deviceauth. The legacy file is consulted only to migrate an existing local
// session; it is not removed until a later explicit logout.
type SecureStore struct {
	credentials     deviceauth.Store
	loadCredentials func() (deviceauth.Store, error)
	credentialsMu   sync.Mutex
	legacy          *Store
	metadataPath    string
}

// NewSecureStore constructs a keyring-backed SessionStore. credentials must
// use an issuer-and-client-specific keyring account supplied by the caller.
func NewSecureStore(credentials deviceauth.Store, legacyPath, metadataPath string) *SecureStore {
	return &SecureStore{
		credentials:  credentials,
		legacy:       NewStore(legacyPath),
		metadataPath: metadataPath,
	}
}

// NewLazySecureStore delays keyring initialization. This lets an explicitly
// requested insecure store work on headless machines where no keyring exists.
func NewLazySecureStore(load func() (deviceauth.Store, error), legacyPath, metadataPath string) *SecureStore {
	return &SecureStore{loadCredentials: load, legacy: NewStore(legacyPath), metadataPath: metadataPath}
}

func (s *SecureStore) credentialStore() (deviceauth.Store, error) {
	s.credentialsMu.Lock()
	defer s.credentialsMu.Unlock()
	if s.credentials != nil {
		return s.credentials, nil
	}
	if s.loadCredentials == nil {
		return nil, errors.New("secure session store has no credential store")
	}
	credentials, err := s.loadCredentials()
	if err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("secure session store has no credential store")
	}
	s.credentials = credentials
	return credentials, nil
}

type metadata struct {
	Project      string `json:"project"`
	CurrentSpace string `json:"currentSpace,omitempty"`
	Insecure     bool   `json:"insecureStorage,omitempty"`
}

// Save writes tokens to the keyring before updating non-secret metadata.
func (s *SecureStore) Save(sess Session) error {
	credentials, err := s.credentialStore()
	if err != nil {
		return err
	}
	previous, previousErr := credentials.Load()
	if previousErr != nil && !errors.Is(previousErr, deviceauth.ErrCredentialNotFound) {
		return previousErr
	}
	credential := deviceauth.Credential{
		AccessToken:  sess.IDToken,
		RefreshToken: sess.RefreshToken,
		Expiry:       sess.ExpiresAt,
		AccountID:    sess.UID,
		AccountName:  sess.Email,
		Issuer:       sess.Issuer,
		ClientID:     sess.ClientID,
		Scopes:       append([]string(nil), sess.Scopes...),
		TokenType:    sess.TokenType,
	}
	if err := credentials.Save(credential); err != nil {
		return err
	}
	if err := s.saveMetadata(metadata{Project: sess.Project, CurrentSpace: sess.CurrentSpace}); err != nil {
		if previousErr == nil {
			_ = credentials.Save(previous)
		} else {
			_ = credentials.Delete()
		}
		return err
	}
	return nil
}

// Load returns the keyring session, migrating a legacy 0600 session only after
// the keyring save succeeds. The legacy file remains as a recovery copy until
// a successful logout removes it.
func (s *SecureStore) Load() (Session, error) {
	meta, err := s.loadMetadata()
	if err != nil {
		return Session{}, err
	}
	if meta.Insecure {
		return s.legacy.Load()
	}
	credentials, err := s.credentialStore()
	if err != nil {
		return Session{}, err
	}
	credential, err := credentials.Load()
	if errors.Is(err, deviceauth.ErrCredentialNotFound) {
		legacy, legacyErr := s.legacy.Load()
		if legacyErr != nil {
			return Session{}, legacyErr
		}
		if err := s.Save(legacy); err != nil {
			return Session{}, fmt.Errorf("migrate legacy session to keyring: %w", err)
		}
		if err := s.legacy.Clear(); err != nil {
			return Session{}, fmt.Errorf("remove migrated legacy session: %w", err)
		}
		return legacy, nil
	}
	if err != nil {
		return Session{}, err
	}
	return Session{
		Project:      meta.Project,
		UID:          credential.AccountID,
		Email:        credential.AccountName,
		IDToken:      credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		ExpiresAt:    credential.Expiry,
		CurrentSpace: meta.CurrentSpace,
		Issuer:       credential.Issuer,
		ClientID:     credential.ClientID,
		Scopes:       append([]string(nil), credential.Scopes...),
		TokenType:    credential.TokenType,
	}, nil
}

// InsecureStore is an explicit headless fallback. It records the selection in
// non-secret metadata so subsequent token sources consistently use the same
// 0600 store until a normal secure login replaces it.
type InsecureStore struct {
	legacy       *Store
	metadataPath string
}

func NewInsecureStore(legacyPath, metadataPath string) *InsecureStore {
	return &InsecureStore{legacy: NewStore(legacyPath), metadataPath: metadataPath}
}

func (s *InsecureStore) Save(value Session) error {
	if err := s.legacy.Save(value); err != nil {
		return err
	}
	data, err := json.Marshal(metadata{Project: value.Project, CurrentSpace: value.CurrentSpace, Insecure: true})
	if err != nil {
		return err
	}
	return atomicWriteFile(s.metadataPath, data, 0o600)
}

func (s *InsecureStore) Load() (Session, error) { return s.legacy.Load() }

func (s *InsecureStore) Clear() error {
	if err := s.legacy.Clear(); err != nil {
		return err
	}
	err := os.Remove(s.metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Clear removes keyring, migration copy, and non-secret metadata. Local data
// remains intact if the keyring delete itself fails.
func (s *SecureStore) Clear() error {
	meta, err := s.loadMetadata()
	if err != nil {
		return err
	}
	if meta.Insecure {
		return s.clearPlaintextSession()
	}
	credentials, err := s.credentialStore()
	if err != nil {
		return err
	}
	if err := credentials.Delete(); err != nil {
		return err
	}
	return s.clearPlaintextSession()
}

func (s *SecureStore) clearPlaintextSession() error {
	if err := s.legacy.Clear(); err != nil {
		return err
	}
	err := os.Remove(s.metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeviceAuthStore adapts the normal session location into deviceauth's
// credential contract. A device login therefore persists its Firebase session
// once, through the same secure or explicit insecure store used by every CLI
// command.
type DeviceAuthStore struct {
	store   SessionStore
	project string
}

func NewDeviceAuthStore(store SessionStore, project string) *DeviceAuthStore {
	return &DeviceAuthStore{store: store, project: project}
}

func (s *DeviceAuthStore) Save(credential deviceauth.Credential) error {
	if s == nil || s.store == nil {
		return errors.New("device auth session store is required")
	}
	current, err := s.store.Load()
	if err != nil && !errors.Is(err, ErrNoSession) {
		return err
	}
	return s.store.Save(Session{
		Project:      s.project,
		UID:          credential.AccountID,
		Email:        credential.AccountName,
		IDToken:      credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		ExpiresAt:    credential.Expiry,
		CurrentSpace: current.CurrentSpace,
		Issuer:       credential.Issuer,
		ClientID:     credential.ClientID,
		Scopes:       append([]string(nil), credential.Scopes...),
		TokenType:    credential.TokenType,
	})
}

func (s *DeviceAuthStore) Load() (deviceauth.Credential, error) {
	if s == nil || s.store == nil {
		return deviceauth.Credential{}, errors.New("device auth session store is required")
	}
	value, err := s.store.Load()
	if errors.Is(err, ErrNoSession) {
		return deviceauth.Credential{}, deviceauth.ErrCredentialNotFound
	}
	if err != nil {
		return deviceauth.Credential{}, err
	}
	// A password/login migration has no OAuth issuer binding. Treat it as no
	// device credential so it cannot block an interactive replacement.
	if value.Issuer == "" || value.ClientID == "" {
		return deviceauth.Credential{}, deviceauth.ErrCredentialNotFound
	}
	return deviceauth.Credential{
		AccessToken: value.IDToken, RefreshToken: value.RefreshToken, Expiry: value.ExpiresAt,
		AccountID: value.UID, AccountName: value.Email, Issuer: value.Issuer,
		ClientID: value.ClientID, Scopes: append([]string(nil), value.Scopes...), TokenType: value.TokenType,
	}, nil
}

func (s *DeviceAuthStore) Delete() error {
	if s == nil || s.store == nil {
		return errors.New("device auth session store is required")
	}
	return s.store.Clear()
}

func (s *SecureStore) saveMetadata(meta metadata) error {
	if s.metadataPath == "" {
		return errors.New("secure session metadata path is required")
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return atomicWriteFile(s.metadataPath, data, 0o600)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(parent, ".session-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s *SecureStore) loadMetadata() (metadata, error) {
	if s.metadataPath == "" {
		return metadata{}, errors.New("secure session metadata path is required")
	}
	data, err := os.ReadFile(s.metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return metadata{}, nil
	}
	if err != nil {
		return metadata{}, err
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

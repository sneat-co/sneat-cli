package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
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
	credentials  deviceauth.Store
	legacy       *Store
	metadataPath string
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

type metadata struct {
	Project      string `json:"project"`
	CurrentSpace string `json:"currentSpace,omitempty"`
}

// Save writes tokens to the keyring before updating non-secret metadata.
func (s *SecureStore) Save(sess Session) error {
	if s.credentials == nil {
		return errors.New("secure session store has no credential store")
	}
	if err := s.credentials.Save(deviceauth.Credential{
		AccessToken:  sess.IDToken,
		RefreshToken: sess.RefreshToken,
		Expiry:       sess.ExpiresAt,
		AccountID:    sess.UID,
		AccountName:  sess.Email,
	}); err != nil {
		return err
	}
	return s.saveMetadata(metadata{Project: sess.Project, CurrentSpace: sess.CurrentSpace})
}

// Load returns the keyring session, migrating a legacy 0600 session only after
// the keyring save succeeds. The legacy file remains as a recovery copy until
// a successful logout removes it.
func (s *SecureStore) Load() (Session, error) {
	if s.credentials == nil {
		return Session{}, errors.New("secure session store has no credential store")
	}
	credential, err := s.credentials.Load()
	if errors.Is(err, deviceauth.ErrCredentialNotFound) {
		legacy, legacyErr := s.legacy.Load()
		if legacyErr != nil {
			return Session{}, legacyErr
		}
		if err := s.Save(legacy); err != nil {
			return Session{}, fmt.Errorf("migrate legacy session to keyring: %w", err)
		}
		return legacy, nil
	}
	if err != nil {
		return Session{}, err
	}
	meta, err := s.loadMetadata()
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
	}, nil
}

// Clear removes keyring, migration copy, and non-secret metadata. Local data
// remains intact if the keyring delete itself fails.
func (s *SecureStore) Clear() error {
	if s.credentials == nil {
		return errors.New("secure session store has no credential store")
	}
	if err := s.credentials.Delete(); err != nil {
		return err
	}
	if err := s.legacy.Clear(); err != nil {
		return err
	}
	err := os.Remove(s.metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *SecureStore) saveMetadata(meta metadata) error {
	if s.metadataPath == "" {
		return errors.New("secure session metadata path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.metadataPath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(s.metadataPath, data, 0o600)
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

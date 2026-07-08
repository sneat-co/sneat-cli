package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
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

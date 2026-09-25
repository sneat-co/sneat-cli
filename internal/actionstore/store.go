// Package actionstore persists client-held `sneat action` drafts as JSON
// files, one per action id, so a draft survives between CLI invocations
// without needing a server-held record.
package actionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dbo4sneatai"
	"github.com/sneat-co/sneat-ai-backend/semlint"
)

// ErrNotFound is returned when a draft with the given id does not exist.
var ErrNotFound = errors.New("action draft not found")

// jsonMarshalIndent is a seam over json.MarshalIndent so tests can force a
// marshal failure without needing a genuinely unmarshalable Draft value.
var jsonMarshalIndent = json.MarshalIndent

// Draft is the on-disk shape of a client-held action.
type Draft struct {
	ActionID   string              `json:"actionID"`
	SpaceID    string              `json:"spaceID"`
	Language   string              `json:"language,omitempty"`
	Utterance  string              `json:"utterance,omitempty"`
	Semantic   actionspec.Semantic `json:"semantic"`
	Validation semlint.Validation  `json:"validation"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	// Committed is set once `sneat action commit` succeeds.
	Committed *dbo4sneatai.CommitResult `json:"committed,omitempty"`
	// Cancelled marks a draft that was cancelled (kept for `action list` until deleted).
	Cancelled bool `json:"cancelled,omitempty"`
	// ServerHeld marks an id created with `sneat action new --server`: the
	// authoritative record lives on the server, and this local entry is only
	// an index entry (no semantic/validation kept) so `action show`/`add`/…
	// know to route to the server without an extra round trip to find out.
	ServerHeld bool `json:"serverHeld,omitempty"`
}

// Store reads/writes drafts under a base directory (one file per action id).
type Store struct {
	dir string
}

// NewStore builds a Store rooted at dir. dir is created on first Save.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// DefaultDir returns <userConfigDir>/sneat/actions, mirroring
// session.DefaultPath's pattern for locating the CLI's config home.
func DefaultDir(userConfigDir func() (string, error)) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sneat", "actions"), nil
}

func (s *Store) path(actionID string) string {
	return filepath.Join(s.dir, actionID+".json")
}

// Save writes the draft as indented JSON, creating the store directory if needed.
func (s *Store) Save(d Draft) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := jsonMarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(d.ActionID), data, 0o600)
}

// Load reads one draft by id. It returns ErrNotFound when the file is absent.
func (s *Store) Load(actionID string) (Draft, error) {
	data, err := os.ReadFile(s.path(actionID))
	if err != nil {
		if os.IsNotExist(err) {
			return Draft{}, ErrNotFound
		}
		return Draft{}, err
	}
	var d Draft
	if err := json.Unmarshal(data, &d); err != nil {
		return Draft{}, err
	}
	return d, nil
}

// Delete removes a draft's file. Deleting a missing draft is not an error.
func (s *Store) Delete(actionID string) error {
	err := os.Remove(s.path(actionID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns every stored draft, sorted by ActionID. A missing store
// directory is not an error: it means there are no drafts yet.
func (s *Store) List() ([]Draft, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Draft
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		d, err := s.Load(id)
		if err != nil {
			continue // skip unreadable/corrupt files rather than fail the whole listing
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ActionID < out[j].ActionID })
	return out, nil
}

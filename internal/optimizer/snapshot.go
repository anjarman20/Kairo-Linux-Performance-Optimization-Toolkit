package optimizer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Transaction states.
const (
	StateSnapshot   = "snapshot"
	StateCommitted  = "committed"
	StateRolledBack = "rolled_back"
	StateFailedRoll = "failed_rolled_back"
)

// Metadata is the reversible record of one optimization transaction under
// <base>/<id>/metadata.json. It stores previous and new values for every
// change so rollback never needs to guess.
type Metadata struct {
	ID           string   `json:"id"`
	Timestamp    string   `json:"timestamp"`
	KairoVersion string   `json:"kairo_version"`
	Kernel       string   `json:"kernel"`
	Hostname     string   `json:"hostname"`
	Profile      string   `json:"profile"`
	State        string   `json:"state"`
	Changes      []Change `json:"changes"`
}

// DefaultBase is where transactions live on a real system.
const DefaultBase = "/var/lib/kairo/backups"

// NewID produces a UTC-derived, sortable transaction id.
func NewID(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// SaveSnapshot persists the transaction metadata before any write happens.
// Directories and files are created with owner-only permissions.
func SaveSnapshot(base string, m Metadata) error {
	dir := filepath.Join(base, m.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600)
}

// LoadSnapshot reads a transaction's metadata.
func LoadSnapshot(base, id string) (Metadata, error) {
	var m Metadata
	data, err := os.ReadFile(filepath.Join(base, id, "metadata.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

// SetState rewrites a transaction's state after apply/rollback completes.
func SetState(base, id, state string) error {
	m, err := LoadSnapshot(base, id)
	if err != nil {
		return err
	}
	m.State = state
	return SaveSnapshot(base, m)
}

// Latest returns the most recent transaction id (empty when none exist).
func Latest(base string) (string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == 16 { // 20060102T150405Z
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// CommitTransaction marks an applied transaction as committed.
func CommitTransaction(base, id string) error {
	return SetState(base, id, StateCommitted)
}

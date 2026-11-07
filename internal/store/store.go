// Package store persists the active countdown between application runs.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is the version of the on-disk state format.
const SchemaVersion = 1

// Status values persisted with the countdown.
const (
	StatusRunning = "running"
	StatusPaused  = "paused"
)

// State is the persisted countdown.
type State struct {
	Version       int       `json:"version"`
	Status        string    `json:"status,omitempty"`
	PresetMinutes int       `json:"presetMinutes"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	Deadline      time.Time `json:"deadline,omitempty"`
	RemainingSec  int       `json:"remainingSec,omitempty"`
}

// Store reads and writes a single state file.
type Store struct {
	path string
}

// DefaultPath returns $XDG_STATE_HOME/light-wails-timer/state.json, falling back
// to ~/.local/state/light-wails-timer/state.json.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "light-wails-timer", "state.json"), nil
}

// New returns a Store at the default XDG location.
func New() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// NewAt returns a Store for an explicit path, used by tests.
func NewAt(path string) *Store { return &Store{path: path} }

// Path reports the file backing the store.
func (s *Store) Path() string { return s.path }

// Load returns the persisted state. A missing file yields (nil, nil); a file
// that cannot be parsed or has an unknown version yields an error.
func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read timer state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse timer state: %w", err)
	}
	if state.Version != SchemaVersion {
		return nil, fmt.Errorf("unsupported timer state version %d", state.Version)
	}
	if state.PresetMinutes <= 0 {
		return nil, errors.New("incomplete timer state")
	}

	// Legacy files omit status and always represent a running countdown.
	if state.Status == "" {
		state.Status = StatusRunning
	}

	switch state.Status {
	case StatusRunning:
		if state.Deadline.IsZero() {
			return nil, errors.New("incomplete timer state")
		}
	case StatusPaused:
		if state.RemainingSec <= 0 {
			return nil, errors.New("incomplete timer state")
		}
	default:
		return nil, fmt.Errorf("unsupported timer status %q", state.Status)
	}
	return &state, nil
}

// Save writes the state atomically with 0600 permissions.
func (s *Store) Save(state State) error {
	state.Version = SchemaVersion
	if state.Status == "" {
		state.Status = StatusRunning
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode timer state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("set state permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

// Clear removes the persisted state; a missing file is not an error.
func (s *Store) Clear() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove timer state: %w", err)
	}
	return nil
}

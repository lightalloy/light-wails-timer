package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPathUsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/xdg-state", "light-wails-timer", "state.json")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewAt(path)

	want := State{
		Status:        StatusRunning,
		PresetMinutes: 30,
		StartedAt:     time.Now().Add(-time.Minute).Truncate(time.Second),
		Deadline:      time.Now().Add(29 * time.Minute).Truncate(time.Second),
	}
	if err := st.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil state")
	}
	if got.PresetMinutes != want.PresetMinutes {
		t.Fatalf("preset = %d, want %d", got.PresetMinutes, want.PresetMinutes)
	}
	if got.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", got.Status, StatusRunning)
	}
	if !got.Deadline.Equal(want.Deadline) {
		t.Fatalf("deadline = %v, want %v", got.Deadline, want.Deadline)
	}
	if got.Version != SchemaVersion {
		t.Fatalf("version = %d, want %d", got.Version, SchemaVersion)
	}
}

func TestSaveAndLoadPausedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewAt(path)

	want := State{
		Status:        StatusPaused,
		PresetMinutes: 15,
		RemainingSec:  321,
	}
	if err := st.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil state")
	}
	if got.Status != StatusPaused || got.RemainingSec != 321 || got.PresetMinutes != 15 {
		t.Fatalf("unexpected paused state: %+v", got)
	}
}

func TestLoadLegacyRunningStateWithoutStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	deadline := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	data := `{"version":1,"presetMinutes":30,"deadline":"` + deadline + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := NewAt(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Status != StatusRunning {
		t.Fatalf("legacy state should default to running, got %+v", got)
	}
}

func TestSavedFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewAt(path)

	if err := st.Save(State{PresetMinutes: 15, Deadline: time.Now().Add(time.Minute)}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	st := NewAt(filepath.Join(t.TempDir(), "absent.json"))

	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil state, got %+v", got)
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := NewAt(path).Load(); err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := `{"version":99,"presetMinutes":30,"deadline":"2026-10-31T15:00:00Z"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := NewAt(path).Load(); err == nil {
		t.Fatal("expected an error for an unknown schema version")
	}
}

func TestLoadRejectsIncompleteState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := NewAt(path).Load()
	if err == nil {
		t.Fatal("expected an error for incomplete state")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClearRemovesFileAndToleratesMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewAt(path)
	if err := st.Save(State{PresetMinutes: 60, Deadline: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := st.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state file still present: %v", err)
	}
	if err := st.Clear(); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

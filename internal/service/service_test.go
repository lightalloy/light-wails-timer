package service

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"light-wails-timer/internal/store"
	"light-wails-timer/internal/timer"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

type fakeNotifier struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (f *fakeNotifier) Send(title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, title+": "+body)
	return nil
}

func (f *fakeNotifier) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

type fakeAutostart struct {
	enabled    bool
	enableErr  error
	disableErr error
}

func (f *fakeAutostart) Enable() error {
	if f.enableErr != nil {
		return f.enableErr
	}
	f.enabled = true
	return nil
}

func (f *fakeAutostart) Disable() error {
	if f.disableErr != nil {
		return f.disableErr
	}
	f.enabled = false
	return nil
}

func (f *fakeAutostart) IsEnabled() (bool, error) { return f.enabled, nil }

type harness struct {
	clock     *fakeClock
	store     *store.Store
	notifier  *fakeNotifier
	autostart *fakeAutostart
	svc       *Service
	published []State
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		clock:     &fakeClock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)},
		store:     store.NewAt(filepath.Join(t.TempDir(), "state.json")),
		notifier:  &fakeNotifier{},
		autostart: &fakeAutostart{},
	}
	h.svc = New(h.clock, h.store, h.notifier, h.autostart, func(s State) {
		h.published = append(h.published, s)
	}, nil)
	return h
}

func TestStartPersistsAndPublishes(t *testing.T) {
	h := newHarness(t)

	if err := h.svc.Start(timer.PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state := h.svc.GetState()
	if state.Status != "running" || state.RemainingSec != 1800 || state.PresetMinutes != 30 {
		t.Fatalf("unexpected state: %+v", state)
	}

	persisted, err := h.store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted == nil {
		t.Fatal("state was not persisted")
	}
	if persisted.PresetMinutes != 30 {
		t.Fatalf("persisted preset = %d, want 30", persisted.PresetMinutes)
	}
}

func TestReplaceDoesNotNotify(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(timer.PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.clock.Advance(5 * time.Minute)

	if err := h.svc.Start(timer.PresetShort); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	if got := h.svc.GetState().RemainingSec; got != 900 {
		t.Fatalf("remaining = %d, want 900", got)
	}
	if len(h.notifier.messages()) != 0 {
		t.Fatalf("replacement sent notifications: %v", h.notifier.messages())
	}
}

func TestStopClearsState(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(timer.PresetLong); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := h.svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if state := h.svc.GetState(); state.Status != "idle" {
		t.Fatalf("status = %q, want idle", state.Status)
	}
	persisted, err := h.store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted != nil {
		t.Fatalf("state still persisted: %+v", persisted)
	}
}

func TestPausePersistsAndResumeContinues(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(timer.PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.clock.Advance(12 * time.Minute)

	if err := h.svc.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	state := h.svc.GetState()
	if state.Status != "paused" || state.RemainingSec != 1080 {
		t.Fatalf("unexpected paused state: %+v", state)
	}

	persisted, err := h.store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted == nil || persisted.Status != store.StatusPaused || persisted.RemainingSec != 1080 {
		t.Fatalf("unexpected persisted paused state: %+v", persisted)
	}

	h.clock.Advance(3 * time.Minute)
	h.svc.Tick()
	if got := h.svc.GetState().RemainingSec; got != 1080 {
		t.Fatalf("remaining drifted while paused: %d", got)
	}

	if err := h.svc.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	state = h.svc.GetState()
	if state.Status != "running" || state.RemainingSec != 1080 {
		t.Fatalf("unexpected resumed state: %+v", state)
	}

	persisted, err = h.store.Load()
	if err != nil {
		t.Fatalf("Load after resume: %v", err)
	}
	if persisted == nil || persisted.Status != store.StatusRunning || persisted.Deadline.IsZero() {
		t.Fatalf("unexpected persisted resumed state: %+v", persisted)
	}
}

func TestRestorePausedTimer(t *testing.T) {
	h := newHarness(t)
	if err := h.store.Save(store.State{
		Status:        store.StatusPaused,
		PresetMinutes: 15,
		RemainingSec:  420,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := h.svc.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	state := h.svc.GetState()
	if state.Status != "paused" || state.RemainingSec != 420 || state.PresetMinutes != 15 {
		t.Fatalf("unexpected restored paused state: %+v", state)
	}
	if len(h.notifier.messages()) != 0 {
		t.Fatalf("restore of a paused timer notified: %v", h.notifier.messages())
	}
}

func TestFinishNotifiesOnceAndClearsState(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(timer.PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}

	h.clock.Advance(30*time.Minute + time.Second)
	for i := 0; i < 3; i++ {
		h.svc.Tick()
	}

	messages := h.notifier.messages()
	if len(messages) != 1 {
		t.Fatalf("notifications = %v, want exactly one", messages)
	}
	if messages[0] != "Таймер завершён: Интервал 30 минут истёк" {
		t.Fatalf("unexpected notification: %q", messages[0])
	}
	if state := h.svc.GetState(); state.Status != "idle" {
		t.Fatalf("status = %q, want idle", state.Status)
	}
	persisted, err := h.store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted != nil {
		t.Fatalf("finished state still persisted: %+v", persisted)
	}
}

func TestRestoreRunningTimer(t *testing.T) {
	h := newHarness(t)
	if err := h.store.Save(store.State{
		PresetMinutes: 30,
		StartedAt:     h.clock.now.Add(-20 * time.Minute),
		Deadline:      h.clock.now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := h.svc.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	state := h.svc.GetState()
	if state.Status != "running" || state.RemainingSec != 600 {
		t.Fatalf("unexpected restored state: %+v", state)
	}
	if len(h.notifier.messages()) != 0 {
		t.Fatalf("restore of a live timer notified: %v", h.notifier.messages())
	}
}

func TestRestoreStaleTimerNotifiesMissed(t *testing.T) {
	h := newHarness(t)
	if err := h.store.Save(store.State{
		PresetMinutes: 15,
		StartedAt:     h.clock.now.Add(-30 * time.Minute),
		Deadline:      h.clock.now.Add(-15 * time.Minute),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := h.svc.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if state := h.svc.GetState(); state.Status != "idle" {
		t.Fatalf("status = %q, want idle", state.Status)
	}
	messages := h.notifier.messages()
	if len(messages) != 1 {
		t.Fatalf("notifications = %v, want exactly one missed-completion notice", messages)
	}
	if messages[0] != "Таймер завершён: Интервал завершился, пока приложение было закрыто" {
		t.Fatalf("unexpected notification: %q", messages[0])
	}
}

func TestRestoreCorruptStateSetsNotice(t *testing.T) {
	h := newHarness(t)
	path := h.store.Path()
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := h.svc.Restore(); err == nil {
		t.Fatal("Restore should report the corrupt state")
	}

	state := h.svc.GetState()
	if state.Status != "idle" {
		t.Fatalf("status = %q, want idle", state.Status)
	}
	if state.Notice == "" {
		t.Fatal("expected a notice about the unreadable state")
	}
}

func TestNotificationFailureSetsNotice(t *testing.T) {
	h := newHarness(t)
	h.notifier.err = errors.New("no notification daemon")
	if err := h.svc.Start(timer.PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}

	h.clock.Advance(15*time.Minute + time.Second)
	h.svc.Tick()

	state := h.svc.GetState()
	if state.Status != "idle" {
		t.Fatalf("status = %q, want idle even without a notification", state.Status)
	}
	if state.Notice == "" {
		t.Fatal("expected a notice about the undelivered notification")
	}
}

func TestStartClearsNotice(t *testing.T) {
	h := newHarness(t)
	h.notifier.err = errors.New("no notification daemon")
	if err := h.svc.Start(timer.PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.clock.Advance(15*time.Minute + time.Second)
	h.svc.Tick()

	if h.svc.GetState().Notice == "" {
		t.Fatal("expected a notice before restarting")
	}

	h.notifier.err = nil
	if err := h.svc.Start(timer.PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if notice := h.svc.GetState().Notice; notice != "" {
		t.Fatalf("notice = %q, want cleared on start", notice)
	}
}

func TestAutostartToggle(t *testing.T) {
	h := newHarness(t)

	if err := h.svc.SetAutostart(true); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !h.svc.GetState().Autostart {
		t.Fatal("state should report autostart as enabled")
	}

	if err := h.svc.SetAutostart(false); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if h.svc.GetState().Autostart {
		t.Fatal("state should report autostart as disabled")
	}
}

func TestAutostartFailureSetsNotice(t *testing.T) {
	h := newHarness(t)
	h.autostart.enableErr = errors.New("read-only config dir")

	if err := h.svc.SetAutostart(true); err == nil {
		t.Fatal("SetAutostart should return the registration error")
	}

	state := h.svc.GetState()
	if state.Autostart {
		t.Fatal("autostart must stay disabled after a failure")
	}
	if state.Notice == "" {
		t.Fatal("expected a notice about the failed registration")
	}
}

func TestPublishedSnapshotsTrackTicks(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(timer.PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.clock.Advance(42 * time.Second)
	h.svc.Tick()

	last := h.published[len(h.published)-1]
	if last.Status != "running" || last.RemainingSec != 858 {
		t.Fatalf("last snapshot = %+v, want running with 858s", last)
	}
}

package timer

import (
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)}
}

func TestPresetDurations(t *testing.T) {
	cases := []struct {
		minutes int
		want    time.Duration
	}{
		{PresetShort, 15 * time.Minute},
		{PresetMedium, 30 * time.Minute},
		{PresetLong, 60 * time.Minute},
	}

	for _, tc := range cases {
		got, err := ValidatePreset(tc.minutes)
		if err != nil {
			t.Fatalf("ValidatePreset(%d): %v", tc.minutes, err)
		}
		if got != tc.want {
			t.Errorf("ValidatePreset(%d) = %v, want %v", tc.minutes, got, tc.want)
		}
	}

	if _, err := ValidatePreset(10); err == nil {
		t.Fatal("ValidatePreset(10) should fail")
	}
}

func TestStartUsesFullRemaining(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)

	snap, err := tm.Start(PresetMedium)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if snap.Status != Running {
		t.Fatalf("status = %q, want %q", snap.Status, Running)
	}
	if snap.RemainingSec != 1800 || snap.TotalSec != 1800 {
		t.Fatalf("remaining/total = %d/%d, want 1800/1800", snap.RemainingSec, snap.TotalSec)
	}
	if snap.PresetMinutes != PresetMedium {
		t.Fatalf("preset = %d, want %d", snap.PresetMinutes, PresetMedium)
	}
}

func TestCountingDown(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}

	clock.Advance(90 * time.Second)
	snap, finished := tm.Tick()
	if finished {
		t.Fatal("timer finished too early")
	}
	if snap.RemainingSec != 810 {
		t.Fatalf("remaining = %d, want 810", snap.RemainingSec)
	}
}

func TestReplaceActiveInterval(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}
	clock.Advance(10 * time.Minute)

	snap, err := tm.Start(PresetShort)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if snap.RemainingSec != 900 {
		t.Fatalf("remaining after replacement = %d, want 900", snap.RemainingSec)
	}
}

func TestStopReturnsIdle(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetLong); err != nil {
		t.Fatalf("Start: %v", err)
	}

	snap := tm.Stop()
	if snap.Status != Idle {
		t.Fatalf("status = %q, want %q", snap.Status, Idle)
	}
	if snap.RemainingSec != 0 || snap.TotalSec != 0 {
		t.Fatalf("idle snapshot should be empty, got %+v", snap)
	}

	// Stopping an idle timer is a no-op.
	if snap := tm.Stop(); snap.Status != Idle {
		t.Fatalf("second Stop changed status to %q", snap.Status)
	}
}

func TestFinishHappensOnce(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}

	clock.Advance(15*time.Minute + time.Second)

	snap, finished := tm.Tick()
	if !finished {
		t.Fatal("first tick after deadline should finish the timer")
	}
	if snap.Status != Idle {
		t.Fatalf("status after finish = %q, want %q", snap.Status, Idle)
	}

	snap, finished = tm.Tick()
	if finished {
		t.Fatal("second tick must not report another completion")
	}
	if snap.Status != Idle {
		t.Fatalf("status stayed %q, want %q", snap.Status, Idle)
	}
}

func TestRemainingNeverNegative(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetShort); err != nil {
		t.Fatalf("Start: %v", err)
	}
	clock.Advance(2 * time.Hour)

	if got := tm.Snapshot().RemainingSec; got != 0 {
		t.Fatalf("remaining = %d, want 0", got)
	}
}

func TestSleepCountsAgainstWallClock(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetMedium); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Simulate 20 minutes of system sleep: the clock jumps, no ticks happen.
	clock.Advance(20 * time.Minute)

	snap, finished := tm.Tick()
	if finished {
		t.Fatal("timer finished after 20 of 30 minutes")
	}
	if snap.RemainingSec != 600 {
		t.Fatalf("remaining = %d, want 600", snap.RemainingSec)
	}
}

func TestClockJumpForwardFinishes(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)
	if _, err := tm.Start(PresetLong); err != nil {
		t.Fatalf("Start: %v", err)
	}

	clock.Advance(3 * time.Hour)

	if _, finished := tm.Tick(); !finished {
		t.Fatal("clock jump past the deadline should finish the timer")
	}
}

func TestRestore(t *testing.T) {
	clock := newFakeClock()
	tm := New(clock)

	deadline := clock.Now().Add(10 * time.Minute)
	snap, finished := tm.Restore(PresetMedium, deadline)
	if finished {
		t.Fatal("restore of a future deadline reported finished")
	}
	if snap.Status != Running || snap.RemainingSec != 600 {
		t.Fatalf("restored snapshot = %+v, want running with 600s", snap)
	}

	stale, finished := tm.Restore(PresetMedium, clock.Now().Add(-time.Minute))
	if !finished {
		t.Fatal("restore of a past deadline should report finished")
	}
	if stale.Status != Idle {
		t.Fatalf("status after stale restore = %q, want %q", stale.Status, Idle)
	}
}

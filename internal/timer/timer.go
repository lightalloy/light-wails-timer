// Package timer implements the countdown state machine used by the application.
//
// The timer stores an absolute deadline instead of decrementing a counter, so a
// suspended or restarted process still computes the honest remaining time.
package timer

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Status is the externally visible state of the timer.
type Status string

const (
	// Idle means no countdown is running.
	Idle Status = "idle"
	// Running means a countdown is active.
	Running Status = "running"
)

// Supported presets in minutes.
const (
	PresetShort  = 15
	PresetMedium = 30
	PresetLong   = 60
)

// Presets lists the supported interval lengths in minutes.
var Presets = []int{PresetShort, PresetMedium, PresetLong}

// ErrInvalidPreset is returned when a preset outside Presets is requested.
var ErrInvalidPreset = errors.New("invalid preset")

// Clock abstracts the system time so tests can control the passage of time.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// SystemClock returns a Clock backed by the real system time.
func SystemClock() Clock { return systemClock{} }

// Snapshot is a point-in-time view of the timer, safe to send to the UI.
type Snapshot struct {
	Status        Status `json:"status"`
	PresetMinutes int    `json:"presetMinutes"`
	RemainingSec  int    `json:"remainingSec"`
	TotalSec      int    `json:"totalSec"`
}

// Timer is a single countdown. It is not safe for concurrent use; callers must
// serialise access (the service package does this).
type Timer struct {
	clock    Clock
	status   Status
	minutes  int
	deadline time.Time
}

// New creates an idle timer driven by the given clock.
func New(clock Clock) *Timer {
	return &Timer{clock: clock, status: Idle}
}

// ValidatePreset returns the duration for a supported preset or ErrInvalidPreset.
func ValidatePreset(minutes int) (time.Duration, error) {
	for _, preset := range Presets {
		if preset == minutes {
			return time.Duration(preset) * time.Minute, nil
		}
	}
	return 0, fmt.Errorf("%w: %d minutes", ErrInvalidPreset, minutes)
}

// Start begins a countdown for the given preset, replacing any running interval.
func (t *Timer) Start(minutes int) (Snapshot, error) {
	duration, err := ValidatePreset(minutes)
	if err != nil {
		return t.Snapshot(), err
	}
	t.status = Running
	t.minutes = minutes
	t.deadline = t.clock.Now().Add(duration)
	return t.Snapshot(), nil
}

// Restore re-instates a previously persisted countdown. It reports finished=true
// when the deadline is in the past (or the data is unusable), leaving the timer
// idle so the caller can notify about the missed completion.
func (t *Timer) Restore(minutes int, deadline time.Time) (Snapshot, bool) {
	if minutes <= 0 || deadline.IsZero() || !t.clock.Now().Before(deadline) {
		t.reset()
		return t.Snapshot(), true
	}
	t.status = Running
	t.minutes = minutes
	t.deadline = deadline
	return t.Snapshot(), false
}

// Stop cancels the countdown and returns the idle snapshot.
func (t *Timer) Stop() Snapshot {
	t.reset()
	return t.Snapshot()
}

// Tick re-evaluates the countdown. It reports finished=true exactly once, on the
// tick that observes the deadline has been reached.
func (t *Timer) Tick() (Snapshot, bool) {
	if t.status != Running {
		return t.Snapshot(), false
	}
	if !t.clock.Now().Before(t.deadline) {
		t.reset()
		return t.Snapshot(), true
	}
	return t.Snapshot(), false
}

// Deadline returns the active deadline, if any.
func (t *Timer) Deadline() (time.Time, bool) {
	if t.status != Running {
		return time.Time{}, false
	}
	return t.deadline, true
}

// Snapshot returns the current view of the timer.
func (t *Timer) Snapshot() Snapshot {
	if t.status != Running {
		return Snapshot{Status: Idle}
	}
	return Snapshot{
		Status:        Running,
		PresetMinutes: t.minutes,
		RemainingSec:  t.remaining(),
		TotalSec:      t.minutes * 60,
	}
}

func (t *Timer) reset() {
	t.status = Idle
	t.minutes = 0
	t.deadline = time.Time{}
}

func (t *Timer) remaining() int {
	left := t.deadline.Sub(t.clock.Now())
	if left <= 0 {
		return 0
	}
	// Round up so a freshly started interval reads "30:00" rather than "29:59".
	return int(math.Ceil(left.Seconds()))
}

// Package service coordinates the timer, persistence, notifications and
// autostart. It is the single source of truth for the UI: every state change is
// published as a snapshot.
package service

import (
	"fmt"
	"sync"
	"time"

	"light-wails-timer/internal/i18n"
	"light-wails-timer/internal/store"
	"light-wails-timer/internal/timer"
)

// Notifier delivers a system notification.
type Notifier interface {
	Send(title, body string) error
}

// Autostart controls the "launch at login" registration.
type Autostart interface {
	Enable() error
	Disable() error
	IsEnabled() (bool, error)
}

// State is what the UI renders and what is emitted on the timer:state event.
type State struct {
	Status        string `json:"status"`
	PresetMinutes int    `json:"presetMinutes"`
	RemainingSec  int    `json:"remainingSec"`
	TotalSec      int    `json:"totalSec"`
	Notice        string `json:"notice"`
	Autostart     bool   `json:"autostart"`
}

// Service owns the timer and publishes snapshots.
type Service struct {
	mu          sync.Mutex
	timer       *timer.Timer
	store       *store.Store
	notifier    Notifier
	autostart   Autostart
	publish     func(State)
	logf        func(format string, args ...any)
	notice      string
	autostartOn bool
}

// New builds a service. publish may be nil (useful in tests).
func New(clock timer.Clock, st *store.Store, notifier Notifier, autostart Autostart, publish func(State), logf func(string, ...any)) *Service {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{
		timer:     timer.New(clock),
		store:     st,
		notifier:  notifier,
		autostart: autostart,
		publish:   publish,
		logf:      logf,
	}
}

// Restore loads a persisted countdown. A deadline in the past is reported as a
// missed completion: the state is cleared and a single notification is sent.
func (s *Service) Restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.autostartOn = s.readAutostartLocked()

	persisted, err := s.store.Load()
	if err != nil {
		s.logf("load timer state: %v", err)
		s.notice = i18n.NoticeStateReadFailed
		s.publishLocked()
		return err
	}
	if persisted == nil {
		s.publishLocked()
		return nil
	}

	_, finished := s.timer.Restore(persisted.PresetMinutes, persisted.Deadline)
	if !finished {
		s.publishLocked()
		return nil
	}

	if err := s.store.Clear(); err != nil {
		s.logf("clear stale timer state: %v", err)
	}
	s.publishLocked()
	s.notifyLocked(i18n.NotificationTitle, i18n.NotificationMissedBody)
	return nil
}

// Start begins a countdown, replacing any active interval without notifying.
func (s *Service) Start(minutes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.timer.Start(minutes)
	if err != nil {
		return err
	}
	s.notice = ""

	deadline, ok := s.timer.Deadline()
	if ok {
		if err := s.store.Save(store.State{
			PresetMinutes: snapshot.PresetMinutes,
			StartedAt:     deadline.Add(-time.Duration(snapshot.TotalSec) * time.Second),
			Deadline:      deadline,
		}); err != nil {
			s.logf("save timer state: %v", err)
			s.notice = i18n.NoticeSaveFailed
		}
	}

	s.publishLocked()
	return nil
}

// Stop cancels the countdown and clears the persisted state.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.timer.Stop()
	if err := s.store.Clear(); err != nil {
		s.logf("clear timer state: %v", err)
	}
	s.publishLocked()
	return nil
}

// Tick re-evaluates the countdown and publishes the current snapshot.
func (s *Service) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.timer.Snapshot()
	_, finished := s.timer.Tick()

	if finished {
		if err := s.store.Clear(); err != nil {
			s.logf("clear finished timer state: %v", err)
		}
		s.publishLocked()
		s.notifyLocked(i18n.NotificationTitle, fmt.Sprintf(i18n.NotificationBodyFormat, before.PresetMinutes))
		return
	}
	s.publishLocked()
}

// GetState returns the current snapshot for the initial UI render.
func (s *Service) GetState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

// SetAutostart enables or disables launch at login.
func (s *Service) SetAutostart(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	if enabled {
		err = s.autostart.Enable()
	} else {
		err = s.autostart.Disable()
	}
	if err != nil {
		s.logf("change autostart: %v", err)
		s.notice = i18n.NoticeAutostartFailed
		s.autostartOn = s.readAutostartLocked()
		s.publishLocked()
		return err
	}

	s.autostartOn = s.readAutostartLocked()
	if s.autostartOn != enabled {
		s.notice = i18n.NoticeAutostartFailed
	}
	s.publishLocked()
	return nil
}

// AutostartEnabled reports the last known registration state.
func (s *Service) AutostartEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autostartOn
}

func (s *Service) readAutostartLocked() bool {
	enabled, err := s.autostart.IsEnabled()
	if err != nil {
		s.logf("read autostart state: %v", err)
		return false
	}
	return enabled
}

func (s *Service) notifyLocked(title, body string) {
	if err := s.notifier.Send(title, body); err != nil {
		s.logf("send notification: %v", err)
		s.notice = i18n.NoticeNotifyFailed
		s.publishLocked()
	}
}

func (s *Service) publishLocked() {
	if s.publish != nil {
		s.publish(s.stateLocked())
	}
}

func (s *Service) stateLocked() State {
	snapshot := s.timer.Snapshot()
	return State{
		Status:        string(snapshot.Status),
		PresetMinutes: snapshot.PresetMinutes,
		RemainingSec:  snapshot.RemainingSec,
		TotalSec:      snapshot.TotalSec,
		Notice:        s.notice,
		Autostart:     s.autostartOn,
	}
}

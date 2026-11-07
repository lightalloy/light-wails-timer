package main

import (
	"context"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"light-wails-timer/internal/service"
)

// TimerService exposes the timer core to the frontend through Wails bindings.
type TimerService struct {
	core *service.Service
}

// Start begins a countdown for one of the supported presets.
func (s *TimerService) Start(minutes int) error {
	return s.core.Start(minutes)
}

// Pause freezes the running countdown.
func (s *TimerService) Pause() error {
	return s.core.Pause()
}

// Resume continues a paused countdown.
func (s *TimerService) Resume() error {
	return s.core.Resume()
}

// Stop cancels the running countdown.
func (s *TimerService) Stop() error {
	return s.core.Stop()
}

// GetState returns the snapshot the window renders on load.
func (s *TimerService) GetState() service.State {
	return s.core.GetState()
}

// SetAutostart enables or disables launch at login.
func (s *TimerService) SetAutostart(enabled bool) error {
	return s.core.SetAutostart(enabled)
}

// AutostartEnabled reports the current registration state.
func (s *TimerService) AutostartEnabled() bool {
	return s.core.AutostartEnabled()
}

// ServiceStartup restores the persisted countdown and starts the one-second tick.
func (s *TimerService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if err := s.core.Restore(); err != nil {
		log.Printf("restore timer: %v", err)
	}
	go s.tick(ctx)
	return nil
}

func (s *TimerService) tick(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.core.Tick()
		}
	}
}

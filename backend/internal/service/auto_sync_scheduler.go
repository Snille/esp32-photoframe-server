package service

import (
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

type AutoSyncScheduler struct {
	name          string
	settings      *SettingsService
	isRelevantKey func(key string) bool
	isConfigured  func() bool
	getConfig     func() (bool, time.Duration)
	runSync       func() error
	retryInterval time.Duration
	resetCh       chan struct{}
	startOnce     sync.Once
	runMu         sync.Mutex
	stateMu       sync.Mutex
	lastSuccessAt time.Time
	retryAfter    time.Time
}

func NewAutoSyncScheduler(opts AutoSyncSchedulerOptions) *AutoSyncScheduler {
	retry := opts.RetryInterval
	if retry <= 0 {
		retry = 5 * time.Minute
	}
	return &AutoSyncScheduler{
		name:          opts.Name,
		settings:      opts.Settings,
		isRelevantKey: opts.IsRelevantKey,
		isConfigured:  opts.IsConfigured,
		getConfig:     opts.GetConfig,
		runSync:       opts.RunSync,
		retryInterval: retry,
		resetCh:       make(chan struct{}, 1),
	}
}

type AutoSyncSchedulerOptions struct {
	Name          string
	Settings      *SettingsService
	IsRelevantKey func(key string) bool
	IsConfigured  func() bool
	GetConfig     func() (bool, time.Duration)
	RunSync       func() error
	RetryInterval time.Duration
}

func (s *AutoSyncScheduler) Start() {
	s.startOnce.Do(func() {
		s.settings.RegisterOnChange(func(key, _ string) {
			if s.isRelevantKey(key) {
				s.TriggerReset()
			}
		})
		go s.loop()
	})
}

func (s *AutoSyncScheduler) TriggerReset() {
	select {
	case s.resetCh <- struct{}{}:
	default:
	}
}

// SyncNow runs the configured sync function. It recovers any panic from
// runSync and treats it as a normal failure (logged + backed off), rather
// than letting it propagate out of this goroutine: an unrecovered panic here
// would crash the entire server process (this runs on a long-lived
// background loop, not behind Echo's per-request Recover() middleware), not
// just this one sync — which would silently stop every frame from being
// served until someone noticed and restarted the container.
func (s *AutoSyncScheduler) SyncNow() (err error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s sync: %v\n%s", s.name, r, debug.Stack())
			err = fmt.Errorf("panic: %v", r)
		}
		if err != nil {
			s.stateMu.Lock()
			s.retryAfter = time.Now().Add(s.retryInterval)
			s.stateMu.Unlock()
		}
	}()

	if err = s.runSync(); err != nil {
		return err
	}

	s.stateMu.Lock()
	s.lastSuccessAt = time.Now()
	s.retryAfter = time.Time{}
	s.stateMu.Unlock()
	s.TriggerReset()
	return nil
}

func (s *AutoSyncScheduler) loop() {
	timer := time.NewTimer(s.nextDelay())
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			s.tryRunDue()
		case <-s.resetCh:
		}

		delay := s.nextDelay()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (s *AutoSyncScheduler) tryRunDue() {
	enabled, _ := s.getConfig()
	if !enabled || !s.isConfigured() {
		return
	}

	if err := s.SyncNow(); err != nil {
		log.Printf("%s auto-sync failed: %v", s.name, err)
		return
	}

	log.Printf("%s auto-sync completed", s.name)
}

func (s *AutoSyncScheduler) nextDelay() time.Duration {
	enabled, interval := s.getConfig()
	if !enabled || !s.isConfigured() {
		return 24 * time.Hour
	}

	now := time.Now()
	s.stateMu.Lock()
	lastSuccessAt := s.lastSuccessAt
	retryAfter := s.retryAfter
	s.stateMu.Unlock()

	if !retryAfter.IsZero() && now.Before(retryAfter) {
		return time.Until(retryAfter)
	}

	if lastSuccessAt.IsZero() {
		return 0
	}

	nextRun := lastSuccessAt.Add(interval)
	if !now.Before(nextRun) {
		return 0
	}

	return time.Until(nextRun)
}

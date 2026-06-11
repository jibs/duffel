package search

import "sync"

// ReindexScheduler coalesces repeated reindex triggers so only one qmd update
// runs at a time, with at most one pending rerun.
type ReindexScheduler struct {
	mu          sync.Mutex
	running     bool
	pending     bool
	start       func(func(error)) error
	onCompleted func(error)
}

// NewReindexScheduler creates a scheduler backed by qmd StartIndexing.
func NewReindexScheduler(collection string, onCompleted func(error)) *ReindexScheduler {
	return &ReindexScheduler{
		start: func(done func(error)) error {
			return StartIndexing(collection, done)
		},
		onCompleted: onCompleted,
	}
}

// newReindexSchedulerWithStart is test-only injection for custom starters.
func newReindexSchedulerWithStart(start func(func(error)) error, onCompleted func(error)) *ReindexScheduler {
	return &ReindexScheduler{
		start:       start,
		onCompleted: onCompleted,
	}
}

// Trigger starts indexing, or marks a single pending rerun if one is active.
func (s *ReindexScheduler) Trigger() error {
	s.mu.Lock()
	if s.running {
		s.pending = true
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	if err := s.start(s.handleDone); err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return err
	}

	return nil
}

func (s *ReindexScheduler) handleDone(err error) {
	s.callOnCompleted(err)

	s.mu.Lock()
	if !s.pending {
		s.running = false
		s.mu.Unlock()
		return
	}
	s.pending = false
	s.mu.Unlock()

	if startErr := s.start(s.handleDone); startErr != nil {
		s.callOnCompleted(startErr)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}
}

func (s *ReindexScheduler) callOnCompleted(err error) {
	if s.onCompleted != nil {
		s.onCompleted(err)
	}
}

package search

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReindexSchedulerFirstTriggerStartsRun(t *testing.T) {
	var done func(error)
	started := make(chan struct{}, 1)

	s := newReindexSchedulerWithStart(func(cb func(error)) error {
		done = cb
		started <- struct{}{}
		return nil
	}, nil)

	if err := s.Trigger(); err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first trigger did not start indexing")
	}

	if done == nil {
		t.Fatal("start callback was not captured")
	}
	done(nil)
}

func TestReindexSchedulerCoalescesPendingTrigger(t *testing.T) {
	var (
		mu        sync.Mutex
		doneFns   []func(error)
		starts    int
		startedCh = make(chan struct{}, 4)
	)

	s := newReindexSchedulerWithStart(func(cb func(error)) error {
		mu.Lock()
		doneFns = append(doneFns, cb)
		starts++
		mu.Unlock()
		startedCh <- struct{}{}
		return nil
	}, nil)

	if err := s.Trigger(); err != nil {
		t.Fatalf("first Trigger() error = %v", err)
	}
	<-startedCh

	if err := s.Trigger(); err != nil {
		t.Fatalf("second Trigger() error = %v", err)
	}
	if err := s.Trigger(); err != nil {
		t.Fatalf("third Trigger() error = %v", err)
	}

	mu.Lock()
	firstDone := doneFns[0]
	mu.Unlock()
	firstDone(nil)

	select {
	case <-startedCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("pending trigger did not schedule exactly one rerun")
	}

	select {
	case <-startedCh:
		t.Fatal("unexpected extra rerun started")
	case <-time.After(120 * time.Millisecond):
	}

	mu.Lock()
	if starts != 2 {
		t.Fatalf("starts = %d, want 2", starts)
	}
	secondDone := doneFns[1]
	mu.Unlock()
	secondDone(nil)
}

func TestReindexSchedulerStartFailureAllowsFutureTriggers(t *testing.T) {
	var attempts atomic.Int32
	started := make(chan struct{}, 2)

	s := newReindexSchedulerWithStart(func(cb func(error)) error {
		n := attempts.Add(1)
		started <- struct{}{}
		if n == 1 {
			return errors.New("boom")
		}
		cb(nil)
		return nil
	}, nil)

	if err := s.Trigger(); err == nil {
		t.Fatal("first Trigger() error = nil, want non-nil")
	}
	<-started

	if err := s.Trigger(); err != nil {
		t.Fatalf("second Trigger() error = %v", err)
	}
	<-started

	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

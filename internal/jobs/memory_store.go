package jobs

import (
	"context"
	"reflect"
	"sync"
	"time"
)

// MemoryStore is a deterministic prototype of the atomic operations a future
// durable store must implement. A lease may be reclaimed only after expiry.
type MemoryStore struct {
	mu        sync.Mutex
	active    map[JobName]lease
	lastStart map[JobName]time.Time
	bindings  map[string]string
	completed map[string]Receipt
}

type lease struct {
	runID string
	until time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{active: make(map[JobName]lease), lastStart: make(map[JobName]time.Time), bindings: make(map[string]string), completed: make(map[string]Receipt)}
}

func (s *MemoryStore) Acquire(ctx context.Context, definition Definition, runID, inputDigest string, now, leaseUntil time.Time) (AcquireResult, error) {
	if err := ctx.Err(); err != nil {
		return AcquireResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(definition.Name) + "\x00" + runID
	if bound, ok := s.bindings[key]; ok && bound != inputDigest {
		return AcquireResult{}, ErrIdempotencyConflict
	}
	if receipt, ok := s.completed[key]; ok {
		copy := receipt
		return AcquireResult{Existing: &copy}, nil
	}
	if current, ok := s.active[definition.Name]; ok && now.Before(current.until) {
		return AcquireResult{}, ErrAlreadyRunning
	}
	if last, ok := s.lastStart[definition.Name]; ok && now.Before(last.Add(definition.MinimumInterval)) {
		return AcquireResult{}, ErrTooSoon
	}
	// The semantic binding is permanent even if this process dies before
	// Finish. A durable implementation must commit it atomically with the lease.
	s.bindings[key] = inputDigest
	s.active[definition.Name] = lease{runID: runID, until: leaseUntil}
	s.lastStart[definition.Name] = now
	return AcquireResult{Acquired: true}, nil
}

func (s *MemoryStore) Finish(ctx context.Context, receipt Receipt) error {
	// Completion is persisted even when the execution context timed out. A
	// persistent implementation should use its own bounded storage context too.
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(receipt.Job) + "\x00" + receipt.RunID
	if existing, ok := s.completed[key]; ok {
		if reflect.DeepEqual(existing, receipt) {
			return nil
		}
		return ErrInvalidInput
	}
	current, ok := s.active[receipt.Job]
	if !ok || current.runID != receipt.RunID {
		return ErrAlreadyRunning
	}
	s.completed[key] = receipt
	delete(s.active, receipt.Job)
	return nil
}

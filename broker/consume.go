package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Handler processes a decoded event. It must be safe to call at-least-once: the
// broker is assumed at-least-once delivery, and Dispatch only guarantees
// at-most-once *handler* invocation per event id through the IdempotencyStore.
type Handler func(ctx context.Context, ev *Envelope) error

// IdempotencyStore records which event ids have already been processed, so a
// redelivered message is not handled twice (Audit Design: at-least-once
// assumed). Implementations must be safe for concurrent use.
type IdempotencyStore interface {
	// Seen atomically reports whether eventID was already recorded; if not, it
	// records it and returns false. A non-nil error aborts processing (the
	// message should be retried later).
	Seen(ctx context.Context, eventID string) (bool, error)
}

// Dispatch decodes a raw broker payload into an envelope and invokes handler
// exactly once per event id, deduplicating redeliveries via store. A payload
// that fails to decode or validate is rejected without invoking the handler.
//
// It returns nil when the message is a duplicate (already processed) so the
// caller can safely acknowledge it.
func Dispatch(ctx context.Context, payload []byte, store IdempotencyStore, handler Handler) error {
	if handler == nil {
		return errors.New("broker: nil handler")
	}

	ev := &Envelope{}
	if err := json.Unmarshal(payload, ev); err != nil {
		return fmt.Errorf("broker: decode envelope: %w", err)
	}

	if err := ev.Validate(); err != nil {
		return err
	}

	if store != nil {
		seen, err := store.Seen(ctx, ev.EventID)
		if err != nil {
			return fmt.Errorf("broker: idempotency check for %q: %w", ev.EventID, err)
		}

		if seen {
			// Already processed — safe to acknowledge.
			return nil
		}
	}

	return handler(ctx, ev)
}

// MemoryIdempotencyStore is an in-process IdempotencyStore. It is suitable for
// tests and single-instance consumers; multi-instance consumers should back the
// store with shared state (e.g. Redis) so dedup holds across replicas.
type MemoryIdempotencyStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewMemoryIdempotencyStore returns an empty in-memory store.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{seen: make(map[string]struct{})}
}

// Seen records eventID and reports whether it was already present.
func (s *MemoryIdempotencyStore) Seen(_ context.Context, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[eventID]; ok {
		return true, nil
	}

	s.seen[eventID] = struct{}{}

	return false, nil
}

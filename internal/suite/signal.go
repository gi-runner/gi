package suite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/value"
)

// ErrYield asks a parked call to run another retained call and wait again.
var ErrYield = errors.New("yield")

// mailbox retains early signals. Replacing the notification channel under the
// queue mutex makes delivery, timeout and cancellation free of lost wakeups.
type mailbox struct {
	mu    sync.Mutex
	ready chan struct{}
	queue []any
}

func (s *Suite) Signal(v any) error {
	copied, err := value.Clone(v)
	if err != nil {
		return err
	}
	s.box.mu.Lock()
	defer s.box.mu.Unlock()
	s.box.queue = append(s.box.queue, copied)
	if s.box.ready != nil {
		close(s.box.ready)
	}
	s.box.ready = make(chan struct{})
	return nil
}

func (s *Suite) Listen(ctx context.Context, timeout time.Duration, wake <-chan struct{}) (any, error) {
	if timeout < 0 {
		return nil, errors.New("listen timeout must be >= 0")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.box.mu.Lock()
		if len(s.box.queue) > 0 {
			got := s.box.queue[0]
			s.box.queue[0] = nil
			s.box.queue = s.box.queue[1:]
			s.box.mu.Unlock()
			return got, nil
		}
		if s.box.ready == nil {
			s.box.ready = make(chan struct{})
		}
		ready := s.box.ready
		s.box.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("listen timed out after %s", timeout)
		case <-wake:
			return nil, ErrYield
		case <-ready:
		}
	}
}

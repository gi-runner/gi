package suite

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	locksUnknown uint32 = iota
	locksNone
	locksUsed
)

// scenarioLocks grants a scenario's complete lock set atomically. Context-owned
// waiting avoids lock goroutines; a pending exclusive lock prevents starvation.
type scenarioLocks struct {
	mu        sync.Mutex
	changed   chan struct{}
	names     map[string]int
	active    int
	exclusive bool
	writers   int
	mode      atomic.Uint32
}

// UseLocks records whether any scenario on this suite carries @lock.
// Pass false only before scenarios start. A later true sticks.
func (s *Suite) UseLocks(used bool) {
	if s == nil {
		return
	}
	if used {
		s.locks.mode.Store(locksUsed)
		return
	}
	s.locks.mode.CompareAndSwap(locksUnknown, locksNone)
}

func (s *Suite) Acquire(ctx context.Context, tags []string) (func(), error) {
	names, exclusive := lockNames(tags)
	if s.locks.mode.Load() == locksNone && names == nil && !exclusive {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return func() {}, nil
	}
	if names != nil || exclusive {
		s.locks.mode.Store(locksUsed)
	}
	l := &s.locks
	l.mu.Lock()
	if l.changed == nil {
		l.changed = make(chan struct{})
		l.names = make(map[string]int)
	}
	if exclusive {
		l.writers++
	}
	for {
		if err := ctx.Err(); err != nil {
			if exclusive {
				l.writers--
				l.notify()
			}
			l.mu.Unlock()
			return nil, err
		}
		available := !l.exclusive
		if exclusive {
			available = available && l.active == 0
		} else {
			available = available && l.writers == 0
			for name := range names {
				if l.names[name] > 0 {
					available = false
					break
				}
			}
		}
		if available {
			l.active++
			if exclusive {
				l.writers--
				l.exclusive = true
			} else {
				for name := range names {
					l.names[name]++
				}
			}
			l.mu.Unlock()
			return sync.OnceFunc(func() {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.active--
				l.exclusive = false
				for name := range names {
					l.names[name]--
					if l.names[name] <= 0 {
						delete(l.names, name)
					}
				}
				l.notify()
			}), nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		l.mu.Lock()
	}
}

func (l *scenarioLocks) notify() { close(l.changed); l.changed = make(chan struct{}) }

func lockNames(tags []string) (map[string]bool, bool) {
	var names map[string]bool
	exclusive := false
	for _, tag := range tags {
		raw, ok := strings.CutPrefix(strings.TrimPrefix(tag, "@"), "lock=")
		if !ok {
			continue
		}
		for name := range strings.SplitSeq(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if name == "*" {
				exclusive = true
				continue
			}
			if names == nil {
				names = map[string]bool{}
			}
			names[name] = true
		}
	}
	return names, exclusive
}

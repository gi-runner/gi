package suite

import (
	"context"
	"strings"
	"sync"
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
}

func (s *Suite) Acquire(ctx context.Context, tags []string) (func(), error) {
	names := make(map[string]bool)
	exclusive := false
	for _, tag := range tags {
		if raw, ok := strings.CutPrefix(strings.TrimPrefix(tag, "@"), "lock="); ok {
			for name := range strings.SplitSeq(raw, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					names[name] = true
				}
				if name == "*" {
					exclusive = true
				}
			}
		}
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
			var once sync.Once
			return func() {
				once.Do(func() {
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
				})
			}, nil
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

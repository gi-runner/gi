package script

import (
	"sync"

	"github.com/grafana/sobek"
)

// Programs contain immutable bytecode, not scenario values or runtime objects.
// Bound both entries and source bytes so generated expressions cannot grow the cache.
const (
	maxPrograms      = 256
	maxProgramSource = 256 << 10
)

type programCache struct {
	mu      sync.RWMutex
	entries map[string]*sobek.Program
	bytes   int
}

var programs programCache

func (c *programCache) compile(source string) (*sobek.Program, error) {
	c.mu.RLock()
	program := c.entries[source]
	c.mu.RUnlock()
	if program != nil {
		return program, nil
	}
	program, err := sobek.Compile("expr", "("+source+")", false)
	if err != nil {
		program, err = sobek.Compile("stmt", source, false)
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.entries[source]; existing != nil {
		return existing, nil
	}
	if len(c.entries) < maxPrograms && len(source) <= maxProgramSource-c.bytes {
		if c.entries == nil {
			c.entries = make(map[string]*sobek.Program)
		}
		c.entries[source] = program
		c.bytes += len(source)
	}
	return program, nil
}

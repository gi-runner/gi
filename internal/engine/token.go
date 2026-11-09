package engine

import (
	"sync"
	"time"
)

// TokenCache keeps OAuth access tokens for one functional run or one load profile.
// Scenarios and iterations with the same client, grant, and user share one entry.
type TokenCache struct {
	mu      sync.Mutex
	entries map[string]*tokenEntry
}

type tokenEntry struct {
	mu      sync.Mutex
	access  string
	refresh string
	until   time.Time
}

func (c *TokenCache) entry(key string) *tokenEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*tokenEntry{}
	}
	e, ok := c.entries[key]
	if !ok {
		e = &tokenEntry{}
		c.entries[key] = e
	}
	return e
}

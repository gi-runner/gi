package suite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/value"
)

type Suite struct {
	Root    string
	Env     string
	Base    string
	Main    string
	Overlay string
	Err     error

	locks  scenarioLocks
	mu     sync.Mutex
	once   map[string]*flight
	single map[string]*flight
	box    mailbox
}

type flight struct {
	val      any
	err      error
	wait     chan struct{}
	finished bool
	waiting  map[*flight]int
}

func Open(start, env string) *Suite {
	root := findRoot(start)
	s := &Suite{
		Root:   root,
		Env:    env,
		once:   map[string]*flight{},
		single: map[string]*flight{},
	}
	load := func(names ...string) string {
		src, err := readFirst(root, names...)
		s.Err = errors.Join(s.Err, err)
		return src
	}
	s.Base = load("gi-base.js", "karate-base.js")
	s.Main = load("gi-config.js", "karate-config.js")
	if env != "" {
		s.Overlay = load("gi-config-"+env+".js", "karate-config-"+env+".js")
	}
	return s
}

func findRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		dir = start
	}
	if dir == "" {
		dir = "."
	}
	if st, err := os.Stat(dir); (err == nil && !st.IsDir()) || (errors.Is(err, os.ErrNotExist) && strings.HasSuffix(dir, ".feature")) {
		dir = filepath.Dir(dir)
	}
	startDir := dir
	for {
		if hasConfig(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return startDir
		}
		dir = parent
	}
}

func hasConfig(dir string) bool {
	for _, name := range []string{"gi-config.js", "karate-config.js", "gi-base.js", "karate-base.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func readFirst(dir string, names ...string) (string, error) {
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return string(b), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("config %s: %w", filepath.Join(dir, name), err)
		}
	}
	return "", nil
}

func Resolve(root, from, ref string) string {
	if path, ok := strings.CutPrefix(ref, "classpath:"); ok {
		return filepath.Join(root, path)
	}
	if path, ok := strings.CutPrefix(ref, "this:"); ok {
		return filepath.Join(from, path)
	}
	if path, ok := strings.CutPrefix(ref, "file:"); ok {
		return path
	}
	if after, ok := strings.CutPrefix(ref, "/"); ok {
		return filepath.Join(root, after)
	}
	rel := filepath.Join(from, ref)
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if root != "" {
		alt := filepath.Join(root, ref)
		if _, err := os.Stat(alt); err == nil {
			return alt
		}
	}
	return rel
}

func (s *Suite) DoOnce(ctx context.Context, key string, run func(context.Context) (any, error)) (any, error) {
	return s.do(ctx, &s.once, key, run)
}

func (s *Suite) DoSingle(ctx context.Context, key string, run func(context.Context) (any, error)) (any, error) {
	return s.do(ctx, &s.single, key, run)
}

// Cached data is copied at publication and on every read.
// A cached function keeps calling the runtime that created it.
func (s *Suite) do(ctx context.Context, cache *map[string]*flight, key string, run func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, _ := ctx.Value(cacheContextKey{}).(cacheContext)
	s.mu.Lock()
	f, exists := (*cache)[key]
	if !exists {
		f = &flight{wait: make(chan struct{}), waiting: make(map[*flight]int)}
		if *cache == nil {
			*cache = make(map[string]*flight)
		}
		(*cache)[key] = f
	}
	if owner.suite == s && !f.finished {
		if dependsOn(f, owner.flight, make(map[*flight]bool)) {
			s.mu.Unlock()
			return nil, fmt.Errorf("cyclic cached call: %s", key)
		}
		owner.flight.waiting[f]++
		defer func() {
			s.mu.Lock()
			owner.flight.waiting[f]--
			if owner.flight.waiting[f] == 0 {
				delete(owner.flight.waiting, f)
			}
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	if exists {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.wait:
			return cachedValue(f)
		}
	}
	completed := false
	defer func() {
		if !completed {
			s.mu.Lock()
			f.err = errors.New("cached operation did not complete")
			f.finished = true
			close(f.wait)
			s.mu.Unlock()
		}
	}()
	runCtx := context.WithValue(ctx, cacheContextKey{}, cacheContext{suite: s, flight: f})
	f.val, f.err = run(runCtx)
	if f.err == nil {
		f.val, f.err = cloneCached(f.val)
		if f.err != nil {
			f.err = fmt.Errorf("cache %q requires JSON data: %w", key, f.err)
		}
	}
	s.mu.Lock()
	f.finished = true
	completed = true
	close(f.wait)
	s.mu.Unlock()
	return cachedValue(f)
}

// Cache dependencies belong to the execution context, not a goroutine ID. The
// graph also detects cycles between flights started by different scenarios.
type cacheContextKey struct{}
type cacheContext struct {
	suite  *Suite
	flight *flight
}

// Caller holds the suite mutex. Completed entries cannot block another flight.
func dependsOn(from, target *flight, seen map[*flight]bool) bool {
	if from == target {
		return true
	}
	if from.finished || seen[from] {
		return false
	}
	seen[from] = true
	for next := range from.waiting {
		if dependsOn(next, target, seen) {
			return true
		}
	}
	return false
}

func cachedValue(f *flight) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return cloneCached(f.val)
}

// A cache entry with non-JSON execution state can define its own ownership copy.
// Implementations must detach all mutable data and reject runtime-owned handles.
type cacheCloner interface{ CloneCache() (any, error) }

func cloneCached(v any) (any, error) {
	if c, ok := v.(cacheCloner); ok {
		return c.CloneCache()
	}
	return value.Clone(v)
}

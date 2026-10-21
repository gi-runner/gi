package match

import (
	"strings"
	"sync"
	"testing"
)

func TestRegexCacheConcurrentAndAnchored(t *testing.T) {
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 50 {
				re, err := getRegex("a|b")
				if err != nil {
					t.Error(err)
					return
				}
				if !re.MatchString("a") || re.MatchString("ab") {
					t.Error("lost whole-string matching")
				}
				if _, err := getRegex("["); err == nil {
					t.Error("accepted invalid pattern")
				}
			}
		})
	}
	wg.Wait()
}

func TestRegexCacheDoesNotRetainLargePatterns(t *testing.T) {
	pattern := strings.Repeat("a", 4097)
	if _, err := getRegex(pattern); err != nil {
		t.Fatal(err)
	}
	regexMu.RLock()
	_, cached := regexCache[pattern]
	regexMu.RUnlock()
	if cached {
		t.Fatal("retained a large dynamic expression")
	}
}

package script

import (
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSharedProgramsKeepRuntimeStateSeparate(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			v := New()
			v.Set("counter", int64(i))
			for range 3 {
				if _, err := v.Eval(`counter++`); err != nil {
					t.Error(err)
					return
				}
			}
			got, err := v.Eval("counter")
			if err != nil || got != int64(i+3) {
				t.Errorf("runtime %d: %v %v", i, got, err)
			}
		})
	}
	wg.Wait()
}

func TestProgramCacheBoundsAndSyntaxErrors(t *testing.T) {
	var cache programCache
	for i := range maxPrograms + 10 {
		if _, err := cache.compile(strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != maxPrograms {
		t.Fatalf("entries=%d", len(cache.entries))
	}
	var large programCache
	if _, err := large.compile("'" + strings.Repeat("a", maxProgramSource) + "'"); err != nil {
		t.Fatal(err)
	}
	if len(large.entries) != 0 {
		t.Fatal("oversized source cached")
	}
	if _, err := large.compile("function("); err == nil {
		t.Fatal("invalid syntax accepted")
	}
}

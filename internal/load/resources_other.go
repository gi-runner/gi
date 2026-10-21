//go:build !unix

package load

import (
	"runtime"
	"time"
)

func osUsage() (cpu time.Duration, rss uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return 0, m.HeapAlloc
}

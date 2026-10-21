//go:build unix

package load

import (
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

func osUsage() (cpu time.Duration, rss uint64) {
	var r unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &r); err != nil {
		return 0, 0
	}
	cpu = time.Duration(r.Utime.Nano() + r.Stime.Nano())
	if r.Maxrss > 0 {
		rss = uint64(r.Maxrss)
	}
	// Linux reports kilobytes. Darwin reports bytes.
	if runtime.GOOS == "linux" || runtime.GOOS == "android" {
		rss *= 1024
	}
	return cpu, rss
}

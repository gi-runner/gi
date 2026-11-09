package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/gi-runner/gi/internal/value"
)

// singleCache keeps callSingle results on disk across runs, as callSingleCache configures.
type singleCache struct {
	minutes int
	dir     string
}

func (c singleCache) wrap(key string, run func(context.Context) (any, error)) func(context.Context) (any, error) {
	if c.minutes <= 0 {
		return run
	}
	dir := c.dir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return run
		}
		dir = filepath.Join(base, "gi", "callSingle")
	}
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	return func(ctx context.Context) (any, error) {
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < time.Duration(c.minutes)*time.Minute {
			if b, err := os.ReadFile(path); err == nil {
				if v, err := value.Parse(b); err == nil {
					return v, nil
				}
			}
		}
		v, err := run(ctx)
		if err != nil {
			return v, err
		}
		if b, err := value.Marshal(v); err == nil && os.MkdirAll(dir, 0o750) == nil {
			_ = os.WriteFile(path, b, 0o600)
		}
		return v, nil
	}
}

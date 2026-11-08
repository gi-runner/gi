package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/load"
)

func applyShare(cfg *load.Config) error {
	raw := os.Getenv("GI_LOAD_SHARE")
	if raw == "" {
		return nil
	}
	index, n, err := parseShare(raw)
	if err != nil {
		return err
	}
	shared, err := load.Share(*cfg, index, n)
	if err != nil {
		return err
	}
	shared.PartPath = os.Getenv("GI_LOAD_PART")
	*cfg = shared
	return nil
}

func parseShare(raw string) (int, int, error) {
	indexText, countText, ok := strings.Cut(raw, "/")
	if !ok {
		return 0, 0, fmt.Errorf("injector share %q", raw)
	}
	index, err := strconv.Atoi(indexText)
	if err != nil {
		return 0, 0, fmt.Errorf("injector share %q", raw)
	}
	n, err := strconv.Atoi(countText)
	if err != nil {
		return 0, 0, fmt.Errorf("injector share %q", raw)
	}
	return index, n, nil
}

func waitLoadStart(ctx context.Context) error {
	raw := os.Getenv("GI_LOAD_START")
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("injector start %q", raw)
	}
	delay := time.Until(time.Unix(0, n))
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func launchInjectors(ctx context.Context, cfg load.Config, file string) (load.Result, error) {
	n := cfg.Injectors
	dir, err := os.MkdirTemp("", "gi-load-")
	if err != nil {
		return load.Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	exe, err := os.Executable()
	if err != nil {
		return load.Result{}, err
	}
	start := time.Now().Add(500 * time.Millisecond)
	snaps := make([]load.Snapshot, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			snaps[i], errs[i] = runInjector(ctx, exe, dir, file, start, i, n)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return load.Result{}, err
		}
	}
	return load.Merge(snaps, cfg)
}

func runInjector(ctx context.Context, exe, dir, file string, start time.Time, index, n int) (load.Snapshot, error) {
	path := filepath.Join(dir, strconv.Itoa(index)+".json")
	cmd := exec.CommandContext(ctx, exe, os.Args[1:]...) //nolint:gosec // Re-exec this process with the arguments it was given.
	cmd.Env = append(os.Environ(),
		"GI_LOAD_SHARE="+strconv.Itoa(index)+"/"+strconv.Itoa(n),
		"GI_LOAD_PART="+path,
		"GI_LOAD_FILE="+file,
		"GI_LOAD_START="+strconv.FormatInt(start.UnixNano(), 10),
	)
	out, err := cmd.CombinedOutput()
	snap, readErr := load.ReadSnapshot(path)
	if readErr != nil {
		if err == nil {
			err = readErr
		}
		return load.Snapshot{}, fmt.Errorf("injector %d of %d: %w\n%s", index+1, n, err, tail(out))
	}
	return snap, nil
}

func tail(out []byte) string {
	const maxTail = 4096
	if len(out) > maxTail {
		out = out[len(out)-maxTail:]
	}
	return string(out)
}

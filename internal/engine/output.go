package engine

import (
	"io"
	"sync"
)

type synchronizedWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

func synchronizeWriter(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	if _, ok := w.(*synchronizedWriter); ok {
		return w
	}
	return &synchronizedWriter{out: w}
}

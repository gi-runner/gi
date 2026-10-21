package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResponseBodyOwnershipAndLimit(t *testing.T) {
	payload := "first"
	sender := New(&http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})})
	send := func() (engine.Reply, error) {
		return sender.Send(context.Background(), engine.Call{Method: "GET", URL: "http://example.test"})
	}
	first, err := send()
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 16, 32767, 32768, 32769, 64 << 10, 8 << 20, (8 << 20) + 1} {
		payload = strings.Repeat("x", size)
		reply, err := send()
		if size > 8<<20 {
			if err == nil {
				t.Fatal("accepted oversized body")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply.Body, []byte(payload)) {
			t.Fatalf("incorrect body for size %d", size)
		}
		if string(first.Body) != "first" {
			t.Fatal("a later response mutated an earlier body")
		}
	}
}

func BenchmarkResponseRead(b *testing.B) {
	for _, size := range []int{32, 8192, 128 << 10, 1 << 20} {
		payload := bytes.Repeat([]byte("x"), size)
		for _, pooled := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/pool=%t", size, pooled), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if !pooled {
						reader := io.LimitReader(bytes.NewReader(payload), (8<<20)+1)
						if _, err := io.ReadAll(reader); err != nil {
							b.Fatal(err)
						}
						continue
					}
					body, err := readResponse(bytes.NewReader(payload), 8<<20)
					if err != nil {
						b.Fatal(err)
					}
					if len(body) != size {
						b.Fatal("short body")
					}

				}
			})
		}
	}
}

func TestResponseReadPropagatesFailure(t *testing.T) {
	failure := errors.New("broken response")
	for _, size := range []int{0, 16, 32768, 65536} {
		reader := io.MultiReader(strings.NewReader(strings.Repeat("x", size)), failingReader{failure})
		if _, err := readResponse(reader, 8<<20); !errors.Is(err, failure) {
			t.Fatalf("size %d: lost read error: %v", size, err)
		}
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

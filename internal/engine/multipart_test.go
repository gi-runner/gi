package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestMultipartAndCookies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /up", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, filename, fileType := "", "", ""
		if f, hdr, err := r.FormFile("note"); err == nil {
			b, _ := io.ReadAll(f)
			_ = f.Close()
			file = string(b)
			filename = hdr.Filename
			fileType = hdr.Header.Get("Content-Type")
		}
		raw, _ := json.Marshal(map[string]string{
			"name":     r.FormValue("name"),
			"city":     r.FormValue("city"),
			"file":     file,
			"filename": filename,
			"fileType": fileType,
			"type":     r.Header.Get("Content-Type"),
			"cookie":   r.Header.Get("Cookie"),
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `
Feature:

Scenario: upload
* url baseUrl
* path 'up'
* multipart field name = 'Ada'
* multipart fields { city: 'London' }
* multipart file note = { read: 'note.txt', filename: 'note.txt', contentType: 'text/plain' }
* cookies { session: 'abc' }
* method post
* match response.name == 'Ada'
* match response.city == 'London'
* match response.file == 'hello'
* match response.filename == 'note.txt'
* match response.fileType == 'text/plain'
* match response.type contains 'multipart/form-data'
* match response.cookie contains 'session=abc'
`
	f, err := feature.Parse(filepath.Join(dir, "up.feature"), src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		HTTP: httpx.New(srv.Client()),
		Vars: map[string]any{"baseUrl": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range res.Scenarios {
		if !sc.Passed {
			t.Fatalf("%s\n%s", sc.Name, sc.Error)
		}
	}
}

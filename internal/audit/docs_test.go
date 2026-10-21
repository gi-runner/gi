package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/testutil"
)

// Execute the actual introductory snippets so edits cannot silently break them.
func TestDocumentationExamples(t *testing.T) {
	root := filepath.Join("..", "..")
	quickstart := documentedFeatures(t, filepath.Join(root, "docs", "quickstart.md"))
	if len(quickstart) != 2 {
		t.Fatalf("quick start has %d complete features, want mock and test", len(quickstart))
	}
	mockFeature := featureOf(t, filepath.Join(t.TempDir(), "hello.mock.feature"), quickstart[0])
	hello := httptest.NewServer(mock.Handler(mockFeature))
	defer hello.Close()
	usersFeature, err := feature.ParseFile(filepath.Join(root, "examples", "users.mock.feature"))
	if err != nil {
		t.Fatal(err)
	}
	users := httptest.NewServer(mock.Handler(usersFeature))
	defer users.Close()
	graphql := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Query != "query User($id: ID!) { user(id: $id) { id name } }" || body.Variables["id"] != "42" {
			http.Error(w, "invalid GraphQL request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"user":{"id":"42","name":"Ada"}}}`)
	}))
	defer graphql.Close()
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			kind, message, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), kind, message); err != nil {
				return
			}
		}
	}))
	defer echo.Close()
	for _, tc := range []struct {
		name, source, url string
		jobs, scenarios   int
	}{
		{"quick start", quickstart[1], hello.URL, 1, 1},
		{"README", oneDocumentedFeature(t, filepath.Join(root, "README.md")), users.URL, 1, 1},
		{"GraphQL", documentedFeature(t, filepath.Join(root, "docs", "http.md"), "GraphQL users"), graphql.URL, 1, 1},
		{"events", documentedFeature(t, filepath.Join(root, "docs", "suite.md"), "scenario events"), "", 2, 2},
		{"HTTP", documentedFeature(t, filepath.Join(root, "docs", "http.md"), "HTTP greeting"), hello.URL, 1, 1},
		{"assertions", oneDocumentedFeature(t, filepath.Join(root, "docs", "match.md")), "", 1, 2},
		{"data", oneDocumentedFeature(t, filepath.Join(root, "docs", "data.md")), "", 1, 3},
		{"reuse", documentedFeature(t, filepath.Join(root, "docs", "suite.md"), "Reuse a flow"), "", 1, 1},
		{"language", oneDocumentedFeature(t, filepath.Join(root, "docs", "language.md")), "", 1, 1},
		{"helpers", oneDocumentedFeature(t, filepath.Join(root, "docs", "helpers.md")), "", 1, 1},
		{"WebSocket", oneDocumentedFeature(t, filepath.Join(root, "docs", "protocols.md")), "", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := featureOf(t, filepath.Join(t.TempDir(), "example.feature"), tc.source)
			if tc.name == "reuse" {
				helper := documentedFeature(t, filepath.Join(root, "docs", "suite.md"), "Greeting helper")
				if err := os.WriteFile(filepath.Join(filepath.Dir(f.Path), "greeting.feature"), []byte(helper), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sender := httpx.New(nil)
			defer sender.CloseIdleConnections()
			results, err := engine.RunFiles(t.Context(), []*feature.File{f}, engine.Options{
				HTTP: sender, Vars: map[string]any{"baseUrl": tc.url, "wsUrl": "ws" + strings.TrimPrefix(echo.URL, "http")}, Jobs: tc.jobs, Suite: suite.Open(f.Path, ""), Dial: adapters.Dial,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || !results[0].Passed() || len(results[0].Scenarios) != tc.scenarios {
				t.Fatalf("documented example failed: %+v", results)
			}
		})
	}
}

func documentedFeatures(t *testing.T, path string) []string {
	t.Helper()
	text := testutil.ReadMarkdown(t, path)
	var features []string
	for _, block := range strings.Split(text, "```gherkin\n")[1:] {
		source, _, ok := strings.Cut(block, "```")
		if !ok {
			t.Fatalf("unclosed Gherkin block in %s", path)
		}
		if strings.HasPrefix(source, "Feature:") {
			features = append(features, source)
		}
	}
	return features
}

func oneDocumentedFeature(t *testing.T, path string) string {
	t.Helper()
	features := documentedFeatures(t, path)
	if len(features) != 1 {
		t.Fatalf("%s has %d complete features, want 1", path, len(features))
	}
	return features[0]
}

func documentedFeature(t *testing.T, path, name string) string {
	t.Helper()
	for _, source := range documentedFeatures(t, path) {
		if strings.HasPrefix(source, "Feature: "+name+"\n") {
			return source
		}
	}
	t.Fatalf("%s has no feature named %q", path, name)
	return ""
}

func TestDocumentedMock(t *testing.T) {
	source := oneDocumentedFeature(t, filepath.Join("..", "..", "docs", "mock.md"))
	handler := mock.Handler(featureOf(t, filepath.Join(t.TempDir(), "users.mock.feature"), source))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/users", `{"name":"Ada"}`, http.StatusCreated},
		{http.MethodGet, "/users/1", "", http.StatusOK},
		{http.MethodPost, "/users/1", "", http.StatusNotFound},
	} {
		request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s %s: status %d: %s", tc.method, tc.path, response.Code, response.Body.String())
		}
		if tc.status != http.StatusNotFound {
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["id"] != "1" || body["name"] != "Ada" {
				t.Fatalf("unexpected body: %v", body)
			}
		}
	}
}

func TestDocumentedGoAPI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	quickstart := documentedFeatures(t, filepath.Join(root, "docs", "quickstart.md"))
	if len(quickstart) != 2 {
		t.Fatalf("quick start has %d complete features, want mock and test", len(quickstart))
	}
	server := httptest.NewServer(mock.Handler(featureOf(t, filepath.Join(t.TempDir(), "hello.mock.feature"), quickstart[0])))
	defer server.Close()
	text := testutil.ReadMarkdown(t, filepath.Join(root, "docs", "embedding.md"))
	_, block, ok := strings.Cut(text, "```go\n")
	if !ok {
		t.Fatal("no Go example in embedding docs")
	}
	source, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("unclosed Go example")
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": fmt.Sprintf(`module docs-example

go 1.27.2

require github.com/gi-runner/gi v0.0.0
replace github.com/gi-runner/gi => %s
`, strconv.Quote(root)),
		"hello_test.go": strings.ReplaceAll(source, "http://127.0.0.1:18081", server.URL),
		"hello.feature": quickstart[1],
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", "-run", "^TestHello$", ".")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("documented Go example: %v\n%s", err, out)
	}
}

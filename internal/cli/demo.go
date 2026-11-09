package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/suite"
)

const helloMock = `Feature: Hello API

Scenario: greet
  * methodIs GET
  * pathMatches '/hello'
  * def response = { message: 'Hello, Ada!' }
`

const helloFeature = `Feature: Hello API

Scenario: get a greeting
  * url baseUrl
  * path 'hello'
  * method get
  * status 200
  * match response == { message: 'Hello, Ada!' }
`

func runInit() int {
	files := []struct{ name, body string }{
		{"hello.mock.feature", helloMock},
		{"hello.feature", helloFeature},
	}
	for _, file := range files {
		if _, err := os.Stat(file.name); err == nil {
			fmt.Fprintf(os.Stderr, "%s already exists\n", file.name)
			return 2
		}
	}
	for _, file := range files {
		if err := os.WriteFile(file.name, []byte(file.body), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	fmt.Print(`Wrote hello.feature and hello.mock.feature.

  gi mock hello.mock.feature
  gi test hello.feature --var baseUrl=http://127.0.0.1:18080
  gi load hello.feature --var baseUrl=http://127.0.0.1:18080 --vus 10 --duration 10s
`)
	return 0
}

func runDemo() int {
	dir, err := os.MkdirTemp("", "gi-demo-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	mockPath := filepath.Join(dir, "hello.mock.feature")
	featurePath := filepath.Join(dir, "hello.feature")
	if err := os.WriteFile(mockPath, []byte(helloMock), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(featurePath, []byte(helloFeature), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv, err := mock.Start(ctx, engine.MockSpec{Path: mockPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = srv.Stop() }()
	fmt.Println("functional")
	if code := runTest([]string{featurePath}, testOptions{commonOptions: commonOptions{vars: []string{"baseUrl=" + srv.URL}}}); code != 0 { //nolint:modernize // eliding the embedded type does not typecheck
		return code
	}
	f, err := feature.ParseFile(featurePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("load")
	res, err := load.Run(ctx, f, load.Config{
		LoadSpec:  feature.LoadSpec{VUs: 4, Duration: 2 * time.Second}, //nolint:modernize // eliding the embedded type does not typecheck
		Vars:      map[string]any{"baseUrl": srv.URL},
		Timeout:   5 * time.Second,
		Suite:     suite.Open(featurePath, ""),
		Dial:      adapters.Dial,
		NewPage:   adapters.NewPage,
		StartMock: mock.Start,
	})
	printLoad(res)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !res.Passed {
		return 1
	}
	fmt.Println("demo passed")
	return 0
}

func runPlan(args []string, parts int, start string) int {
	if parts < 2 {
		fmt.Fprintln(os.Stderr, "--parts must be at least 2")
		return 2
	}
	files, err := discover(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if start == "" {
		start = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, start); err != nil {
		fmt.Fprintf(os.Stderr, "--start-at expects RFC 3339 time: %v\n", err)
		return 2
	}
	quoted := make([]string, len(files))
	for i, path := range files {
		quoted[i] = strconv.Quote(path)
	}
	line := strings.Join(quoted, " ")
	fmt.Println("# Run one line on each machine, then merge.")
	snaps := make([]string, parts)
	for i := 1; i <= parts; i++ {
		snaps[i-1] = fmt.Sprintf("part%d.json", i)
		fmt.Printf("gi load %s --part %d/%d --snapshot %s --start-at %s\n", line, i, parts, snaps[i-1], start)
	}
	fmt.Printf("gi load merge %s\n", strings.Join(snaps, " "))
	return 0
}

package gi_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gi-runner/gi"
)

func TestRegisteredHelper(t *testing.T) {
	if err := gi.Register("double", func(args []any) (any, error) {
		n, _ := args[0].(int64)
		return n * 2, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gi.Unregister("double") })
	if err := gi.Register("map", func([]any) (any, error) { return nil, os.ErrInvalid }); err == nil {
		t.Fatal("built-in name was registered")
	}
	path := filepath.Join(t.TempDir(), "helper.feature")
	if err := os.WriteFile(path, []byte("Feature: helper\nScenario: double\n* def n = gi.double(21)\n* match n == 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := gi.Run(t.Context(), []string{path}, gi.Options{})
	if err != nil || !results[0].Passed() {
		t.Fatalf("%+v %v", results, err)
	}
}

func TestGoStepsAndRunHelpers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steps.feature")
	body := "Feature: steps\nScenario: cart\nGiven a cart with 3 items\n* def total = karate.price(count)\n* match total == 30\n" +
		"When the cart holds\n  \"\"\"\n  apples\n  \"\"\"\nThen match held == 'apples'\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	opt := gi.Options{
		Steps: []gi.Step{
			{Pattern: `a cart with (\d+) items`, Run: func(_ context.Context, sc *gi.Scope, args []string) error {
				n, err := strconv.Atoi(args[0])
				sc.Set("count", n)
				return err
			}},
			{Pattern: `the cart holds`, Run: func(_ context.Context, sc *gi.Scope, _ []string) error {
				sc.Set("held", sc.DocString())
				return nil
			}},
		},
		Helpers: map[string]func([]any) (any, error){
			"price": func(args []any) (any, error) {
				n, _ := args[0].(int64)
				return n * 10, nil
			},
		},
	}
	results, err := gi.Run(t.Context(), []string{path}, opt)
	if err != nil || !results[0].Passed() {
		t.Fatalf("%+v %v", results, err)
	}
	if _, err := gi.Run(t.Context(), []string{path}, gi.Options{Steps: []gi.Step{{Pattern: "("}}}); err == nil {
		t.Fatal("accepted a step without Run")
	}
	if _, err := gi.Run(t.Context(), []string{path}, gi.Options{Helpers: map[string]func([]any) (any, error){"match": opt.Helpers["price"]}}); err == nil {
		t.Fatal("accepted a built-in helper name")
	}
}

func TestMockAndLoad(t *testing.T) {
	dir := t.TempDir()
	mockPath := filepath.Join(dir, "hello-mock.feature")
	if err := os.WriteFile(mockPath, []byte("Feature: hello\nScenario: pathMatches('/hello')\n* def response = { msg: greeting }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv, err := gi.Mock(t.Context(), mockPath, gi.MockOptions{Arg: map[string]any{"greeting": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	loadPath := filepath.Join(dir, "hello.feature")
	body := "Feature: hello\nScenario: get\nGiven the hello service\n* url base\n* path 'hello'\n* method get\n* status 200\n* match response.msg == 'hi'\n"
	if err := os.WriteFile(loadPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := gi.Load(t.Context(), loadPath, gi.LoadOptions{
		Steps: []gi.Step{{Pattern: "the hello service", Run: func(_ context.Context, sc *gi.Scope, _ []string) error {
			sc.Set("base", srv.URL)
			return nil
		}}},
		Stages:     []string{"users=2 duration=300ms"},
		Thresholds: []string{"errorRate < 1%"},
	})
	if err != nil || !res.Passed || res.Requests == 0 || res.FailedIterations > 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.feature")
	if err := os.WriteFile(path, []byte("Feature: public\nScenario: values\n* match supplied == 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := gi.Run(t.Context(), []string{path}, gi.Options{Vars: map[string]any{"supplied": 42}})
	if err != nil || len(results) != 1 || !results[0].Passed() {
		t.Fatalf("%+v %v", results, err)
	}
}

func TestCheckFindsJVMOnlyCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.feature")
	if err := os.WriteFile(path, []byte("Feature: legacy\n@parallel=false\nScenario: old\n* def x = Java.type('Foo')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	found, err := gi.Check([]string{path}, gi.Options{})
	if err != nil || len(found) < 2 {
		t.Fatalf("%+v %v", found, err)
	}
}

func TestScenariosBecomeSubtests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.feature")
	body := "Feature: orders\nScenario: create\n* match 1 == 1\nScenario: broken\n* match 1 == 2\n@ignore\nScenario: later\n* match 1 == 1\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GI_SUBTEST_HELPER") == "1" {
		gi.Test(t, gi.Options{}, os.Getenv("GI_SUBTEST_PATH"))
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestScenariosBecomeSubtests$", "-test.v")
	cmd.Env = append(os.Environ(), "GI_SUBTEST_HELPER=1", "GI_SUBTEST_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a failing scenario passed:\n%s", out)
	}
	for _, want := range []string{"--- PASS: TestScenariosBecomeSubtests/orders/create", "--- FAIL: TestScenariosBecomeSubtests/orders/broken", "--- SKIP: TestScenariosBecomeSubtests/orders/later", "match failed"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestParallelOutput(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, 2)
	for _, name := range []string{"one.feature", "two.feature"} {
		path := filepath.Join(dir, name)
		body := "Feature: output\nScenario: one\n* print 'one'\nScenario: two\n* print 'two'\n"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	var output bytes.Buffer
	result, err := gi.Run(t.Context(), paths, gi.Options{Jobs: 2, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || strings.Count(output.String(), "one") != 2 || strings.Count(output.String(), "two") != 2 {
		t.Fatalf("parallel output: %q (%+v)", output.String(), result)
	}
}

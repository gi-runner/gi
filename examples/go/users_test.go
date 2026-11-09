// Package users_test runs the users feature from go test: once against a mock,
// once under load, and once with steps written in Go.
package users_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gi-runner/gi"
)

func mockUsers(t *testing.T) string {
	t.Helper()
	srv, err := gi.Mock(t.Context(), "../users.mock.feature", gi.MockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv.URL
}

func TestUsers(t *testing.T) {
	gi.Test(t, gi.Options{Vars: map[string]any{"baseUrl": mockUsers(t)}}, "../users.feature")
}

func TestUsersUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load run")
	}
	res, err := gi.Load(t.Context(), "../users.feature", gi.LoadOptions{
		Vars:       map[string]any{"baseUrl": mockUsers(t)},
		Stages:     []string{"users=1:4 duration=1s"},
		Thresholds: []string{"p95 < 200ms"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Fatalf("thresholds failed: %+v", res.Thresholds)
	}
	t.Logf("%d requests, p95 %s", res.Requests, res.Overall.P95)
}

// Plain-language steps defined in Go mix with the built-in HTTP keywords,
// so no Go code is needed for requests or assertions.
const signup = `Feature: signup
  Scenario: a new user signs up
    Given a user named "Ada" aged 36
    When url baseUrl
    And path 'users'
    And request user
    And method post
    Then status 201
    And match response contains user
`

func TestStepsInGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signup.feature")
	if err := os.WriteFile(path, []byte(signup), 0o600); err != nil {
		t.Fatal(err)
	}
	gi.Test(t, gi.Options{
		Vars: map[string]any{"baseUrl": mockUsers(t)},
		Steps: []gi.Step{{Pattern: `a user named {string} aged {int}`, Run: func(_ context.Context, sc *gi.Scope, args []string) error {
			age, err := strconv.Atoi(args[1])
			sc.Set("user", map[string]any{"name": args[0], "age": age, "email": args[0] + "@example.com"})
			return err
		}}},
	}, path)
}

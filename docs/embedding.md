# Run gi from Go

Import `github.com/gi-runner/gi` and call `Test` or `Run` with explicit feature paths. The API uses the same runner and native adapters as the CLI. `Register` adds a Go function to the `gi` object for later runs; `Unregister` removes it.

## Try it

In a new directory, run `go mod init example.com/api-tests` first. In your Go module, add the dependency with `go get github.com/gi-runner/gi`. Copy `hello.feature` from the [quick start](quickstart.md), keep its mock running, and save this as `hello_test.go`:

```go
package apitest

import (
    "testing"
    "time"

    "github.com/gi-runner/gi"
)

func TestHello(t *testing.T) {
    gi.Test(t, gi.Options{
        Vars:    map[string]any{"baseUrl": "http://127.0.0.1:18081"},
        Timeout: 5 * time.Second,
    }, "hello.feature")
}
```

Run `go test -v`. Each scenario is a subtest named `feature/scenario`, such as `TestHello/Hello_API/get_a_greeting`, so `go test` reports each pass, failure, and skip on its own line. A failed scenario fails its subtest with the step error and match diff. Scenario output goes to the test log.

## Results and options

`gi.Test` is `Run` plus reporting. Use `Run` when you need the results themselves.

`Run` returns an error for a parse failure, a failed expansion, cancellation, or a selection that ran no scenarios. Assertion and setup failures are on the results. Check `Passed()` when the error is nil. `ScenarioResult`, `StepResult`, and `Exchange` are in this package.

`Options` also accepts `Env`, `Tags`, `Client`, `Output`, `Steps`, and `Helpers`.

- `Jobs` of 0 uses `GOMAXPROCS`.
- `Timeout` bounds one network or browser operation. The context deadline bounds the run, including JavaScript.
- `Client` supplies transport or TLS. The caller closes it. Each scenario has its own cookie jar.
- `Output` is synchronized for one `Run`. A writer shared by concurrent runs is the caller's to synchronize.
- `Vars` holds data values.
- `Helpers` adds functions to the `karate` object for this run only.
- `Steps` runs Go for step text no built-in keyword claims. The pattern must match the whole text. A regular expression's groups become args. `{string}`, `{int}`, `{float}`, and `{word}` are cucumber parameters. `{string}` is a double-quoted value, and the argument does not include the quotes.

`gi.Mock` starts a mock feature and returns `{ URL, Port, Close }`. `gi.Load` runs one feature under its load profile or `LoadOptions.Stages`. `gi.Check` reports JVM-only calls and unknown steps without running them.

Paths are files. Configuration is read from the first path. Packages under `internal` are not an extension API.

`ssl`, `proxy` and `connectTimeout` require a client using `*http.Transport`. gi clones the transport settings and reuses connections. For scope and cancellation rules when calling features, see [Reuse and configuration](suite.md#call).

## Register a helper

```go
if err := gi.Register("greet", func(args []any) (any, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("greet expects one argument")
    }
    return fmt.Sprintf("Hello, %v!", args[0]), nil
}); err != nil {
    return err
}
defer gi.Unregister("greet")
```

A later scenario can use `* def message = gi.greet('Ada')`. Registration is process-wide; each scenario snapshots the registered functions. Prefer `Options.Helpers` when two tests need different helpers. Built-in names are rejected. Helpers may run concurrently, so synchronize any captured mutable state. This API registers functions, not protocol providers.

## Common mistakes

| Symptom | Check |
| --- | --- |
| A failed scenario does not fail your Go test | Use `gi.Test`, or check every result's `Passed()` as well as the returned error. |
| A directory path fails | `gi.Run` accepts explicit feature files. Directory discovery belongs to the CLI. |
| A registered helper races under parallel runs | Synchronize mutable state captured by the helper, or pass `Options.Helpers` so the helper stays on that run. |
| A custom transport rejects TLS configuration | The `ssl`, `proxy` and `connectTimeout` settings require an `*http.Transport`. |

# Run gi from Go

Import `github.com/gi-runner/gi` and call `Run` with explicit feature paths. The API uses the same runner and native adapters as the CLI. `Register` adds a Go function to the `gi` object for later runs; `Unregister` removes it.

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
    results, err := gi.Run(t.Context(), []string{"hello.feature"}, gi.Options{
        Vars: map[string]any{"baseUrl": "http://127.0.0.1:18081"},
        Jobs: 1,
        Timeout: 5 * time.Second,
    })
    if err != nil {
        t.Fatal(err)
    }
    for _, file := range results {
        if !file.Passed() {
            t.Fatalf("feature failed: %s: %+v", file.Path, file.Scenarios)
        }
    }
}
```

Run `go test -v`. `TestHello` passes when the mock returns the expected greeting. A network or assertion failure fails the Go test with the scenario results.

## Results and options

`Run` returns an error for a parse failure, a failed expansion, cancellation, or a selection that ran no scenarios. Assertion and setup failures are on the results. Check `Passed()` when the error is nil. `ScenarioResult`, `StepResult`, and `Exchange` are in this package.

`Options` also accepts `Env`, `Tags`, `Client`, and `Output`.

- `Jobs` of 0 uses `GOMAXPROCS`.
- `Timeout` bounds one network or browser operation. The context deadline bounds the run, including JavaScript.
- `Client` supplies transport or TLS. The caller closes it. Each scenario has its own cookie jar.
- `Output` is synchronized for one `Run`. A writer shared by concurrent runs is the caller's to synchronize.
- `Vars` holds data values.

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

A later scenario can use `* def message = gi.greet('Ada')`. Registration is process-wide; each scenario snapshots the registered functions. Built-in names are rejected. Helpers may run concurrently, so synchronize any captured mutable state. This API registers functions, not protocol providers.

## Common mistakes

| Symptom | Check |
| --- | --- |
| A failed scenario does not fail your Go test | Check every result's `Passed()` as well as the returned error. |
| A directory path fails | `gi.Run` accepts explicit feature files. Directory discovery belongs to the CLI. |
| A registered helper races under parallel runs | Synchronize mutable state captured by the helper. Registration is process-wide. |
| A custom transport rejects TLS configuration | The `ssl`, `proxy` and `connectTimeout` settings require an `*http.Transport`. |

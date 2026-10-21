# Embedding

Import `github.com/gi-runner/gi` and call `Run` with explicit feature paths. The API uses the same runner and native adapters as the CLI. `Register` adds a Go function to the `gi` object for later runs; `Unregister` removes it.

```go
results, err := gi.Run(ctx, []string{"features/users.feature"}, gi.Options{
    Vars: map[string]any{"baseUrl": "https://api.example.com"},
    Jobs: 4,
    Timeout: 5 * time.Second,
})
if err != nil {
    return err
}
for _, file := range results {
    if !file.Passed() {
        return fmt.Errorf("feature failed: %s", file.Path)
    }
}
```

`Run` returns an error for a parse failure, a failed expansion, cancellation, or a selection that ran no scenarios. Assertion and setup failures are on the results. Check `Passed()` when the error is nil. `ScenarioResult`, `StepResult`, and `Exchange` are in this package.

`Options` also accepts `Env`, `Tags`, `Client`, and `Output`.

- `Jobs` of 0 uses `GOMAXPROCS`.
- `Timeout` bounds one network or browser operation. The context deadline bounds the run, including JavaScript.
- `Client` supplies transport or TLS. The caller closes it. Each scenario has its own cookie jar.
- `Output` is synchronized for one `Run`. A writer shared by concurrent runs is the caller's to synchronize.
- `Vars` holds data values.

Paths are files. Configuration is read from the first path. Packages under `internal` are not an extension API.

`ssl`, `proxy`, and `connectTimeout` apply when the client's transport is an `*http.Transport`. gi clones it and keeps the connection pool. An assigned call and `setup` run in a new JavaScript realm seeded from the caller. The caller's bindings and built-ins stay as they were. A function returned from the call keeps the callee realm. A function from the caller remains callable and stops if that call's deadline fires.

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

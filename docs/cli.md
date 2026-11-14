# Command line

```text
gi test [files or directories] [flags]
gi load [files or directories] [flags]
gi load merge <snapshots> [flags]
gi mock <file.feature> [flags]
gi spec [spec directory]
gi check [files or directories]
gi demo
gi init
gi version
gi completion {bash|zsh|fish|powershell}
```

`gi test` without a path searches `features/`, then `examples/`, then the current directory. It uses the first of those directories that exists. A directory argument includes `.feature` files below it, except hidden directories, `vendor/`, and `*.mock.feature`. Pass a file to run only that file, including a mock file.

Exit status is 0 when the run passed, 1 when a scenario, a threshold, or a file failed, and 2 when the command line is wrong.

Use `gi <command> --help` for command options. Shell completion is available through
`gi completion bash`, `gi completion zsh`, `gi completion fish`, or `gi completion powershell`.

Flags may follow the file path. Use `--` before paths beginning with `-`. `gi test users.feature --var baseUrl=http://127.0.0.1:18080` is valid.

## test

| Flag | Meaning |
| --- | --- |
| `--env name` | Loads `gi-config-<env>.js` and selects `@env` tags. |
| `--var key=value` | A variable. Repeat the flag. Wins over config. |
| `--tag expr` | Tag expression. See [Suite](suite.md). |
| `--jobs n` | Features in parallel. Default is the CPU count, or 1 for a single `# gi-spec` file. `1` keeps order. See [Suite](suite.md). |
| `--timeout 30s` | Per-request timeout. |
| `--report file.html` | HTML report. |
| `--junit file.xml` | JUnit XML. |
| `--json file.json` | JSON report. |
| `--cucumber file.json` | Cucumber JSON. |
| `--otel-endpoint url` | OTLP/HTTP JSON trace. |

## load

`test` flags except `--jobs`, plus:

| Flag | Meaning |
| --- | --- |
| `--vus n` | Virtual users, each looping the scenario. Default 10. |
| `--vus-to n` | Ramp concurrent users from `--vus` to n across warmup and the window. |
| `--rate n` | Scenario starts per second. Earlier starts keep running. |
| `--rate-to n` | Ramp the start rate from `--rate` to n. |
| `--stage spec` | Repeatable segment, such as `users=1:40 duration=30s` or `spike rate=500 duration=10s`. |
| `--max-inflight n` | Open-loop cap. A start past the cap is a missed start. |
| `--max-endpoints n` | Named endpoint limit (default 1,000). Further paths aggregate under `[other endpoints]`. |
| `--duration 10s` | Measured window. Stages carry their own durations. |
| `--warmup 0s` | Unmeasured time before the window. |
| `--rps n` | Cap on HTTP calls. `0` means no cap. |
| `--threshold 'p95 < 200ms'` | Repeatable. Also `errorRate`, `rps`, `schedP95`, `missed`, and `late`. A call site name follows the value. See [Load](load.md). |
| `--part 2/4` | Run share 2 of 4 of the profile on this machine. Thresholds are judged by `gi load merge`. |
| `--snapshot part2.json` | Write the run's histograms for `gi load merge`. Takes one feature file. |
| `--start-at 2026-11-08T16:00:00Z` | Wait until this RFC 3339 time, so every part starts together. |
| `--metrics live.jsonl` | Stream one JSON line per second: `time`, `feature`, `elapsed` seconds, `users`, cumulative `requests` and `failed`, and that second's `rps`. |
| `--metrics-listen :9090` | Serve Prometheus text at `/metrics` for the length of the run. |
| `--profile spike` | Run one named `gi-load` profile. Omit it and every profile in the file starts together. |

`gi load merge part*.json` joins the snapshots of every part, prints the summary, and judges the recorded thresholds plus any `--threshold`. It accepts `--report`, `--json`, and `--junit`, and fails when a part is missing, repeated, or from another profile.

`gi load plan users.feature --parts 4` prints one command per machine and the merge command. `--start-at` defaults to one minute from now. `--parts` must be at least 2.

## demo and init

`gi demo` runs a built-in greeting as a functional test and a two-second load test. `gi init` writes that greeting into the current directory as `hello.feature` and `hello.mock.feature`, and refuses to overwrite either file.

## mock

| Flag | Meaning |
| --- | --- |
| `--addr :18080` | Listen address. |

## spec

`gi spec specs/001-payment` reads `spec.md` or `requirements.md`, plus `openapi.yaml` or `contracts/*.yaml`, and writes `features/*.feature`. `--json` prints the files and notes. The human report ends with `open:` for a requirement the run did not prove. `--out` sets the feature directory. Spec Kit, Kiro, and OpenSpec use the same command. Run `gi check` on that directory, then `gi test` with `--var baseUrl=...`. See [Specs](spec.md).

## check

`gi check` reports steps, configure keys, and tags gi cannot run, without running anything. It flags JVM-only calls such as `Java.type`, unknown keywords, and `@parallel=false` (use `@lock` instead). Exit status is 1 when it finds any, 0 when the files are clean, and 2 when the path is wrong.

## Version

`gi version`, `gi --version`, and `gi -v` print `gi` and the version.

## Common mistakes

| Symptom | Check |
| --- | --- |
| `no .feature files found` | Supply the feature path or a directory containing features. |
| `no scenarios ran` | Check tags and environment selection. A selection with no runnable scenarios fails. |
| A path beginning with `-` is parsed as a flag | Put `--` before the path, after all flags. |
| A variable value contains commas | Use `--var 'names=Ada,Grace'`. Repeat `--var` for different variables. |
| A threshold is split into separate arguments | Quote it: `--threshold 'p95 < 200ms'`. |

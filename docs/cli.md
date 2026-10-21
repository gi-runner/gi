# Command line

```text
gi test [files or directories] [flags]
gi load [files or directories] [flags]
gi mock <file.feature> [flags]
gi version
```

Exit status is 0 when the run passed, 1 when a scenario, a threshold, or a file failed, and 2 when the command line is wrong.

Flags may follow the file path. `gi test users.feature --var baseUrl=http://127.0.0.1:18080` is valid.

## test

| Flag | Meaning |
| --- | --- |
| `--env name` | Loads `gi-config-<env>.js` and selects `@env` tags. |
| `--var key=value` | A variable. Repeat the flag. Wins over config. |
| `--tag expr` | Tag expression. See [Suite](suite.md). |
| `--jobs n` | Features in parallel. Default is the CPU count. `1` keeps order. See [Suite](suite.md). |
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
| `--duration 10s` | Measured window. Stages carry their own durations. |
| `--warmup 0s` | Unmeasured time before the window. |
| `--rps n` | Cap on HTTP calls. `0` means no cap. |
| `--threshold 'p95 < 200ms'` | Repeatable. Also `errorRate` and `rps`. |

## mock

| Flag | Meaning |
| --- | --- |
| `--addr :18080` | Listen address. |

## Version

`gi version` prints `gi` and the version.

# Load testing

`gi load` runs the same scenarios as `gi test`. A scenario tagged `@noload` is skipped. `@ignore` is skipped in both modes.

## Try it

Use `hello.feature` and the running mock from the [quick start](quickstart.md), then run:

```bash
gi load hello.feature --var baseUrl=http://127.0.0.1:18081 \
  --rate 20 --duration 5s --max-inflight 20 \
  --threshold 'p95 < 500ms' --threshold 'errorRate < 1%' \
  --report load.html
```

The runner schedules 20 scenario starts per second for five seconds. On a terminal, stderr redraws one line with elapsed time, users, requests, that second's rps, and failed. Piped or redirected output stays quiet. The summary shows iterations, requests, HTTP latency, missed starts, and a pass/fail result for each threshold. Open `load.html` for the same measurements. Counts and latency depend on the machine and service; work still in flight at the end is cancelled.

Exit status `0` means the run and thresholds passed. A failed assertion or threshold exits `1`. Invalid profile options exit `2`.

## Choose a profile

Choose what you want to control:

- **Users (`--vus`):** each virtual user finishes a scenario, then starts it again. A slower service reduces throughput. This is a closed-loop test.
- **Starts per second (`--rate`):** gi schedules scenarios at that rate even when earlier ones are still running. A slower service increases concurrent work. This is an open-loop test.

| Profile | What is controlled | Comment |
| --- | --- | --- |
| Constant users | A fixed population, each looping the scenario. This is the default. | `vus=10` |
| Ramp users | The population grows or shrinks linearly from `vus` to `vusTo`. | `vus=1 vusTo=40` |
| Constant rate | Scenario starts per second. | `rate=50` |
| Ramp rate | The start rate moves linearly from `rate` to `rateTo`. | `rate=10 rateTo=200` |

A user profile and a rate profile cannot be combined. Flags override the comments for the profile they set.

```gherkin
# gi-load: vus=10 duration=10s warmup=2s rps=50
# gi-load: threshold p95 < 500ms
# gi-load: threshold errorRate < 1%
```

`# gi-load: vu=10` is the same as `vus=10`.

### Named profiles

`name=` starts another profile in the same file. `gi load` runs each profile to completion, then the next. `--profile spike` runs one. A scenario with no `@load` tag runs in every profile. `@load=spike` runs only in that profile.

```gherkin
# gi-load: name=soak vus=5 duration=30s
# gi-load: name=spike rate=20 duration=5s

Scenario: browse
  * method get

@load=spike
Scenario: checkout
  * method post
```

Add `abort` to a threshold to stop the window once that condition has already failed: `--threshold 'errorRate < 1% abort'`. Other thresholds are still judged when the window ends.

`gi load plan orders.feature --parts 4` prints the `--part` commands and the matching `gi load merge`. A Grafana dashboard for the Prometheus metrics is `examples/grafana/gi-load.json`. `--otel-endpoint` that ends in `/v1/traces` also receives load gauges at the sibling `/v1/metrics` path (`gi.http.reqs`, `gi.http.req_failed`, `gi.http.req_rate`, `gi.http.p95`).

### Scenario mix

By default one iteration runs every selected scenario in the file. Tag scenarios with `@weight=n` and each iteration runs one scenario instead, in proportion to its weight. A selected scenario without the tag counts as weight 1, `@weight=0` keeps it out of load, and `gi test` ignores the tag.

```gherkin
@weight=8
Scenario: browse
  ...
@weight=2
Scenario: checkout
  ...
```

The order is interleaved and exact: every 10 iterations hold 8 browses and 2 checkouts. An outline is one weighted unit, so all its rows run together.

For example, ramp the quick-start test from 1 to 20 users:

```bash
gi load hello.feature --var baseUrl=http://127.0.0.1:18081 \
  --vus 1 --vus-to 20 --duration 30s
```

### Stages

Use several stages to ramp, hold, then reduce the load:

```bash
gi load hello.feature --var baseUrl=http://127.0.0.1:18081 \
  --stage 'users=1:20 duration=10s' \
  --stage 'users=20 duration=30s' \
  --stage 'users=20:1 duration=10s'
```

With no comment and no flag, the run is 10 virtual users for 10 seconds. `--rps` caps HTTP calls. Leave it unset and each call is sent when the scenario reaches it. Spare capacity under the cap is not saved, so `--rps` is a ceiling, not a promised rate. `--rate` is the target scenario start rate; capacity limits can cause missed starts.

Repeat `--stage` for each segment. Use `users=10` or `rate=50` to hold a level; `users=1:40` or `rate=10:200` ramps it. `spike users=200` jumps for that stage, then returns to the previous level. Every stage needs a duration, and all stages must use users or all must use rates. Stages replace the simple user/rate/duration flags.

`--warmup` holds the opening level and discards those samples. The stages are the measured window.

### Several machines

One process already uses every core, so add machines, not processes. Give each machine the same profile and its own part:

```bash
# on each of four machines, i = 1..4
gi load orders.feature --vus 400 --duration 5m --part $i/4 \
  --start-at 2026-11-08T16:00:00Z --snapshot part$i.json
# anywhere, after copying the snapshots
gi load merge part*.json --report load.html
```

Virtual users, the start rate, `--rps`, and `--max-inflight` are split so the parts sum to the profile. Each part runs `@setup` itself. The merge joins the histograms, so percentiles and call-site thresholds are judged on the whole run, not averaged. A part needs at least one user and a share of each cap.

## Read the summary

| Metric | Meaning |
| --- | --- |
| Iterations | Completed scenario iterations plus missed starts. One scenario can make several HTTP requests. |
| Requests / rps | HTTP calls completed and their measured rate. |
| p95 | 95% of measured HTTP calls completed within approximately this time. |
| Scheduled p95 | Time from a start being due until its HTTP response; includes starting late and earlier scenario work. |
| Missed | Starts skipped because the `--max-inflight` cap was reached. |
| Late | Accepted starts that began more than 20 ms after they were due. |

`--max-inflight` limits concurrent scenarios in a rate test. A missed start counts as a failed iteration. A late start is not automatically a failure; add a `late` threshold if lateness should fail the run.

??? note "Window boundaries and runner resources"

    Work still in flight at the window boundary is cancelled. It is not counted as a completed iteration or missed start; an assertion failure recorded earlier in that iteration remains a failure. A start accepted but not begun before the window ends is also excluded. Missed and late starts are separate counts.

    The runner reports user plus system CPU time, peak goroutines, and process maximum resident memory when the OS provides it. Otherwise the memory figure is sampled peak live heap usage.

## What counts as an error

A connection failure is a transport error. A failed assertion fails that iteration. `errorRate` is failed iterations divided by iterations. A scenario that expects HTTP 400 is not an error when the assertion passes.

Login once in `@setup`, then read it with `gi.setupOnce()`. A login inside the scenario runs on every iteration.

## Metrics

Each HTTP call is stored under its feature file, line, and path template. UUIDs and numeric segments collapse to `{id}`, so `users.feature:14 GET /users/550e8400-e29b-41d4-a716-446655440000` becomes `users.feature:14 GET /users/{id}`. The same path on another line is its own row.

The measured window is split into 30 slices. Each slice reports the requests in it, transport errors, mean and p95 HTTP latency, and the most users in flight. The HTML and JSON reports include the slices.

`--metrics live.jsonl` writes one JSON line per second while the run is in progress. `--metrics-listen :9090` serves Prometheus text at `/metrics` (`gi_vus`, `gi_http_reqs`, `gi_http_req_failed`, `gi_http_req_rate`) until the run ends.

By default, a run keeps 1,000 named endpoints. Set `--max-endpoints 2000` or `# gi-load: maxEndpoints=2000` to change the limit. Further paths aggregate under `[other endpoints]`; counts, errors, and overall latency still include every request. Reports show the number of requests aggregated.

### Thresholds

A threshold is a pass condition, such as `--threshold 'p95 < 200ms'`. All configured thresholds must pass.

| Threshold | Compares |
| --- | --- |
| `p50` `p95` `p99` `mean` `min` `max` | A duration: `200ms`, `1s`. |
| `errorRate` | A percent: `1%`. |
| `rps` | Requests per second. |
| `schedP50` `schedP95` `schedP99` `schedMean` `schedMin` `schedMax` | Time from a scheduled start until the HTTP response is recorded. Includes schedule lag and earlier scenario work, not HTTP latency alone. |
| `missed` `late` | Missed starts, and starts more than 20ms late. |

Operators are `<`, `>`, `<=`, and `>=`. A failed threshold fails the run. Thresholds reject negative and non-finite values. JUnit records failed iterations even when all configured thresholds pass.

Name a call site after the value to judge that call alone: `p95 < 200ms users.feature:14 GET /users/{id}`. On a call site, `errorRate` is transport errors divided by requests to that call, and `rps` is that call's rate. `schedP95`, `missed`, and `late` stay on the whole run. A named call that received no requests fails the threshold.

??? note "Percentile precision"

    Percentiles use a bounded histogram with 32 buckets per power of two. The reported upper edge is at most 3.125% above the nearest-rank value (exact below 32 ns), capped by the slowest sample. This makes latency thresholds fail closed.

## Unique test data

`vu` identifies the virtual user and `iter` counts its iterations, both starting at 1. A restarted user keeps its iteration count. In rate tests, each start gets a new `vu` and `iter` stays 1. Both values are 1 in a functional test.

Use them to create distinct test data:

```gherkin
* def email = 'user-' + vu + '-' + iter + '@example.com'
```

## Common mistakes

| Symptom | Check |
| --- | --- |
| gi rejects `--vus` with `--rate` | Choose a user profile or an arrival-rate profile. They cannot be combined. |
| gi rejects `--stage` with `--duration` | Each stage carries its own duration. Stages replace the simple profile. |
| The run misses starts | Check service latency, runner capacity, and `--max-inflight`. Increasing the cap can increase pressure on the service. |
| `schedP95` exceeds HTTP `p95` | Scheduled latency also includes waiting to start and scenario work before the request. |
| Login dominates each iteration | Move reusable authentication to `@setup` and call `gi.setupOnce()`. |
| Many paths appear as `[other endpoints]` | Review dynamic URL segments and the `--max-endpoints` limit. Overall counts still include those requests. |

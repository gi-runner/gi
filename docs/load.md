# Load

`gi load` runs the same scenarios as `gi test`. A scenario tagged `@noload` is skipped. `@ignore` is skipped in both modes.

The profile is either a population of virtual users or a start rate. Users are a closed loop: a user finishes an iteration, then starts the next, so concurrency stays on the profile. A start rate is an open loop: the next scenario starts when it is due, whether or not earlier ones have finished. In-flight work is then an outcome, not the control.

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

```bash
gi load examples/users.feature \
  --var baseUrl=http://127.0.0.1:18080 \
  --vus 20 --duration 10s --warmup 2s \
  --threshold 'p95 < 200ms'

gi load examples/users.feature --rate 50 --duration 30s
gi load examples/users.feature --vus 1 --vus-to 40 --duration 30s
gi load examples/users.feature --rate 10 --rate-to 200 --duration 30s
gi load examples/users.feature \
  --stage 'users=1:40 duration=30s' \
  --stage 'users=40 duration=1m' \
  --stage 'spike users=200 duration=10s' \
  --stage 'users=40 duration=30s'
```

With no comment and no flag, the run is 10 virtual users for 10 seconds. `--rps` caps HTTP calls. Leave it unset and each call is sent when the scenario reaches it. Spare capacity under the cap is not saved, so `--rps` is a ceiling, not a promised rate. `--rate` is the promised start rate.

`--stage` repeats. A stage is `users=10`, `users=1:40`, `rate=50`, or `rate=10:200`, each with `duration`. `spike users=200` or `spike rate=500` jumps to that level for the stage, then returns to the level before it. The next stage is the recovery. Stages are all users or all rates. They replace `--vus`, `--rate`, and `--duration`.

`--warmup` holds the opening level and discards those samples. The stages are the measured window.

`--max-inflight` caps an open loop. A start that would pass the cap is not run. It counts as a missed start and a failed iteration. A start that begins more than 20ms after it was due counts as late. `schedP95` is the time from the scheduled start to the response, so a stall inside the runner shows up there. `p95` stays the HTTP call itself.

The summary also reports the runner's CPU time, the process max RSS, and the peak number of goroutines.

Under a user profile, `vu` is that user and `iter` counts its loops, both starting at 1. If a stage removes and later restarts a user, its iteration count continues. Under a rate profile, each start gets the next `vu` and `iter` stays 1. A user run reports the larger end of the population. A rate run reports the peak number of scenarios in flight.

## What counts as an error

A connection failure is a transport error. A failed assertion fails that iteration. `errorRate` is failed iterations divided by iterations. A scenario that expects HTTP 400 is not an error when the assertion passes.

Login once in `@setup`, then read it with `gi.setupOnce()`. A login inside the scenario runs on every iteration.

## Metrics

Each HTTP call is stored under a path template. UUIDs and numeric segments collapse to `{id}`, so `GET /users/550e8400-e29b-41d4-a716-446655440000` becomes `GET /users/{id}`.

The summary reports count, throughput, min, mean, p50, p95, and p99. Percentiles come from a fixed histogram with 32 buckets per power of two, so a long run stays bounded. The bucket upper bound is at most 3.125% above the nearest-rank value (exact below 32 ns). A percentile is the upper edge of its bucket, capped by the slowest sample, which makes a latency threshold fail closed. An in-flight operation cancelled at the window boundary does not create a failed iteration. An assertion failure completed earlier in that same iteration remains a failure.

| Threshold | Compares |
| --- | --- |
| `p50` `p95` `p99` `mean` `min` `max` | A duration: `200ms`, `1s`. |
| `errorRate` | A percent: `1%`. |
| `rps` | Requests per second. |
| `schedP50` `schedP95` `schedP99` `schedMean` `schedMin` `schedMax` | Time from a scheduled start to the response. |
| `missed` `late` | Missed starts, and starts more than 20ms late. |

Operators are `<`, `>`, `<=`, and `>=`. A failed threshold fails the run. Thresholds reject negative and non-finite values. JUnit records failed iterations even when all configured thresholds pass.

`vu` and `iter` are set, so a scenario can build unique data:

```gherkin
* def email = 'user-' + vu + '-' + iter + '@example.com'
```

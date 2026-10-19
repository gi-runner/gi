# Load

`gi load` runs the same scenarios as `gi test`, under a closed model of virtual users. Each user loops until the window ends. A scenario tagged `@noload` is skipped. `@ignore` is skipped in both modes.

Declare the run on the feature. Flags override the comments.

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
```

With no comment and no flag, the run is 10 virtual users for 10 seconds. `--rps` caps request rate with a token bucket. Leave it unset and each user sends the next request when the previous one returns.

`--warmup` discards samples from the opening interval. The duration is the measured window after warmup.

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

Operators are `<`, `>`, `<=`, and `>=`. A failed threshold fails the run. Thresholds reject negative and non-finite values. JUnit records failed iterations even when all configured thresholds pass.

`vu` and `iter` are set, so a scenario can build unique data:

```gherkin
* def email = 'user-' + vu + '-' + iter + '@example.com'
```

> Historical checkpoint. Superseded by [PARITY-RELEASE.md](PARITY-RELEASE.md) for the subsequent native parity work and verification.

# Quality and release follow-up

## Verdict

The original 17 audit regressions pass, and the final full race suite, lint, vet, path fuzzing, no-CGO CLI build, and binary smoke test pass. The account approval limit that interrupted the earlier checkpoint no longer blocks verification.

The implementation is materially stronger in ownership, cancellation, error reporting, shared execution, and public contracts. It is a candidate for a release scoped to the documented native feature subset. **It still does not satisfy a full Java Karate parity release.** XML/XPath, the broader Driver API, full config/call/lifecycle semantics, and several extensions remain unimplemented. See the current [compatibility contract](../../docs/compatibility.md).

“Staff Go Engineer quality” is not a certification provided by lint or any particular Go author. The concrete defects discussed below have been repaired and tested. A blanket claim that the entire repository is beautiful, optimal, or bug-free would exceed this evidence.

## What changed in this continuation

### Calls and caches

- Shared feature calls now return persistent HTTP settings and changed JSON data to the caller; assigned calls retain isolation. Repeated cached shared calls replay those settings without sharing a scenario cookie jar.
- Removed duplicate redirect/auth inheritance flags; one session-copy path owns inheritance and clears transient request builders.
- Added the explicit shared form of `karate.call`, same-file tag selectors, stored `read('child.feature')` references, JS object targets, singleton JS arguments, and separate singleton keys using `?suffix`.
- Feature calls reject invalid argument shapes and empty selector matches. Dynamic child examples receive call argument metadata before expansion.
- `callonce` uses the first argument for a calling feature/target rather than creating a different cache entry for every argument.
- Cache publication and reads detach JSON data. Runtime-owned functions and handles cannot enter shared caches. Zero-value suites initialize their maps safely.
- Cache callbacks receive a derived context carrying dependency ownership. A graph detects cycles between parallel flights, which a local call stack alone cannot detect. A panicking owner still publishes an error to waiting callers and preserves the original panic.

### Correctness and operational behavior

- Excluded dynamic outlines no longer execute setup or other expressions before tag/environment selection.
- JSON parsing consumes the complete document; trailing values or garbage fail.
- Integer conversion rejects fractions, non-finite numbers, and overflow. Float normalization no longer wraps positive `2^63` into a negative integer. Retry/timeout values are validated before duration multiplication.
- Byte normalization copies mutable input rather than retaining caller-owned storage.
- Header functions retain repeated values and null-removal semantics.
- Sort key callbacks run once per item; distinct compares object structure independently of key insertion order; `filterKeys` accepts its common overloads; `appendTo` mutates its target list as in the Java baseline.
- Parallel output is serialized for ordinary `io.Writer` implementations. Output errors are surfaced. Worker concurrency is capped by the number of jobs.
- Config read errors are surfaced, and root discovery handles a not-yet-created feature path correctly.
- Scenario results distinguish cancellation from assertion failure. Ending a load window cannot hide a real failure that occurred before a later request was cancelled.
- Histogram memory remains bounded, with 32 buckets per power of two and at most 3.125% relative bucket error. Thresholds reject negative/non-finite values.
- JUnit records failed iterations even when configured load thresholds all pass.
- XML rejects multiple roots and non-whitespace text outside the root. This does not implement XPath or change the documented map model.

### Native protocols and structure

- Added verified gRPC TLS, custom CA trust, server name validation, mutual TLS, and repeated request metadata. Reflection receives the same metadata as unary calls.
- Reflection service descriptors are cached per link. Streaming methods fail explicitly through the unary API.
- Moved the HTTP request pipeline out of execution orchestration into `internal/engine/http.go`.
- Added public aliases for scenario, step, and exchange result types; documented the embedding API and ownership/error contracts.
- Updated the supported-feature contract and language, HTTP, calls, mocks, load, protocol, installation, and site documentation. Removed outdated claims about deep merge, row numbering, cache keys, protocol TLS, and exact sample percentiles.
- `make ci` now includes formatting/lint, vet, race tests, a bounded fuzz run, and the no-CGO build. This is a local reproducible target; no remote CI execution is claimed.

The earlier follow-up also added scoped cookies, persistent mocks, typed file reads/outlines, helper aliases, validator error handling, operation deadlines, request timestamps, and the initial public runner. The original audit regressions already passed when this continuation began; credit for the user's earlier repairs belongs to those changes, not this continuation.

## Verification on the final code

Environment: macOS arm64, Go 1.27.2. Java comparison checkout remains clean at `00d0fc9efdbbfe632005af69224bb07b3848d85a`.

| Check | Result |
| --- | --- |
| `go test -race -coverpkg=./... -coverprofile=... ./...` | Passed, including original audit and local protocol/browser integrations; no races reported |
| Combined statement coverage | **70.4%** across `./...`; profile and per-function summary retained |
| `make lint` | **0 issues** |
| `go vet ./...` | Passed |
| `make fuzz` | Passed; final 10-second run executed **71,644** cases; an earlier run executed 158,330 |
| `make build` | Passed with `CGO_ENABLED=0`, producing `bin/gi` |
| Final binary smoke | Passed two scenarios in parallel and wrote HTML, JSON, and JUnit artifacts |
| Overlapping-validator benchmark | 64 elements: about 180.5 µs/op, 73,826 B/op, 520 allocations/op on Apple M4; a single workload, not a general performance guarantee |

The original audit reported 63.0% coverage with a slightly different instrumentation scope (`./internal/...`); the coverage figures are evidence of the exercised code, not a normalized quality score.

Passing checks do not establish complete behavioral equivalence. No full differential execution of the Java Karate feature corpus or cross-platform release matrix was performed.

Logs, a compressed coverage profile, a coverage summary, and source SHA-256 hashes are in [release-checks](release-checks). The hashes identify the source state verified here. The workspace has no Git metadata, so no commit or PR was created.

## Remaining work for the original full-parity goal

1. A namespace-preserving XML document model, XPath evaluation/mutation, XML matching/HTTP/SOAP semantics, and broad JSONPath support.
2. Driver locator/element/wait/page/frame/dialog/upload/screenshot coverage and browser ownership across feature calls.
3. Full configuration propagation, callable/closure ownership, cached cookie replay, line selectors, disk caches, feature/outline hooks, special tags, and public extension lifecycles.
4. Asynchronous channel APIs, streaming and richer protocol options, additional auth flows, templating/server APIs, image comparison, Cucumber JSON, and other reporting/extension surfaces.
5. A differential conformance corpus against the pinned Java checkout and an explicit platform/browser support matrix.

These are feature implementations and conformance work, not issues that a stylistic rewrite or a passing linter can resolve. The current compatibility document keeps them visible rather than treating partial support as equivalence.

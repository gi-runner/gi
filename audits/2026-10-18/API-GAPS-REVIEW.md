# Review of async, attachments and retained cached closures

The existing full race suite passes and the configured linter reports 0 issues. Independent behavioral probes nevertheless reproduce correctness defects. No implementation files were changed by this review.

## Findings

1. **P1 — cached function execution ignores caller cancellation.** `internal/script/jsfunc.go:96` executes directly on the origin VM without watching the caller context. A later scenario invokes a cached infinite loop; a public Run with a 300 ms context deadline remains hung until an external watchdog kills it after three seconds. Waiting for the origin mutex is also not cancellable.
2. **P1 — lost wakeups in listen cancellation and timeout.** `internal/suite/signal.go:37` and `:49` broadcast without taking the condition mutex. Cancellation/expiry can occur after the predicate check and before Wait registers. A cancellation can then wait for the full listen timeout; a lost timeout broadcast can wait indefinitely. This finding follows from synchronization order; the normal timeout test does not exercise that interleaving.
3. **P2 — retained closures lose dependencies and scalar state.** `dataEnv` excludes callable/null values, snapshots scalar values, and install/restore discards scalar updates. Reproduced: a returned function invoking a sibling helper throws ReferenceError; a counter over a DSL def returns 1 on both calls instead of 1 then 2. This does not invalidate the passing lexical-let example, but the general closure claim is too broad.
4. **P2 — null arguments change semantics.** `callCached` maps both null and undefined to Sobek Undefined. Reproduced: a cached `function(x){ return x === null }` returns false for null.
5. **P2 — hook attachments disappear.** `internal/engine/hook.go:17` constructs a StepResult without draining e.embeds. An afterScenario callback embeds text, but the emitted JSON has zero attachments. Before hooks can misattribute pending attachments to the next ordinary step; after hooks can discard them.

## Engineering concerns

`internal/script/lock.go` implements recursive locking by parsing goroutine IDs from runtime.Stack output. This is not a supported goroutine identity API and complicates cross-runtime ownership and lock ordering. Retained VMs also retain scenario bindings/resources, so caller-vs-origin behavior needs an explicit contract. A probe of karate.info inside a later cached invocation reports the first scenario, not the current caller; whether that is intentional should be documented and tested.

## What is established

The normal signal/listen tests exercise an early queued value, concurrent delivery and a basic timeout. Normal-step attachments serialize through HTML/JSON/Cucumber paths; screenshot attachment wiring is present. Cached pure functions with the tested lexical-let closure work. These narrower results do not establish complete callback or closure semantics.

## Reproduction

Saved runner and Python probe generator are in api-gap-review/. Build runner.go.txt as a Go file from this module, then run the generator against that runner. Probe paths default to /tmp/gi-api-review-probes and the runner binary to /tmp/gi-api-review-runner. The deadline case intentionally runs in a disposable subprocess with a three-second watchdog.

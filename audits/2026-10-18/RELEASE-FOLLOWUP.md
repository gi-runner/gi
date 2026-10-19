# Release follow-up — earlier checkpoint

This checkpoint was superseded by [QUALITY-RELEASE.md](QUALITY-RELEASE.md). The approval limit was resolved and the final verification subsequently completed; retain the text below as history.

The current implementation is closer to Java Karate compatibility, but this is not a release approval or a claim of full parity. The original audit is in AUDIT.md. The fixes below are saved in the workspace.

## Changes made in this follow-up

- JavaScript: prevent repeated config execution on runtime errors; use live VM state for path mutations; synchronize cancellation cleanup; bound array path allocation; add native callable invocation.
- Compatibility: expose `karate` alongside `gi`; add collection and conversion helpers, typed file reads, embedded JSON expressions, and Karate config filename fallback.
- Scenarios: retain typed example values, evaluate expression columns, use zero-based `__num`, and support expression-driven Examples.
- HTTP: use a domain/path/expiry-aware cookie jar across redirects and child calls; expose response bytes and cookie metadata; support list-valued headers and parameters, dynamic methods, and timeout/configuration aliases.
- Calls: support JavaScript targets, dynamic read paths, array-driven feature calls, tag selectors, call metadata, inherited request sessions, and cycle detection.
- Mocks: execute Background once, preserve user state across requests, reset request metadata, expose route/request helpers, and fail on invalid matcher expressions.
- Reliability: bound channel/browser operations, make channel closure idempotent, preserve actual request timestamps, widen load histogram counters, and close internally owned idle connections.
- Matching: prevent validator exceptions or invalid regexes from becoming successful negative assertions.
- Public API: add a Go runner API and share adapter wiring with the CLI.
- Maintenance: consolidate CSV parsing and path matching, extract built-in JavaScript helpers, tidy module dependencies, and add regression/conformance tests plus a path-mutation fuzz target.

These are follow-up changes, not credit for the user's earlier fixes to the original 17 audit reproductions.

## Verification status

- All original 17 audit reproductions passed at the start of this follow-up.
- The ordinary full Go test suite passed during implementation, including the public runner and new HTTP, mock, file, call, and outline cases.
- Additional edits followed that full-suite pass, including validator error handling and helper extraction. The complete final tree still needs a fresh test run.
- Latest lint: **0 issues** (`/tmp/gi-release-lint.log`).
- Final race/coverage and fuzz commands were **not executed**: automatic approval review could not complete because the account usage limit was reached. This was not an unsafe-action determination.
- A final standalone no-CGO build remains unverified in this follow-up.

## Remaining release/parity work

A comprehensive final comparison is still required. Known areas needing further coverage or implementation include XML/XPath namespaces and mixed content, JSONPath breadth, browser Driver breadth, configuration and lifecycle hooks, tag/setup semantics, shared-scope call propagation and callSingle variants, SOAP, protocol TLS/streaming, and image/report extensions. The load histogram remains coarse; its percentiles are bucket estimates.

The changes have not established a blanket “Staff Engineer quality” conclusion. That requires review of the complete final tree, meaningful regression coverage, operational behavior, and clearly documented supported scope.

## Resume checks

Run these once normal command approval is available, without bypassing the failed approval review:

```sh
go test ./...
go test -race -coverpkg=./... -coverprofile=/tmp/gi-release-coverage.out ./...
go test ./internal/script -run '^$' -fuzz FuzzSetPath -fuzztime=5s -parallel=2
CGO_ENABLED=0 go build ./...
```

Then review the follow-up diff, update the compatibility documentation against actual behavior, and finish the remaining parity assessment. A pre-follow-up source snapshot is available at `/tmp/gi-release-before/`; this workspace has no Git metadata. Temporary files are not a durable backup.

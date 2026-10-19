# Native parity and release checkpoint

## Decision

The implementation is substantially closer to Karate and is a candidate for a release explicitly scoped to the documented native subset. It is **not established as a full Java Karate replacement**. A general compatibility release remains gated on broader upstream/differential coverage and the missing surfaces below. No release was published or tagged.

Comparison source: local Java Karate commit `00d0fc9efdbbfe632005af69224bb07b3848d85a`. The Go workspace has no Git metadata; the evidence directory includes source hashes for this checkpoint.

## Changes implemented

- Scoped calls share a scenario-local VM to preserve live closures, inherit configuration without re-running config, and restore assigned-call data/configuration. Shared calls propagate cookies, settings and browser ownership. Cache hits replay copied cookie updates and portable callback configuration. Setup uses isolated caller configuration without Background execution.
- Native XML DOM preserves namespaces, mixed content and sibling order. Added XPath query/set/remove, detached namespace-complete fragments, XML-aware copy/matching/serialization, HTTP XML parsing and SOAP action. XML JavaScript views are read-only to protect the document.
- Added native JSONPath recursive descent, filters, slices and unions; implicit response paths and get expressions. Replaced manual argument splitting with JavaScript parsing and fixed assignment parsing around XPath predicates.
- Added feature line selectors, function-call map propagation, comma-separated environment tags, afterFeature/afterScenarioOutline/onStepFailure, expected-failure tags, report suppression and cancellable named/exclusive suite locks.
- Expanded native Chrome Driver locators, fluent elements, actions, waits, navigation, script execution and PNG screenshots. Established browser borrowing/ownership across calls.
- Added HTTP TLS/PEM trust and mutual TLS, proxy/bypass configuration and connection timeout. Immutable transport settings reuse pools; owned transport pools close at run completion without changing a caller's transport.
- Added behavioral regressions for scoping, cookies/cache callbacks, XML/JSONPath, HTTP/SOAP, hooks/tags/locks, reports and real Chrome. Seven unchanged upstream scenarios now run with serial and concurrent scheduling; fixture hashes and MIT license are retained.
- Updated compatibility, data, suite, HTTP, protocol, language, installation and embedding documentation.

## Verification

All listed checks passed on macOS arm64 using Go 1.27.2:

| Check | Result |
| --- | --- |
| Full race suite with whole-module statement coverage | Pass; **71.7%** |
| Configured golangci-lint | **0 issues** |
| go vet | Pass |
| Module verification | All modules verified |
| Path fuzzing | 109,154 executions |
| Native XML fuzzing | 185,949 executions |
| CGO_ENABLED=0 CLI build | Pass |
| Linux amd64 / Windows amd64 no-CGO cross-builds | Pass; compilation only |
| Rebuilt CLI upstream smoke | All 7 scenarios pass; HTML, JSON and JUnit generated |

Logs, compressed coverage, smoke reports and a source manifest are in [parity-checks](parity-checks/). Browser tests exercised installed Chrome. The fuzz run preceded the final screenshot-format and detached-fragment namespace fixes; the full race suite and final build include those fixes.

## Remaining release boundaries

- Complete Java Driver surface: frames/windows/dialogs/uploads, providers, interception, browser cookies, fluent retries and advanced input chains.
- Full Java configuration/extension APIs, Java keystores, disk caches, async signal/listen, gRPC streaming/descriptors, advanced Kafka and authentication, templating/server integration, image comparison and complete report attachment formats.
- Cache results cannot contain functions/live handles. Cached configuration callbacks are reconstructed from source; lexical captures are not serialized. Live calls preserve ordinary closures, but scopes are not separate realms and do not completely isolate lexical declarations or intrinsic mutations.
- Native XML table-form set and structured map/list insertion are incomplete; complex missing XPath paths can fail. Native XPath/JSONPath grammar does not establish equivalence with every Java extension.
- Seven upstream scenarios are useful conformance evidence, not whole-corpus differential execution. Linux/Windows runtime testing and realistic downstream suite migration remain release gates for those claims.

## Engineering quality assessment

The changes improve concrete ownership, cancellation, scoped state, immutable cache/transport boundaries and behavioral test coverage. Optional browser capability interfaces keep the core Page contract small. This is evidence of cleaner Go engineering, not a blanket “Staff quality” certification or an endorsement by Pike/Cheney. The expanded engine and JavaScript boundary still warrant review as the corpus grows. Prefer these explicit contracts and regression evidence over pattern names or abstraction counts.

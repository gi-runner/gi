> Historical engineering audit snapshot, retained outside the product documentation. See ../../docs/limits.md for current user-facing limits.

# Compatibility

gi is a native runner for Karate-style feature files. Full Java Karate parity has not been established. Migration should use the supported scope below and execute the existing suite against gi.

The comparison baseline is the local Karate v2 checkout at commit `00d0fc9efdbbfe632005af69224bb07b3848d85a`. This is not a comparison with every published v1 release or commercial extension.

## Supported scope and evidence

| Area | Current behavior | Regression evidence |
| --- | --- | --- |
| Gherkin and examples | Background, scenarios, static and dynamic outlines, typed columns, zero-based row metadata; selection before dynamic expansion | `internal/feature`, `internal/engine/release_test.go` |
| JavaScript and data | Pure-Go Sobek, `karate`/`gi` aliases, expression and statement steps, collection helpers, typed reads, JSON embedded expressions | `internal/script`, `internal/engine/release_test.go` |
| Matching | Structural, contains/contains-only/deep, schema and predicate matching; validator errors cannot pass negative assertions | `internal/match`, `internal/audit` |
| HTTP | Requests, forms, multipart, retries, redirects, auth, list headers/params, bounded response bodies, scoped cookies, exact integer JSON parsing | `internal/engine`, `internal/audit` |
| Feature calls | Feature/JS targets, argument maps, list calls, tag and same-file selectors, call metadata, shared HTTP settings and changed JSON data | `internal/engine/release_test.go` |
| Caches | Per-calling-feature `callonce`, suite `callSingle`, first argument wins, copied JSON results, explicit runtime-handle rejection | `internal/suite`, `internal/engine/release_test.go` |
| Mocks | Background once, persistent serialized state, route variables, request metadata, matcher failures return 500 | `internal/mock` |
| Load | Context cancellation, failure preservation at deadline, bounded histogram with <=3.125% bucket error, thresholds, JUnit failures | `internal/load`, `internal/report` |
| Native protocols | WebSocket text, unary gRPC reflection with TLS/mTLS and metadata, basic Kafka records | `internal/engine/protocol_test.go`, `internal/grpcx` |
| Embedding | Public Go `Run`, options, result types, custom HTTP client and synchronized output | `gi_test.go`, [Embedding](../../docs/embedding.md) |

HTTP timing includes reading the body. Bodies over 8 MiB fail. Report headers mask `Authorization`, `Proxy-Authorization`, `Cookie`, and `Set-Cookie`. These masks do not redact arbitrary secrets in bodies, step text, or printed output.

## Additional native parity coverage

Native XML documents preserve namespaces, mixed content and sibling order, with XPath query/set/remove, XML HTTP bodies and `soap action`. JSONPath supports recursive descent, filters, slices and unions through the native OJG parser. See [Data](../../docs/data.md).

Calls inherit configuration without re-running config and retain ordinary JavaScript closures within the scenario VM. Assigned calls restore scoped data/configuration; shared calls propagate configuration, cookies and browser ownership. Line selectors and cached cookie replay are supported. Lifecycle includes after-feature/outline hooks, on-step-failure decisions, `@fail`, named/exclusive `@lock`, and `@report=false`.

The Driver supports common locators, element actions, waits, navigation, page scripts and PNG screenshots. HTTP supports PEM trust/mTLS, TLS versions, proxies and connect timeouts. Regression evidence is in `internal/engine/parity_test.go`, `internal/engine/upstream_test.go`, and browser, XML, report and suite tests.

## Outstanding compatibility gaps

- **XML/JSONPath:** No blanket equivalence with Java XPath/Jayway extensions. XML views are read-only in JavaScript; use XPath mutation. XML table-form set, structured map/list insertion and complex missing-path creation remain incomplete.
- **Browser Driver:** Frames, windows, dialogs, uploads, provider configuration, interception, browser cookies and the full fluent action API remain incomplete. Chrome is required externally.
- **Calls and configuration:** Scopes share a scenario VM, not separate JavaScript realms; lexical declarations and intrinsic mutations are not fully isolated. Cached returned functions/handles are rejected. Cached configuration callbacks are recreated from source and cannot serialize lexical captures. Disk caches and the complete configuration API remain incomplete.
- **Protocols and auth:** gRPC streaming and descriptor-file loading, Kafka TLS/SASL/groups/offset controls, NTLM, and OAuth grants beyond client credentials are not implemented. Java keystore formats are unsupported; HTTP certificates use PEM.
- **Asynchronous APIs:** WebSocket binary/options and Karate signal/listen/channel lifecycle semantics remain incomplete.
- **Templating and integrations:** Markup/render/doc/server APIs and native replacements for Java integration utilities remain incomplete.
- **Reports and extensions:** Image comparison, Cucumber JSON, arbitrary report attachments, and Java plugin/SPI replacements are incomplete.
- **Validation:** Seven unchanged upstream scenarios run serially and concurrently; this is a sample, not whole-corpus differential testing. Linux and Windows cross-compilation does not establish runtime compatibility.

JVM integration (`Java.type`, JUnit rules, JAR service loaders) is deliberately excluded from a no-JVM runner. Native alternatives need their own public design; they cannot be claimed as compatibility by renaming these hooks.

## Release interpretation

Passing the test, race, fuzz, lint, vet, and no-CGO build checks establishes evidence for this implementation and scope. It does not establish complete Karate equivalence or guarantee that unported feature files will work. The original audit probes remain in `internal/audit`; the follow-up records in `audits/2026-10-18` distinguish fixed defects from outstanding feature work.

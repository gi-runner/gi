# Karate conformance fixtures

These files test gi against examples copied unchanged from Karate. They are test inputs, not the gi implementation or a bundled Java runtime.

| Fixtures | Coverage |
| --- | --- |
| `copy/` | Isolated/shared calls and deep copies: six scenarios. |
| `xml/` | Passing native XML nodes to a called feature: one scenario. |
| `config/` | Reading configuration and keeping detached snapshots: two scenarios. |

Run from the repository root:

```bash
go test ./internal/engine -run 'TestPinnedUpstreamFeatures|TestUpstreamFixtureManifest'
```

The runner executes all nine scenarios with `Jobs: 1` and `Jobs: 4`. Called helpers retain their original `@ignore` tags. The manifest test checks the SHA-256 of every listed fixture.

Source: Karate commit [`00d0fc9efdbbfe632005af69224bb07b3848d85a`](https://github.com/karatelabs/karate/tree/00d0fc9efdbbfe632005af69224bb07b3848d85a). [`manifest.json`](manifest.json) records each original path and checksum. These fixtures retain Karate's [MIT license](LICENSE).

This is a small upstream conformance sample. It does not execute Java Karate alongside gi or establish complete feature parity. Other regressions live under `internal/engine`, `internal/script`, and the protocol packages.

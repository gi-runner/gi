---
name: openspec-gi
description: Compile an OpenSpec scenario into gi features, then run gi check and gi test. Use when the user asks to test a spec, run gi, or turn GIVEN WHEN THEN scenarios into requests.
---

The feature file is the test. Do not write step stubs.

1. Name one HTTP call in each WHEN line, for example `POST /orders/{id}`.
2. Put an `example` on every path parameter, required header, cookie, and required request field. A security scheme needs an example on the header, query, or cookie it uses. `http`, `oauth2`, and `openIdConnect` use `Authorization`. A concrete value in the When line counts.
3. Run `gi spec --json` on the directory that holds `spec.md` or `requirements.md`.
4. Read `notes` and `evidence`. `skipped` means name one call. `example` means add the named example. `clarify` waits for a person. `edge` is not a test. `type` is a match that only checks a type. `load` means the window is 30s because the spec did not state one. `uncovered` means no scenario is tagged with the id. `unexecuted` means the tagged scenario did not run. `passing` and `failing` are this commit. A `# spec.md:9` comment above a scenario is the acceptance line that produced it. `stale` is an earlier commit and is not proof. The same gaps are printed under `open:`.
5. Run `gi check` on the features directory and fix every finding.
6. Run `gi test` with `--var baseUrl=...`. One spec feature runs its scenarios in order. Pass `--jobs` only when each scenario stands alone. Change the service or the spec. Do not invent a path, a body, a header, or a status.
7. `gi test --tag ~@contract` is the acceptance run. `@contract` scenarios are calls no scenario claimed.

A scenario comment is the spec line it came from. Exit 0 means a feature was written. Exit 1 means nothing was written. Exit 2 means the path is wrong.

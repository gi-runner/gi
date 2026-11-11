---
name: openspec-gi
description: Compile an OpenSpec scenario into gi features, then run gi check and gi test. Use when the user asks to test a spec, run gi, or turn GIVEN WHEN THEN scenarios into requests.
---

The feature file is the test. Do not write step stubs.

1. Name one HTTP call in each WHEN line, for example `POST /orders/{id}`.
2. Put an `example` on every path parameter, required header, cookie, and required request field. A security scheme needs an example on the header, query, or cookie it uses. `http`, `oauth2`, and `openIdConnect` use `Authorization`. A concrete value in the When line counts.
3. Run `gi spec --json` on the directory that holds `spec.md` or `requirements.md`.
4. Read `notes`. `skipped` means name one call. `example` means add the named example. `clarify` waits for a person. `edge` is not a test. `load` means the window is 30s because the spec did not state one.
5. Run `gi check` on the features directory and fix every finding.
6. Run `gi test` with `--var baseUrl=...`. Change the service or the spec. Do not invent a path, a body, a header, or a status.
7. `gi test --tag ~@contract` is the acceptance run. `@contract` scenarios are calls no scenario claimed.

A scenario comment is the spec line it came from. Exit 0 means a feature was written. Exit 1 means nothing was written. Exit 2 means the path is wrong.

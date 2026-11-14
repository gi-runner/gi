# Specs

`gi spec` reads a Spec Kit, Kiro, or OpenSpec directory and writes one gi feature per user story. It does not plan the change or write the service. `gi load` runs that feature when the spec states a concurrency or a request rate.

```bash
gi spec --json specs/001-payment
gi check specs/001-payment/features
gi test specs/001-payment/features --var baseUrl=http://127.0.0.1:8080
```

Name the call in When: `POST /invoices/{id}/reminders`. One OpenAPI operation can omit the path. Two operations and no path are a `skipped` note. A path parameter, required header, cookie, body field, or security credential without an `example` is an `example` note, and that call is not written. `http`, `oauth2`, and `openIdConnect` use `Authorization`. An empty security requirement is public. A concrete path such as `/invoices/inv_1/reminders` supplies the example. A response schema becomes a `match`. A required field with an example, or with one enum value, is matched as that value. A field without one stays a type marker such as `#string`. `NEEDS CLARIFICATION` stays in the report. The scenario comment is the spec line.

`--json` prints `files`, `notes`, and `evidence`. Exit 0 means a feature was written, 1 means nothing was written, 2 means the path is wrong. The human report ends with `open:` for a requirement the run did not prove: a clarification, an edge with no scenario, a call that was not written, a match that only checks a type, or a load window the spec did not state. `gi test` prints those same lines from the feature, and adds `failed:` when a generated scenario fails.

`evidence` answers a different question: which requirement or acceptance criterion has a passing scenario on this git commit. `@FR-016` on the feature says the story mentioned it. `@AC-016-01` on a scenario is the association. `passing` and `failing` are that scenario's result for the current commit. A `# spec.md:9` comment above a scenario is the acceptance line that produced it, and that line follows the same result. `unexecuted` means the scenario was not run. `uncovered` means no scenario carries the id. `stale` means the recorded result is for another commit, or the run had no commit, so it is not proof for this tree. `passing` stays in the JSON. The other rows are printed under `open:`. They do not change the exit code. `gi test` writes `.gi-evidence.json` beside the feature for the run it just finished.

One spec feature runs its scenarios in order. `--jobs` runs those scenarios at once, so use it when each scenario stands alone. A directory of spec features still runs each file's scenarios in order.

`50 concurrent users` and `p95 under 200ms` become:

```gherkin
# gi-load: name=spec vus=50 duration=30s
# gi-load: threshold p95 < 200ms
```

The window is 30 seconds when the spec does not state one. "Finish in under 2 minutes" is not a load profile.

Unclaimed operations go to `contract.feature`, tagged `@contract`. `gi test --tag ~@contract` is the acceptance run. Files that start with `# gi-spec:` are rewritten. Other feature files stay.

| Tool | Install | Command |
| --- | --- | --- |
| Spec Kit | `specify extension add --dev sdd/spec-kit` | `/speckit-gi-spec` |
| Kiro | copy `sdd/kiro/gi.md` to `.kiro/steering/gi.md` | `/gi` |
| OpenSpec | copy `sdd/openspec/gi/SKILL.md` to `.agents/skills/openspec-gi/SKILL.md` | `/openspec-gi` |

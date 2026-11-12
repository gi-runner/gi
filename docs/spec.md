# Specs

`gi spec` reads a Spec Kit, Kiro, or OpenSpec directory and writes one gi feature per user story. It does not plan the change or write the service. `gi load` runs that feature when the spec states a concurrency or a request rate.

```bash
gi spec --json specs/001-payment
gi check specs/001-payment/features
gi test specs/001-payment/features --var baseUrl=http://127.0.0.1:8080
```

Name the call in When: `POST /invoices/{id}/reminders`. One OpenAPI operation can omit the path. Two operations and no path are a `skipped` note. A path parameter, required header, cookie, body field, or security credential without an `example` is an `example` note, and that call is not written. `http`, `oauth2`, and `openIdConnect` use `Authorization`. An empty security requirement is public. A concrete path such as `/invoices/inv_1/reminders` supplies the example. A response schema becomes a `match`. A required field with an example, or with one enum value, is matched as that value. A field without one stays a type marker such as `#string`. `NEEDS CLARIFICATION` stays in the report. The scenario comment is the spec line.

`--json` prints `files` and `notes`. Exit 0 means a feature was written, 1 means nothing was written, 2 means the path is wrong.

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

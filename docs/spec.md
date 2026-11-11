# Specs

`gi spec` reads a Spec Kit, Kiro, or OpenSpec directory and writes one gi feature per user story. The feature is the functional test. `gi load` runs it when the spec states a concurrency or a request rate.

```bash
gi spec --json specs/001-payment
gi check specs/001-payment/features
gi test specs/001-payment/features --var baseUrl=http://127.0.0.1:8080
```

Put the call in the acceptance scenario: `When POST /invoices/{id}/reminders`. A directory with one OpenAPI operation can omit the path. Two operations and no path are a `skipped` note that names the shape of the call. A path parameter, a required header or cookie, a required body field, or the credential a security scheme names, without an `example`, is an `example` note, and that call is not written. `http`, `oauth2`, and `openIdConnect` ask for an `Authorization` example. An empty security requirement is a public call. A value in the When line, such as `/invoices/inv_1/reminders`, is the example. A response schema becomes a `match`. Requirements marked `NEEDS CLARIFICATION` stay in the report. Each scenario comment is the spec line it came from.

`gi spec --json` prints `files` and `notes` for an agent. Exit 0 means a feature was written. Exit 1 means a spec was found and nothing was written. Exit 2 means the path is wrong.

A criterion such as `50 concurrent users` and `p95 under 200ms` becomes:

```gherkin
# gi-load: name=spec vus=50 duration=30s
# gi-load: threshold p95 < 200ms
```

The window is 30 seconds when the spec does not state one, and the report says so. A task time, such as "finish in under 2 minutes", is not a load profile.

Operations no scenario claimed are written to `contract.feature` and tagged `@contract`. `gi test --tag ~@contract` runs the acceptance scenarios. Files `gi spec` owns start with `# gi-spec:`. Other feature files in the directory are left in place.

Spec Kit runs that loop from [sdd/spec-kit](https://github.com/gi-runner/gi/blob/main/sdd/spec-kit/commands/spec.md) after the plan. Install it with `specify extension add --dev sdd/spec-kit`. In Cursor the command is `/speckit-gi-spec`. Kiro loads [sdd/kiro/gi.md](https://github.com/gi-runner/gi/blob/main/sdd/kiro/gi.md) from `.kiro/steering/gi.md` as `/gi`. OpenSpec loads [sdd/openspec/gi/SKILL.md](https://github.com/gi-runner/gi/blob/main/sdd/openspec/gi/SKILL.md) from `.agents/skills/openspec-gi/SKILL.md` as `/openspec-gi`. All three run `gi spec --json`, then `gi check`, then `gi test`.

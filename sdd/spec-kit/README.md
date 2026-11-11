# gi Spec Kit extension

Install from a gi checkout:

```bash
specify extension add --dev sdd/spec-kit
```

`speckit.gi.spec` runs after the plan. It compiles the spec with `gi spec --json`, then `gi check`, then `gi test`.

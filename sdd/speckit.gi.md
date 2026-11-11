# speckit.gi

Spec Kit: `specify extension add --dev sdd/spec-kit`, then `/speckit-gi-spec` (`speckit.gi.spec` after the plan).

Kiro: copy `sdd/kiro/gi.md` to `.kiro/steering/gi.md`. The command is `/gi`.

OpenSpec: copy `sdd/openspec/gi/SKILL.md` to `.agents/skills/openspec-gi/SKILL.md`. The command is `/openspec-gi`.

All three run `gi spec --json`, then `gi check`, then `gi test`. Notes in the JSON are the edits: name one call, or add an example. Do not invent a request.

# gi feature files for VS Code

Highlighting, snippets, and run commands for gi `.feature` files.

Type `feature`, `scenario`, `outline`, `background`, `def`, `match`, `request`, `call`, or `docstring`.

Run and Load above `Feature:` run `gi test` and `gi load`. Run above a scenario runs the whole file, because gi selects files by path.

| Setting | Default |
| --- | --- |
| `gi.path` | `gi` |

If Cucumber also claims `*.feature`, set `"files.associations": { "*.feature": "gi-feature" }`.

```sh
cd editors/vscode
npm install
npx @vscode/vsce package
code --install-extension gi-feature-1.0.0.vsix
```

Open this folder and press F5 to try the extension.

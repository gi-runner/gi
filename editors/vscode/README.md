# gi feature files for VS Code

Language support for the `.feature` files that [gi](https://github.com/gi-runner/gi) runs.

## Features

- Syntax highlighting for Gherkin keywords, step keywords (`def`, `match`, `url`, `method`, ...), tags (`@smoke`, `@weight=8`, `@env=dev`), `# gi-load:` directives, match operators, fuzzy markers (`#string`, `##number`, `#[] #string`, `#? _ > 0`), embedded expressions (`#(expr)`, `##(expr)`), Scenario Outline placeholders, and tables.
- Doc strings: lenient JSON (unquoted keys, single quotes) gets its own highlighting, content starting with `<` is highlighted as XML, and content starting with `function` or an arrow function as JavaScript.
- Snippets: `feature`, `scenario`, `outline`, `background`, `def`, `match`, `request`, `call`, `docstring`.
- Commands, also shown as CodeLens links above `Feature:` and each scenario:
  - `gi: Run Feature File` runs `gi test <file>`.
  - `gi: Run Scenario` runs `gi test <file>` as well. gi selects files by path and has no `file:line` form, so the whole file runs.
  - `gi: Load Test Feature File` runs `gi load <file>`.

Commands run in an integrated terminal named `gi`, from the workspace folder that contains the file.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `gi.path` | `gi` | Path to the `gi` executable. |

## `.feature` file association

The extension registers the `gi-feature` language for `*.feature`. If another extension (for example Cucumber) also claims `.feature`, VS Code uses one of them. To choose explicitly, set the association in your settings:

```json
"files.associations": { "*.feature": "gi-feature" }
```

You can also switch a single file with **Change Language Mode** in the status bar. The extension does not change any settings itself.

## Install from a .vsix

```sh
cd editors/vscode
npm install
npx @vscode/vsce package
code --install-extension gi-feature-1.0.0.vsix
```

## Development

```sh
npm install
npm run compile
```

Open `editors/vscode` in VS Code and press F5 to start an Extension Development Host.

# Browser automation

`driver` opens a URL in headless Chrome. Install Chrome on the machine running gi; it is not bundled. Actions accept CSS or XPath selectors; single-element locators also support text selectors. `gi.pageText` reads an element.

```gherkin
Feature: Greeting form

Scenario: submit a name
  * driver pageUrl
  * input '#name' = 'Ada'
  * click '#go'
  * waitForText('#out', 'Ada')
  * def got = gi.pageText('#out')
  * match got == 'Ada'
```

Save as `greeting.feature` and run it against a page with `#name`, `#go` and `#out` elements:

```bash
gi test greeting.feature --var pageUrl=http://127.0.0.1:8080 --report browser.html
```

The test enters `Ada`, clicks the button, waits for `#out` to contain `Ada`, and checks that its complete text is exactly `Ada`. A successful run exits `0`.

`click` and `input` fail with `driver is not open` until `driver` has run. The page closes when the scenario ends. A machine without Chrome fails the `driver` step.

## Actions and locators

After opening `driver`, global helpers and driver methods support `click`, `input`, `inputFile`, `clear`, `select`, `focus`, `scroll`, `text`, `html`, `value`, `attribute`, `exists`, `enabled`, `locate`, `locateAll`, `waitFor`, `waitForText`, `waitForEnabled`, `waitForUrl`, string `waitUntil`, `script` and `screenshot`. Navigation includes back, forward and refresh; `driver.url` and `driver.title` are readable properties. Locators support CSS, XPath and single-element text selectors. Located elements expose fluent actions and read properties.

Screenshots return PNG bytes and are attached to the step that took them. Shared calls retain browser ownership; isolated calls borrow an existing page without closing it.

## Frames, dialogs and windows

| Task | Call |
| --- | --- |
| Enter a same-origin frame | `driver.frame('#checkout')` |
| Return to the top document | `driver.frame()` |
| Accept the next dialog, with optional prompt text | `driver.dialog(true, 'Ada')` |
| Dismiss the next dialog | `driver.dialog(false)` |
| Read the last dialog message | `driver.dialogText()` |
| Open a URL in a new window and switch to it | `driver.window(url)` |
| List windows as `{ index, url, title }` | `driver.window()` |
| Switch to a window by index | `driver.window(0)` |

Arm a dialog response before the action that opens it. Frames must be same-origin; `srcdoc` frames are supported.

## Intercept requests

```gherkin
* driver.intercept('/secret', { status: 200, body: 'hidden' })
* click '#fetch-secret'
* driver.intercept()
```

The rule answers requests whose URL contains the given text. Rules belong to the current window; a window opened afterward inherits the rules already set. Calling `intercept()` clears the current window's rules.

Providers, a full browser cookie jar, and the full fluent retry API remain incomplete.

## File uploads

```gherkin
* driver pageUrl
* inputFile('#document', 'report.pdf')
* driver.inputFile('#photos', ['front.png', 'back.png'])
* locate('#document').inputFile('replacement.pdf')
* inputFile('#document', [])
```

Upload paths resolve like `read()`: relative to the feature or project root, with `this:`, `classpath:` and `file:` prefixes. Files must exist and be regular files. Lists support multiple inputs; an empty list clears the selection. CSS, XPath and single-element text locators use the same lookup as other actions.

gi preserves Chrome's default sandbox settings. On a restricted CI host that cannot run it, `GI_CHROME_NO_SANDBOX=true` explicitly disables it. The Linux CI job uses this workaround; regular runs do not disable the sandbox automatically.

See [Reports](reports.md) for screenshot attachments.

## Common mistakes

| Symptom | Check |
| --- | --- |
| Chrome cannot start | Install Chrome on the runner. Check CI sandbox restrictions before changing browser settings. |
| `driver is not open` | Open a URL with `driver` before calling actions. |
| A locator times out | Confirm the selector and current frame/window. Use `waitFor` or `waitForText` for asynchronous updates. |
| A dialog blocks the next action | Call `driver.dialog(...)` before triggering it. |
| A frame cannot be entered | It must be same-origin. Cross-origin frames are not supported. |
| An upload file is missing | Paths resolve from the feature or project root, not necessarily the shell's working directory. |

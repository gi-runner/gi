# Browser automation

Control Chrome with feature steps such as `driver`, `input` and `click`. Install Chrome on the runner; it is not bundled. Chrome runs headlessly, so no window opens during a normal test.

## Try it

Save as `greeting.feature`:

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

The test expects a name field, a button and an output element. To try it without an application, save this as `greeting.mock.feature`:

```gherkin
Feature: Greeting page

Scenario: serve the form
  * pathMatches '/'
  * def responseHeaders = { 'Content-Type': 'text/html' }
  * text response =
    """
    <!doctype html>
    <input id="name" aria-label="Name">
    <button id="go" onclick="document.getElementById('out').textContent = document.getElementById('name').value">Greet</button>
    <p id="out"></p>
    """
```

Start the example page:

```bash
gi mock greeting.mock.feature --addr 127.0.0.1:18082
```

In another terminal:

```bash
gi test greeting.feature --var pageUrl=http://127.0.0.1:18082 --report browser.html
```

The test enters `Ada`, clicks Greet, waits for the output, and checks that it is exactly `Ada`. It exits `0` when all steps pass. To test your own application, replace `pageUrl` and the selectors. `#name`, for example, selects the element with `id="name"`.

The browser closes at the end of the scenario. Actions fail with `driver is not open` until a `driver` step opens a URL.

## Actions and locators

| Task | Helpers |
| --- | --- |
| Change an element | `click`, `input`, `clear`, `select`, `focus`, `scroll` |
| Read or inspect | `text`, `html`, `value`, `attribute`, `exists`, `enabled` |
| Wait for a change | `waitFor`, `waitForText`, `waitForEnabled`, `waitForUrl`, `waitUntil` (a JavaScript string) |
| Reuse a locator | `locate`, `locateAll`; elements expose actions and properties |
| Upload or capture | `inputFile`, `screenshot` |
| Navigate | `driver.back()`, `driver.forward()`, `driver.refresh()`; `driver.url`, `driver.title` |
| Run page JavaScript | `script` |

Actions accept CSS and XPath selectors. Single-element locators also support text selectors. Most helpers work globally, such as `click('#go')`, or on `driver`, such as `driver.click('#go')`.

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

# Feature files and variables

A `.feature` file contains named tests. gi reads each test's steps from top to bottom. You can write API requests, assertions, and JavaScript expressions in the same file; no Java or Go test code is required.

## Read a feature file

Save this as `basics.feature` and run `gi test basics.feature`. It needs no server.

```gherkin
Feature: User data

Background:
  * def minimumAge = 18

Scenario: check a user
  * def user = { name: 'Ada', age: 36 }
  * match user.name == 'Ada'
  * assert user.age >= minimumAge
```

The scenario passes. Here's what each part means:

| Part | Meaning |
| --- | --- |
| `Feature:` | Names the file's group of tests. |
| `Scenario:` | Names one test. |
| `Background:` | Steps to run before each scenario. |
| `*` | Starts a step. `Given`, `When`, `Then`, `And` and `But` can be used instead. |
| `def` | Creates a variable. |
| `match` | Compares values; see [assertions](match.md). |
| `assert` | Requires a JavaScript expression to be `true`. |

These headings come from Gherkin, the text format also used by Cucumber. You do not need to know Cucumber to use gi.

## Variables and expressions

Use JavaScript literals and expressions after `=`. Variables are available to later steps in the scenario.

```gherkin
* def name = 'Ada'
* def user = { name: name, age: 36 }
* def names = ['Ada', 'Grace']
* def greeting = 'Hello, ' + user.name
* match greeting == 'Hello, Ada'
```

Each scenario starts with its own variables. Use [configuration](suite.md#config) for values needed by many scenarios, such as `baseUrl`. [Feature calls](suite.md#call) let you share a flow.

`response.users[*].id` selects every user's ID. That is a gi path expression, not JavaScript; see [Data](data.md). Load tests also provide `vu` and `iter` for [unique test data](load.md#unique-test-data).

## Multiline values

Three double quotes enclose a multiline value, called a docstring. It belongs to the step above it. `text` keeps it as text; `json`, `yaml`, `csv` and `xml` parse it.

```gherkin
* json user =
  """
  { "name": "Ada", "age": 36 }
  """
* match user.age == 36
```

Inside a structured value, `'#(expression)'` inserts the expression's value. For example:

```gherkin
* def name = 'Ada'
* def user = { name: '#(name)' }
* match user.name == 'Ada'
```

`'##(expression)'` does the same, but drops the key when the value is null or undefined.

## Common steps

| Step | Result |
| --- | --- |
| `copy saved = user` | Deep copy a value before changing it. |
| `set user.name = 'Grace'` | Update an object field or list item. |
| `remove user.nickname` | Delete a field or list item. |
| `table users` | Build a list from the following table's column names and rows. |
| `replace message.name = 'Ada'` | Replace `<name>` in the text variable `message`. A token/value table can replace several placeholders. |
| `print user` | Print a value and record it on the report step. |
| `eval user.age++` | Execute JavaScript. Bare JavaScript statements work too. |
| `string value = expression` / `bytes value = expression` | Convert a value to text or bytes. |

## read

`read('file')` and `gi.read('file')` resolve a path and return its value.

| Path | Resolution |
| --- | --- |
| `classpath:a/b.json` | From the project root. |
| `this:a/b.json` | From the current feature directory. |
| `file:/abs/path` | Host path, used as given. |
| `/a/b.json` | From the project root. |
| `b.json` | The feature's directory, then the project root. |

To find the project root, gi searches the first feature's directory and then its parents for `gi-config.js` or `gi-base.js` (the `karate-` names also work). Without a config file, it is that feature's directory.

| Extension | Return value |
| --- | --- |
| `.json` | Parsed JSON. |
| `.js` | The value of `fn` when the file declares `function fn`. Otherwise the completion value of the script. |
| `.feature` | A feature reference that can be assigned and used by `call`. |
| `.yaml` / `.yml` | Parsed YAML. |
| `.csv` | A list of objects with string cells. |
| `.xml` | An XML document; see [Data](data.md#xml). |
| `.png`, `.jpg`, `.jpeg`, `.gif`, `.pdf`, `.zip`, `.bin`, `.gz` | Bytes. |
| anything else | The file text. |

## configure

Settings apply to the current scenario:

```gherkin
* configure timeout = 5000
* configure followRedirects = false
* configure retry = { count: 5, interval: 200 }
```

`timeout` and `retry.interval` are milliseconds. `gi.configure(key, value)` takes the same keys. An unknown key fails the step, so a typo is caught.

| Key | Effect |
| --- | --- |
| `url`, `headers`, `cookies`, `auth`, `charset` | Request defaults. See [HTTP](http.md). |
| `timeout` / `readTimeout`, `connectTimeout`, `retry`, `httpRetryEnabled` | Timing and retries. |
| `ssl`, `proxy`, `localAddress`, `followRedirects` | Connection settings. |
| `strictResponseParsing`, `matchEachEmptyAllowed` | Parsing and `match each` on an empty list. |
| `continueOnStepFailure` | Keep running steps after a failure. |
| `abortSuiteOnFailure` | Skip every later scenario in the run after a failure. |
| `abortedStepsShouldPass` | Report steps after `karate.abort()` as passed instead of skipped. |
| `beforeScenario`, `afterScenario`, `afterScenarioOutline`, `afterFeature`, `onStepFailure` | Hooks. See [Suite](suite.md#hooks). |
| `callSingleCache` | `{ minutes, dir }` keeps `callSingle` results on disk across runs. The default directory is the user cache directory. |
| `report` | `false`, or `{ showLog, showAllSteps }`. `showAllSteps: false` keeps only HTTP, call, attachment and failed steps. |
| `logging` | `{ console, report, pretty, mask }`. A `console` level above `info` silences `print`; `pretty` indents JSON bodies in reports; `mask: { headers, patterns }` hides more values, and `mask: false` shows secrets. |
| `cors`, `responseHeaders` | Mock settings. See [HTTP mocks](mock.md). |
| `driver` | Browser launch settings. See [Browser](browser.md). |

`logPrettyRequest`, `logPrettyResponse`, `printEnabled`, `lowerCaseResponseHeaders` and `logModifier` are accepted and ignored with a warning, as in Karate v2.

## JavaScript helpers

Use `gi` for helpers such as `gi.read`, `gi.map` and `gi.setupOnce`. `karate` is an alias for the same object, so existing `karate.*` calls work too. The global `read()` is shorthand for `gi.read()`.

See the [helper reference](helpers.md) for collections, conversion, file output, polling and runtime information. To expose a Go function, see [Embedding](embedding.md#register-a-helper).

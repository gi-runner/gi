# Suite

`gi test` with no path walks `features/`, then `examples/`, then the current directory. A directory argument walks every `.feature` file under it, skipping hidden directories and `vendor/`. One HTML report covers the whole run.

## Parallel

`--jobs` is how many features run at the same time. The default is one per CPU. Results stay in the order of the files, and the report is that order.

| Invocation | What runs at once |
| --- | --- |
| Several files, `--jobs 4` | Four features. Scenarios inside a feature stay in order. |
| One file, `--jobs 4` | Four scenarios of that file. |
| `--jobs 1` | One feature at a time, scenarios in order. |

Scenarios that share a server and depend on each other's data should stay at `--jobs 1`, or live in one file that runs in order. `karate.signal(value)` and `karate.listen(milliseconds)` share a queue for the whole run, so one scenario can wait for another when `--jobs` is greater than 1. A signal sent first stays queued. `listen` fails when its timeout elapses. A separate `gi mock` process does not share the queue.

## Config

gi looks upward from the first feature path for three files, and evaluates them in this order on a fresh runtime for every scenario:

1. `gi-base.js`
2. `gi-config.js`
3. `gi-config-<env>.js` when `--env` is set

Each filename falls back to the corresponding `karate-` filename. If both exist, `gi-` wins. Read errors fail the run.

Each file returns an object, or a function that returns an object. The keys become variables. `--var key=value` is applied after config, so the flag wins. `--env` also sets `gi.env`.

```javascript
function fn() {
  var token = gi.callSingle('auth.feature').token
  return { baseUrl: 'http://127.0.0.1:18080', token: token }
}
```

## call

```gherkin
* call read('login.feature')
* def session = call read('login.feature') { user: 'ada' }
* callonce read('login.feature')
```

`call` runs another feature in a scoped scenario VM, preserving ordinary JavaScript closures. `def result = call ...` returns the variables defined by the callee. A bare call also propagates changed JSON data and persistent HTTP settings (URL, request headers, auth, timeouts, redirect following, and response parsing settings). Transient request builders are cleared. Isolated calls fork the cookie jar; shared calls replay changes into the caller. `karate.call(true, path, arg)` requests shared scope; `karate.call(path, arg)` is isolated.

The argument is an object or a list of objects. Object keys are bound as variables alongside `__arg`. List calls return a list of results and bind zero-based `__loop`; a single call uses `__loop = -1`. `child.feature:12` selects a scenario or outline by line; multiple lines use `:12:30`. `child.feature@tag` selects a tag, and `@tag` selects a scenario in the current file. Called features may use `@ignore`; setup and environment exclusions still apply. Excluded dynamic outlines are not evaluated.

`callonce` runs once per calling feature and resolved target; the first call's argument wins. `karate.callSingle(path, arg)` runs once per suite and supports feature or JavaScript files. A `?suffix` gives a singleton a distinct cache key. Cached results are copied and may contain JSON data or native XML. A function in that result, and a `configure` header or hook function, keeps calling the runtime that created it. Scenario variables are live and shared, so a sibling function and a later assignment are still visible, and `null` stays `null`. A lexical `let` or `var` stays with the function. Calling it observes the caller's cancellation, and `karate.info` reports that caller. While it is inside `listen`, `pause`, `waitForHttp`, or `waitForPort`, another scenario can call back into that runtime. Native functions and live protocol handles are rejected. Normal feature and JavaScript calls are not restricted to JSON cache data.

Config is inherited without re-executing config files. Shared calls propagate header/hook configuration and browser ownership; assigned calls restore caller scope. Cache hits replay cookie updates and call the original header and hook functions. Scopes are not separate JavaScript realms; top-level lexical declarations and intrinsic mutation are not fully isolated. During config bootstrap, re-entering the same feature singleton returns an empty object; ordinary recursive calls fail. Calls nest to a depth of 8. Hooks do not run inside called features.

The HTML report nests the called steps under the `call` step. See [Reports](reports.md).

## Tags

| Tag | Effect |
| --- | --- |
| `@ignore` | The scenario is skipped. |
| `@noload` | The scenario is skipped under `gi load` only. |
| `@env=name` | The scenario runs only when `--env` equals `name`. Several `@env` tags mean any of them. |
| `@envnot=name` | The scenario is skipped when `--env` equals `name`. |
| `@setup` | The scenario is not part of the run. `gi.setup()` runs it. `@setup=name` or a comma-separated list names it. |

`--tag` is an expression, applied after the tags above. Names are case-sensitive. The leading `@` is optional.

| Expression | Runs a scenario that |
| --- | --- |
| `@smoke` | has the tag |
| `@a, @b` or `@a or @b` | has either tag |
| `@a and @b` | has both |
| `not @slow` or `~@slow` | does not have the tag |
| `anyOf('a', 'b')` | has either |
| `allOf('a', 'b')` | has both |

Parentheses group terms. An invalid expression fails the file.

## Setup

```gherkin
@setup
Scenario:
  * def data = [{ name: 'Ada' }]

Scenario: use it
  * def rows = gi.setupOnce().data
```

`gi.setup()` runs the `@setup` scenario and returns a copy of its variables. `gi.setupOnce()` does that once per suite. The setup scenario's own steps run. Its background does not. A hook does not run inside setup, so a hook may call `gi.setupOnce()`. Setup cannot call setup.

An unnamed `@setup` is the default. `gi.setup('name')` selects `@setup=name`.

Dynamic outlines call this themselves: `Examples: | gi.setup().data |`. See [Data](data.md).

## Hooks

```javascript
function fn() {
  karate.configure('beforeScenario', function() { karate.set('started', true) })
  karate.configure('afterScenario', function() { karate.set('finished', true) })
  return {}
}
```

`beforeScenario` runs after config and before steps. `afterScenario` runs after the steps, including when they fail. The first error is kept. A hook set by a `configure` step in the scenario only affects `afterScenario`, because `beforeScenario` has already run. Put `beforeScenario` in config.

Neither hook runs inside a called feature or inside `@setup`.

## Additional lifecycle and scheduling

`afterFeature` runs once after a feature. `afterScenarioOutline` runs once after all selected rows of an outline finish, including parallel execution. `onStepFailure` receives information with `proceed()` and `stop()` decisions.

`@fail` expects scenario failure. `@lock=name` serializes scenarios sharing that name; `@lock=*` excludes all other scheduled scenarios in the suite. Lock waiting respects cancellation. `@report=false` suppresses step detail and print output, including called features. Runtime variables remain available to embedding callers. `@env` and `@envnot` accept comma-separated environment names.

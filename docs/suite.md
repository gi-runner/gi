# Suite

`gi test` with no path walks `features/`, then `examples/`, then the current directory. A directory argument walks every `.feature` file under it, skipping hidden directories and `vendor/`. One HTML report covers the whole run.

## Try a reusable feature

Save this as `greeting.feature`:

```gherkin
Feature: Greeting helper

Scenario: greet
  * def message = 'Hello, ' + name + '!'
```

Save this alongside it as `reuse.feature`:

```gherkin
Feature: Reuse a flow

Scenario: call a helper
  * def result = call read('greeting.feature') { name: 'Ada' }
  * match result.message == 'Hello, Ada!'
```

Run `gi test reuse.feature`. The caller passes, and `result.message` contains the helper's result. Passing the caller path explicitly avoids also running the helper as an independent test.

## Parallel

`--jobs` is how many features run at the same time. The default is one per CPU. Results stay in the order of the files, and the report is that order.

| Invocation | What runs at once |
| --- | --- |
| Several files, `--jobs 4` | Four features. Scenarios inside a feature stay in order. |
| One file, `--jobs 4` | Four scenarios of that file. |
| `--jobs 1` | One feature at a time, scenarios in order. |

Use `--jobs 1` when scenarios depend on each other's data. A single file runs scenarios concurrently when `--jobs` is greater than 1.

## Signal and listen

Two scenarios can exchange a value through the run's queue. Save this as `events.feature` and run `gi test events.feature --jobs 2`:

```gherkin
Feature: scenario events

Scenario: wait for an event
  * def event = gi.listen(5000)
  * match event == { status: 'ready' }

Scenario: publish an event
  * gi.signal({ status: 'ready' })
```

A signal sent before `listen` stays queued. Each listen consumes one value and fails if its timeout elapses. Scenarios overlap when `--jobs` is greater than 1. The queue belongs to that run; a separate `gi mock` process cannot signal it.

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

## Call a feature {#call}

```gherkin
* call read('login.feature')
* def session = call read('login.feature') { user: 'ada' }
* callonce read('login.feature')
```

| Form | Scope | Result |
| --- | --- | --- |
| `call read('login.feature')` | Shares caller variables and HTTP settings. | Variables are available to later steps. |
| `def result = call read('login.feature')` | Separate JavaScript realm. | Read returned variables from `result`. |
| `gi.call(path, arg)` | Separate realm; `gi.call(true, path, arg)` shares the caller. | Returns the called feature's variables. |

Assigned calls and setup isolate declarations and changes to built-ins. A returned function keeps the realm it closed over. Shared calls propagate persistent HTTP settings and cookie updates; isolated calls get a fork of the cookie jar. Transient request builders are cleared.

### Arguments and selection

Pass an object to bind its keys as variables, also available through `__arg`. A list of objects calls the feature once per item and returns a list of results. `__loop` is the zero-based item index, or `-1` for a single call.

| Target | Selection |
| --- | --- |
| `child.feature:12` or `child.feature:12:30` | Scenario or outline at those lines. |
| `child.feature@smoke` | Scenarios with that tag. |
| `@smoke` | Tagged scenarios in the current file. |

Called features may use `@ignore`; setup and environment exclusions still apply. Excluded dynamic outlines are not evaluated.

### Cached setup

`callonce` runs once per calling feature and resolved target; the first argument wins. `gi.callSingle(path, arg)` runs once per suite and accepts feature or JavaScript files. Add a `?suffix` to give a singleton a distinct cache key.

Cached data is copied and may contain JSON or native XML. Cached functions, including configured header and hook callbacks, retain their original runtime and live closure state. Native functions and live protocol handles cannot be cached.

??? note "Callback scope and cancellation"

    A returned function sees its captured variables, sibling functions and later assignments. Its `let` and `var` bindings remain in the originating runtime. Invocation respects the caller's cancellation, and `gi.info` identifies that caller.

    A parent function invoked from an isolated call still closes over the parent. It respects the isolated call's deadline; afterward, the parent keeps its own deadline. While a callback waits in `listen`, `pause`, `waitForHttp` or `waitForPort`, another scenario may call into that runtime.

### Configuration and lifecycle

Calls inherit configuration without re-running config files. Shared calls propagate header and hook settings and retain browser ownership. Assigned calls borrow an existing browser page without closing it. Cache hits replay cookie updates and use the original header and hook functions.

Calls nest to a depth of 8. Ordinary recursive calls fail; during configuration bootstrap, re-entering the same feature singleton returns an empty object. Hooks do not run inside called features.

The [HTML report](reports.md) nests called steps under the `call` step.

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

`gi.setup()` runs the `@setup` scenario in its own realm and returns the variables that scenario defined. Lexical bindings and built-in changes made there stay in that realm. `gi.setupOnce()` does that once per suite. The setup scenario's own steps run. Its background does not. A hook does not run inside setup, so a hook may call `gi.setupOnce()`. Setup cannot call setup.

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

## Common mistakes

| Symptom | Check |
| --- | --- |
| A helper is also run as a standalone test | Run the caller path explicitly or tag the helper `@ignore`; called features can still select it. |
| `beforeScenario` does not run after a `configure` step | Define it in config. The before hook has already run when scenario steps begin. |
| Scenarios depend on execution order | Use `--jobs 1`, or remove the dependency by giving each scenario its own data. |
| A cached login uses the first account's data | `callonce` and `callSingle` reuse the first result for their cache key. Use separate keys for separate identities. |
| Config changes do not appear in a call | Calls inherit configuration; they do not re-run config files. |

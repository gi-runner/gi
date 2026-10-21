# Language

A file is standard Gherkin. `Feature`, `Background`, `Scenario`, `Scenario Outline`, and `Examples` work as they do in Cucumber. The step keyword (`*`, `Given`, `When`, `Then`, `And`, `But`) is ignored. The first word of the step text is the command.

```gherkin
Feature: users
  Background:
    * url baseUrl
    * def email = 'ada-' + uuid() + '@example.com'

  Scenario: create
    * path 'users'
    * request { name: 'Ada', email: '#(email)' }
    * method post
    * status 201
    * match response.email == email
```

`Background` is copied in front of every scenario. A docstring belongs to the step above it.

## Variables

Expressions run in [Sobek](https://github.com/grafana/sobek), a pure-Go JavaScript runtime. `uuid()`, string concatenation, and object literals are JavaScript. Paths such as `response.users[*].id` are evaluated by gi, because that syntax is not JavaScript.

`true`, `false`, and `null` are literals. They are not variable names.

| Step | Result |
| --- | --- |
| `def name = expression` | Evaluates the right-hand side. |
| `text name = raw` | Stores the text. A docstring is kept as written. An inline value is trimmed. Nothing is evaluated. |
| `json name = expression` | Evaluates, then parses a string as JSON. |
| `copy name = expression` | Deep copy. |
| `set path = expression` | Writes into an existing object or list. |
| `remove path` | Deletes a key or index. |
| `replace name.token = value` | Replaces `<token>` inside the string variable `name`. A following table with `token` and `value` columns replaces many. |
| `table name` | Builds a list of objects from the following table. The first row is the header. |
| `print expression` | Writes to stdout and records the line on the step. |
| `assert expression` | Fails unless the expression is boolean `true`. |

`#(name)` inside a JSON or JavaScript literal is replaced with the variable before the expression runs.

Each scenario has its own runtime. An assigned `call` and `gi.setup()` run in a further realm; see [Suite](suite.md). `vu` and `iter` are `1` in a functional run. Under a user load profile, `vu` is the virtual user and `iter` counts its loops, starting at 1. Under a rate profile, `vu` numbers each start and `iter` stays 1.

## gi and karate

`karate` and `gi` refer to the same helper object. The global `read` is also available.

| Member | Behavior |
| --- | --- |
| `gi.config` | Detached snapshot of effective `timeout`, `readTimeout`, `connectTimeout`, `followRedirects` and `strictResponseParsing` settings. Callback configuration and other Java settings are not included. |
| `gi.env` | The value of `--env`. |
| `gi.read(path)` | Same as the global `read`. |
| `gi.get(name)` / `gi.set(name, value)` | Read or write a variable. `gi.set` is visible to later steps, including `match`. |
| `gi.configure(key, value)` | Same keys as the `configure` step. |
| `gi.callSingle(path)` | Runs a feature once per suite. See [Suite](suite.md). |
| `gi.setup()` / `gi.setupOnce()` | Runs the `@setup` scenario. See [Suite](suite.md). |
| `gi.map(list, fn)` | Returns a new list. `fn` receives each element. |
| `gi.filter(list, fn)` | Keeps elements for which `fn` is truthy. |
| `gi.merge(a, b, ...)` | Shallow-merges objects. Later keys replace earlier values, including nested objects. |
| `gi.append(list, ...items)` | Returns a new list, flattening list arguments by one level. |
| `gi.channel(kind, spec)` | Opens a WebSocket, gRPC, or Kafka link. See [Protocols](protocols.md). |
| `gi.pageText(selector)` | Text of an element in the open browser page. |

```gherkin
* def names = gi.map(users, function(x){ return x.name })
* def adults = gi.filter(users, function(x){ return x.age > 18 })
* def body = gi.merge(defaults, { name: 'Ada' })
* def ids = gi.append(ids, response.id)
```

`map` and `filter` do not mutate the input list.

## read

`read('file')` and `gi.read('file')` resolve a path and return its value.

| Path | Resolution |
| --- | --- |
| `classpath:a/b.json` | From the project root. |
| `this:a/b.json` | From the current feature directory. |
| `file:/abs/path` | Host path, used as given. |
| `/a/b.json` | From the project root. |
| `b.json` | The feature's directory, then the project root. |

The project root is the directory found by walking upward from the first feature path until `gi-config.js`, `karate-config.js`, or a base config exists, or the feature's directory when no config file exists.

| Extension | Return value |
| --- | --- |
| `.json` | Parsed JSON. Whole numbers are integers, so `36` matches `36`. |
| `.js` | The value of `fn` when the file declares `function fn`. Otherwise the completion value of the script. |
| `.feature` | A feature reference that can be assigned and used by `call`. |
| `.yaml` / `.yml` | Parsed YAML. |
| `.csv` | A list of objects with string cells. |
| `.xml` | The native XML document described in [Data](data.md). |
| `.png`, `.jpg`, `.jpeg`, `.gif`, `.pdf`, `.zip`, `.bin`, `.gz` | Bytes. |
| anything else | The file text. |

## configure

```gherkin
* configure timeout = 5000
* configure followRedirects = false
* configure continueOnStepFailure = true
* configure retry = { count: 5, interval: 200 }
* configure headers = function(){ return { Authorization: 'Bearer ' + token } }
* configure auth = { type: 'bearer', token: token }
```

`timeout` is milliseconds and applies to each HTTP attempt. `retry.interval` is milliseconds. Header and hook functions stay on the scenario runtime. The other keys are values. See [HTTP](http.md) for auth and retry, and [Suite](suite.md) for hooks.

`eval expression` and bare JavaScript statement steps execute in the scenario runtime. `string` and `bytes` convert values. `set` also accepts a table of paths and values.

Collection helpers include `appendTo`, `forEach`, `repeat`, `range`, `distinct`, `sort`, `filterKeys`, `mapWithKey`, `keysOf`, `valuesOf`, and `sizeOf`. Conversion helpers include `typeOf`, `lowerCase`, `pretty`, `toString`, `fromJson`, `fromString`, `toJson`, `toBytes`, `readAsString`, and `readAsBytes`. 

## Native path helpers

`karate.jsonPath(data, path)` supports recursive descent, filters, slices and unions within the native parser's grammar. `karate.xmlPath(document, xpath)` evaluates namespace-aware XPath. `karate.fromString(text)` recognizes JSON and XML, otherwise returning text. `karate.toJson(document)` returns the XML object view. XML mutation uses DSL XPath set/remove; JavaScript XML views are read-only.

## Runtime helpers

For cross-scenario events, see [signal and listen](suite.md#signal-and-listen). For report attachments, see [Reports](reports.md#attachments).

Use either `gi` or `karate` for these helpers:

| Helper | Behavior |
| --- | --- |
| `abort()` | Stops the remaining scenario steps without failing it. |
| `log(...values)` | Writes values to configured output; suppressed under load and `@report=false`. |
| `pause(milliseconds)` | Waits, respecting run cancellation. |
| `info` | Scenario name and feature directory/file name; `scenarioDescription` is currently empty. |
| `scenario` | Current scenario `name` and `line`. |
| `feature` | Current feature `name`, `fileName` and `parentDir`. |
| `write(value, path)` | Writes text, bytes, XML or JSON beneath the project `target/` directory and returns the full path. Absolute paths and `..` escapes are rejected. |
| `waitForHttp(url, options)` | Polls GET until status 200–399. Options: `timeout` (default 30000 ms) and `interval` (default 250 ms). |
| `waitForPort(host, port, timeout, interval)` | Waits for a TCP connection; timeout and interval default to 30000 and 250 ms. |
| `signal(value)` | Queues a value for `listen` in this run. |
| `listen(milliseconds)` | Returns the next queued value, or fails when the timeout elapses. |
| `embed(value, mime, name)` | Attaches text, JSON, or bytes to the current step in the report. |

```gherkin
* karate.log('Running', karate.info.scenarioName)
* karate.waitForHttp(baseUrl + '/health', { timeout: 5000, interval: 100 })
* def saved = karate.write({ status: 'ready' }, 'status.json')
```

For Go functions exposed to scenarios, see [Embedding](embedding.md).

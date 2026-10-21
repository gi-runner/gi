# JavaScript helpers

Call helpers on `gi`, for example `gi.map(users, fn)`. `karate` is an alias for `gi`, and `read()` is shorthand for `gi.read()`. Start with [feature files and variables](language.md) if you are new to the syntax.

| Member | Behavior |
| --- | --- |
| `gi.config` | Snapshot of effective `timeout`, `readTimeout`, `connectTimeout`, `followRedirects` and `strictResponseParsing` settings. Header and hook callbacks are not included. |
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

Save as `collections.feature` and run `gi test collections.feature`:

```gherkin
Feature: Collection helpers

Scenario: transform user data
  * def users = [{ name: 'Ada', age: 36 }, { name: 'Sam', age: 16 }]
  * def names = gi.map(users, user => user.name)
  * match names == ['Ada', 'Sam']
  * def adults = gi.filter(users, user => user.age >= 18)
  * match adults == [{ name: 'Ada', age: 36 }]
  * def body = gi.merge({ active: true }, { name: 'Ada' })
  * match body == { active: true, name: 'Ada' }
  * def ids = gi.append([1, 2], 3)
  * match ids == [1, 2, 3]
```

These helpers return new values. Use `appendTo` when you want to change the original list.

## More collection and conversion helpers

| Helper | Use |
| --- | --- |
| `appendTo(list, ...items)` | Append items to the original list. `append` returns a new list. |
| `forEach(value, fn)` | Visit list items or object entries. |
| `repeat(n, fn)` | Build a list from `fn(0)` through `fn(n - 1)`. |
| `range(start, end, step)` | Build a numeric list, including the end when the step reaches it. |
| `distinct(list)` | Remove duplicate values. |
| `sort(list, keyFn)` | Return a sorted copy, optionally using a key function. |
| `filterKeys(object, keys)` | Keep selected object keys. |
| `mapWithKey(list, key)` | Wrap each value in an object with that key. |
| `keysOf`, `valuesOf`, `sizeOf` | Inspect a collection. |
| `typeOf`, `lowerCase`, `pretty`, `toString` | Inspect or format values. |
| `fromJson`, `fromString`, `toJson`, `toBytes` | Convert data. |
| `readAsString`, `readAsBytes` | Read a file without parsing it. |

Call these on `gi`, for example `gi.range(1, 3)` returns `[1, 2, 3]`. JavaScript's own list methods, such as `users.map(user => user.name)`, also work.

## Native path helpers

`gi.jsonPath(data, path)` supports recursive descent, filters, slices and unions within the native parser's grammar. `gi.xmlPath(document, xpath)` evaluates namespace-aware XPath. `gi.fromString(text)` recognizes JSON and XML, otherwise returning text. `gi.toJson(document)` returns the XML object view. XML mutation uses DSL XPath set/remove; JavaScript XML views are read-only.

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
| `waitForHttp(url, options)` | Polls GET until status 200-399. Options: `timeout` (default 30000 ms) and `interval` (default 250 ms). |
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

# Limits

- No JVM integration (`Java.type`, JARs or JUnit extensions).
- XML JavaScript views are read-only. Change a document with `set name /xpath` or a path/value table. `xml name = { root: { child: 'value' } }` builds a document. Jayway-only JSONPath functions are absent.
- A function returned through `call`, `callonce`, or `callSingle` keeps calling the runtime that created it. Scenario variables, including sibling functions, `null`, and later assignments, stay shared by those functions. A lexical `let` stays with the function that created it. The call is cancelled when the caller is cancelled, and `karate.info` reports the caller. While that function is inside `listen`, `pause`, `waitForHttp`, or `waitForPort`, another scenario can call back into the same runtime. A `configure` header or hook function is kept the same way. An assigned call restores enumerable additions to `Object`, `Array`, and `String` prototypes. Called features still share the scenario runtime.
- The browser requires Chrome. Frames, windows, dialogs, uploads, interception and advanced input chains are not implemented.
- WebSocket supports text messages. gRPC supports unary `call` and streaming `open`, both through server reflection. Kafka supports record send and receive, PLAIN and SCRAM SASL, and TLS. Consumer groups and offset controls are not implemented.
- OAuth2 supports client credentials. NTLM and Java keystore formats are not implemented; TLS certificates use PEM.
- `karate.signal` queues a value for the current run. `karate.listen(milliseconds)` waits for the next one and returns as soon as the value, the timeout, or cancellation arrives. A separate `gi mock` process does not share that queue. Image comparison and templating are not implemented. `karate.embed` attaches text or bytes to the step or lifecycle hook that produced them, and screenshots are attached as PNG. Cucumber JSON includes those attachments.

See [Data](data.md), [Suite](suite.md), [HTTP](http.md) and [Protocols](protocols.md) for the supported APIs and their behavior.

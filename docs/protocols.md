# Protocols

WebSocket, gRPC, Kafka, and the browser are opened from the scenario. The engine does not dial them. `gi test` and `gi load` do.

## gi.channel

`gi.channel(kind, spec)` returns an object. Call its methods from an expression. Methods can be used in bare JavaScript steps or assigned when they return a value.

```gherkin
* def sock = gi.channel('websocket', { url: wsUrl })
* def sent = sock.send('ping')
* def got = sock.receive()
* match got == 'ping'
```

| Kind | Spec | Methods |
| --- | --- | --- |
| `websocket` or `ws` | `{ url }` | `send` and `receive` text or binary messages. |
| `grpc` | `{ addr }` | `call(method, body)` for a unary method. `open(method)` for a stream. Plaintext by default; optional TLS and metadata are described below. |
| `kafka` | `{ brokers, topic }` | `send` and `receive` one record on that topic. `brokers` is a host, a comma-separated list, or a list. Optional SASL and TLS are described below. |

```gherkin
* def rpc = gi.channel('grpc', { addr: addr })
* def res = rpc.call('grpc.health.v1.Health/Check', { service: 'gi' })
* match res.status == 'SERVING'
```

The gRPC method is `package.Service/Method`. A leading slash is optional. gi asks the server for its schema by reflection, or reads a `FileDescriptorSet` from `descriptor` when the server does not reflect. It sends the JSON body and returns the JSON response. Enums come back as names.

```gherkin
* def watch = rpc.open('grpc.health.v1.Health/Watch')
* watch.send({ service: 'gi' })
* watch.closeSend()
* def first = watch.receive()
* match first.status == 'SERVING'
* watch.close()
```

`open` returns `send`, `receive`, `closeSend`, and `close`. A server stream sends one message, then receives. A client stream sends each message, calls `closeSend`, and receives one response. A bidirectional stream sends and receives until `closeSend`, then receives until the server closes. `call` rejects a streaming method. `receive` after the server closes fails the step. Opening a stream respects the operation timeout, without applying that opening deadline to the established stream. A send or receive timeout cancels that stream.

Kafka `send` of a string is the record value. Any other value is sent as JSON. `receive` returns the next record on the topic, as text. With no `offset`, that starts at the beginning of the topic. `offset` is `start`, `end`, or an absolute integer. `group` joins that consumer group and commits each record when `receive` returns it. A later link with the same group continues after those commits. `end` waits for records produced after the link starts.

The link closes when the scenario ends.

## Browser

`driver` opens a URL in headless Chrome. Actions accept CSS or XPath selectors; single-element locators also support Karate text selectors. `gi.pageText` reads an element.

```gherkin
* driver pageUrl
* input '#name' = 'Ada'
* click '#go'
* def got = gi.pageText('#out')
* match got == 'Ada'
```

`click` and `input` fail with `driver is not open` until `driver` has run. The page closes when the scenario ends. A machine without Chrome fails the `driver` step.

### Actions and locators

After opening `driver`, global helpers and driver methods support `click`, `input`, `inputFile`, `clear`, `select`, `focus`, `scroll`, `text`, `html`, `value`, `attribute`, `exists`, `enabled`, `locate`, `locateAll`, `waitFor`, `waitForText`, `waitForEnabled`, `waitForUrl`, string `waitUntil`, `script` and `screenshot`. Navigation includes back, forward and refresh; `driver.url` and `driver.title` are readable properties. Locators support CSS, XPath and single-element Karate text selectors. Located elements expose fluent actions and read properties.

Screenshots return PNG bytes and are attached to the step that took them. Shared calls retain browser ownership; isolated calls borrow an existing page without closing it.

`driver.frame(selector)` runs later locators inside that frame's document. `driver.frame()` returns to the top document. The frame has to be same-origin, so `srcdoc` and a page on the same host work. `driver.dialog(false)` dismisses the next alert, confirm, or prompt. `driver.dialog(true, 'Ada')` accepts it and types into a prompt. `driver.dialogText()` is the message of the last dialog. `driver.intercept('/secret', { status: 200, body: 'hidden' })` answers a later request whose URL contains that text. The rule belongs to that window. A window opened afterward keeps the rules already set and enables interception on its own target. `driver.intercept()` clears the current window's rules. `driver.window(url)` opens that URL in a new window and switches to it. `driver.window()` lists `{ index, url, title }`. `driver.window(n)` switches to that index.

Providers, a full browser cookie jar, and the full fluent retry API remain incomplete.

## gRPC TLS and metadata

```gherkin
* def spec = { addr: 'api.example.com:443', tls: true, metadata: { authorization: 'Bearer #(token)' } }
* def rpc = karate.channel('grpc', spec)
```

For private trust or mutual TLS, use `tls: { caCert: '/path/ca.pem', serverName: 'api.example.com', cert: '/path/client.pem', key: '/path/client-key.pem' }`. Certificate paths are host paths. `cert` and `key` must be provided together. Server certificates and hostnames are verified; system trust is used unless a supplied CA augments it. TLS requires version 1.2 or later. Metadata is sent on reflection, unary calls, and streams. Reflection schemas are cached per link. `call` rejects a streaming method.

`descriptor` is the path of a file written by `protoc --descriptor_set_out`. The file is a `FileDescriptorSet`. When it is set, gi does not call reflection, and a service missing from the file fails the call.

## Kafka SASL and TLS

```gherkin
* def bus = gi.channel('kafka', { brokers: brokers, topic: 'events', sasl: { mechanism: 'SCRAM-SHA-256', username: 'ada', password: 'secret' }, tls: true })
```

`mechanism` is `PLAIN`, `SCRAM-SHA-256`, or `SCRAM-SHA-512`. It defaults to `PLAIN`. `username` and `password` are required when `sasl` is set. TLS uses the same shape as gRPC: `true`, or `{ caCert, serverName, cert, key }`.

```gherkin
* def bus = gi.channel('kafka', { brokers: brokers, topic: 'events', group: 'readers', offset: 'end' })
```

## WebSocket options

```gherkin
* def socket = gi.channel('websocket', { url: wsUrl, headers: { Authorization: 'Bearer ' + token }, subprotocols: ['events'], maxMessageSize: 1048576 })
* socket.send('hello')
* def text = socket.receive()
* socket.send(read('payload.bin'))
* def bytes = socket.receive()
```

Strings use text messages; byte values use binary messages. `receive()` returns a string or bytes according to the incoming message type. Handshake `headers` accepts scalar or list values. `subprotocols` is a list of strings. `maxMessageSize` is an optional positive integer in bytes; otherwise the WebSocket library's default applies. An oversized message fails receive.

## Browser uploads

```gherkin
* driver pageUrl
* inputFile('#document', 'report.pdf')
* driver.inputFile('#photos', ['front.png', 'back.png'])
* locate('#document').inputFile('replacement.pdf')
* inputFile('#document', [])
```

Upload paths resolve like `read()`: relative to the feature or project root, with `this:`, `classpath:` and `file:` prefixes. Files must exist and be regular files. Lists support multiple inputs; an empty list clears the selection. CSS, XPath and single-element text locators use the same lookup as other actions.

gi preserves Chrome's default sandbox settings. On a restricted CI host that cannot run it, `GI_CHROME_NO_SANDBOX=true` explicitly disables it. The Linux CI job uses this workaround; regular runs do not disable the sandbox automatically.

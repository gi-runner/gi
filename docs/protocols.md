# Messaging

Use WebSocket, gRPC and Kafka from feature files in `gi test`, `gi load`, or the Go API. Each link closes when its scenario ends.

## Before you run

Start the service you are testing and supply its address in config or with `--var`, such as `--var wsUrl=ws://127.0.0.1:8080/echo`. The examples below are scenario steps; put them under `Feature:` and `Scenario:` headings. WebSocket's example assumes an echo server, gRPC's assumes the health service, and Kafka's requires the named topic.

## gi.channel

`gi.channel(kind, spec)` returns an object. Call its methods from an expression. Methods can be used in bare JavaScript steps or assigned when they return a value.

```gherkin
* def sock = gi.channel('websocket', { url: wsUrl })
* def sent = sock.send('ping')
* def got = sock.receive()
* match got == 'ping'
```

An echo server returns `ping`, so the match passes. A connection, receive, or assertion failure fails the scenario. Each operation uses the configured timeout.

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

See [Browser automation](browser.md) for Chrome actions, locators, frames, windows and screenshots.

## gRPC TLS and metadata

```gherkin
* def spec = { addr: 'api.example.com:443', tls: true, metadata: { authorization: 'Bearer #(token)' } }
* def rpc = karate.channel('grpc', spec)
```

For private trust or mutual TLS, use `tls: { caCert: '/path/ca.pem', serverName: 'api.example.com', cert: '/path/client.pem', key: '/path/client-key.pem' }`. Certificate paths are host paths. `cert` and `key` must be provided together. Server certificates and hostnames are verified. A supplied CA is added to the system trust pool. HTTP, gRPC, and Kafka share that PEM loading. TLS requires version 1.2 or later. Metadata is sent on reflection, unary calls, and streams. Reflection schemas are cached per link. `call` rejects a streaming method.

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

See [browser file uploads](browser.md#file-uploads).

## Common mistakes

| Symptom | Check |
| --- | --- |
| A channel fails to connect | Confirm the broker/service address is reachable from the runner, including container port mappings. |
| gRPC cannot find the schema | Enable server reflection or set `descriptor` to a FileDescriptorSet containing the service and its imported types. |
| `rpc.call` rejects a method | Streaming methods use `rpc.open`; `call` is unary only. |
| Kafka `receive` waits at offset `end` | Produce after opening the link. `end` skips records that already exist. |
| A Kafka group skips previously read records | Successful `receive` calls commit them. Use a different group for a fresh consumer history. |
| A WebSocket assertion sees bytes instead of text | The incoming message type determines the return type. Binary frames return bytes. |

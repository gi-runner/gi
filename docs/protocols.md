# Messaging

Test WebSocket messages, gRPC services and Kafka topics from a feature file. Start the service first; gi connects to it but does not start it. Browser tests have their own [guide](browser.md).

## Open a channel {#gichannel}

`gi.channel(kind, options)` opens a connection and returns an object with protocol-specific methods. gi closes the connection when the scenario ends. These calls work in `gi test`, `gi load` and the Go API.

| Kind | Required options | Operations |
| --- | --- | --- |
| `websocket` (or `ws`) | `url` | `send`, `receive` |
| `grpc` | `addr` | `call` for one request/response; `open` for streaming |
| `kafka` | `brokers`, `topic` | `send`, `receive` |

## WebSocket

Save as `echo.feature`. This example needs a WebSocket echo server that sends each message back:

```gherkin
Feature: WebSocket echo

Scenario: exchange a message
  * def socket = gi.channel('websocket', { url: wsUrl })
  * socket.send('ping')
  * def message = socket.receive()
  * match message == 'ping'
```

Run `gi test echo.feature --var wsUrl=ws://127.0.0.1:8080/echo`, replacing the URL with your server's address. The scenario passes when it receives `ping`; a connection, timeout or assertion failure fails the test.

### WebSocket options {#websocket-options}

| Option | Use |
| --- | --- |
| `headers` | Handshake headers; values can be strings or lists. |
| `subprotocols` | List of supported subprotocol names. |
| `maxMessageSize` | Positive maximum incoming message size, in bytes. Otherwise the client library's default applies. |

Strings use text messages. Byte values, such as `read('payload.bin')`, use binary messages. `receive()` returns text or bytes according to the incoming frame. An oversized message fails receive.

## gRPC

This example calls the standard gRPC health service:

```gherkin
* def rpc = gi.channel('grpc', { addr: '127.0.0.1:50051' })
* def reply = rpc.call('grpc.health.v1.Health/Check', { service: '' })
* match reply.status == 'SERVING'
```

Use your service's method and body. Methods are named `package.Service/Method`; a leading slash is optional. Requests and responses use JSON objects, and enum values return as names.

The server must expose reflection, which lets gi discover its message types. Without reflection, supply a descriptor file:

```gherkin
* def rpc = gi.channel('grpc', { addr: grpcAddr, descriptor: 'service.pb' })
```

Generate it with `protoc --include_imports --descriptor_set_out=service.pb ...`. It must contain the service and its imported message types. When `descriptor` is set, gi does not use reflection.

### Streams

`call` handles one request and one response. For a streaming method, use `open`:

```gherkin
* def watch = rpc.open('grpc.health.v1.Health/Watch')
* watch.send({ service: '' })
* watch.closeSend()
* def first = watch.receive()
* match first.status == 'SERVING'
* watch.close()
```

| Stream type | Sequence |
| --- | --- |
| Server streaming | Send one request, close the send side, then receive responses. |
| Client streaming | Send requests, close the send side, then receive the response. |
| Bidirectional | Send and receive messages; close the send side when finished sending. |

Receiving after the server closes fails the step. A send or receive timeout cancels the stream. The timeout used to open it does not become a lifetime limit for the established stream.

### gRPC TLS and metadata {#grpc-tls-and-metadata}

Connections use plaintext unless you set `tls`. For a server using a trusted certificate:

```gherkin
* def metadata = { authorization: 'Bearer ' + token }
* def rpc = gi.channel('grpc', { addr: 'api.example.com:443', tls: true, metadata: metadata })
```

Metadata is sent on reflection requests, unary calls and streams. For a private CA or mutual TLS, use `tls: { caCert, serverName, cert, key }`. Certificate paths are host paths; `cert` and `key` must be supplied together. Certificates and hostnames are verified, supplied CAs supplement system trust, and TLS 1.2 or later is required.

## Kafka

With a running broker and an existing topic:

```gherkin
* def bus = gi.channel('kafka', { brokers: brokers, topic: 'events', offset: 'end' })
* bus.send('hello')
* def message = bus.receive()
* match message == 'hello'
```

`brokers` can be one address, a comma-separated string, or a list. This example assumes no other producer writes to the topic. A string is sent as text; other values are JSON-encoded. `receive()` returns the next record's value as text.

| Option | Meaning |
| --- | --- |
| `offset: 'start'` | Start at the beginning; this is the default. |
| `offset: 'end'` | Read records produced after opening the link. |
| `offset: 42` | Start at an absolute offset. |
| `group: 'readers'` | Join a consumer group. Each successful receive commits that record; later links in the group continue from its commits. |

### Kafka SASL and TLS {#kafka-sasl-and-tls}

```gherkin
* def credentials = { mechanism: 'SCRAM-SHA-256', username: 'ada', password: password }
* def bus = gi.channel('kafka', { brokers: brokers, topic: 'events', sasl: credentials, tls: true })
```

SASL supports `PLAIN` (the default), `SCRAM-SHA-256` and `SCRAM-SHA-512`. Both username and password are required. Kafka accepts the same `tls` options as gRPC.

## Common mistakes

| Symptom | Check |
| --- | --- |
| A channel cannot connect | Confirm the service is running and reachable from the runner, including container port mappings. |
| gRPC cannot find a method | Enable reflection or provide a descriptor containing the service and its imports. |
| Kafka receive waits at `end` | Send after opening the link; earlier records are skipped. |
| A Kafka group skips old records | Successful receives already committed them. Use a new group for a separate consumer history. |
| A WebSocket match sees bytes | Binary frames return bytes, not strings. |

## Browser

See [Browser automation](browser.md). For the previous upload section, see [file uploads](browser.md#file-uploads). <span id="browser-uploads"></span>

# Limits

The runner supports API tests, HTTP mocks and load tests in one native binary. The following capabilities remain outside its current scope:

| Area | Current limits |
| --- | --- |
| Java integration | No `Java.type`, JAR loading or JUnit extensions. Go functions can be registered through the [embedding API](embedding.md). |
| XML and JSONPath | XML's JavaScript view is read-only; use XPath set/remove or path/value tables. Jayway-specific JSONPath functions are not implemented. |
| JavaScript scopes | Assigned calls and setup run in their own JavaScript realm. Lexical bindings and built-in changes stay there, and a returned function keeps that realm. A caller function invoked from it stops at that call's deadline. A bare call and `karate.call(true, ...)` still share the caller's runtime. |
| Browser | Chrome is required. Frames, dialogs, extra windows, request interception, and file uploads are supported. A frame must be same-origin. Providers, a full browser cookie jar, and the full fluent retry API are not implemented. |
| Messaging | WebSocket supports text and binary messages. gRPC uses server reflection or a FileDescriptorSet. Kafka supports TLS/SASL, a consumer group, and a start, end, or absolute offset. |
| Authentication | OAuth2 supports client credentials. NTLM and Java keystores are not implemented; certificates use PEM. |
| Async events | Signal/listen queues are local to a run. A separate mock process cannot signal that queue. |
| Visual/server features | Image comparison and HTML templating/server applications are not implemented. HTTP feature mocks are supported. |

Supported APIs and their behavior are documented in [Data](data.md), [Suite](suite.md), [HTTP](http.md) and [Protocols](protocols.md).

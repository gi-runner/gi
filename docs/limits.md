# Limits

| Area | Current limits |
| --- | --- |
| Java integration | No `Java.type`, JAR loading or JUnit extensions. Go functions can be registered through the [embedding API](embedding.md). |
| XML and JSONPath | XML's JavaScript view is read-only; use XPath set/remove or path/value tables. Jayway-specific JSONPath functions are not implemented. |
| Browser | Chrome is required. Frames, dialogs, extra windows, request interception, and file uploads are supported. A frame must be same-origin. Providers, a full browser cookie jar, and the full fluent retry API are not implemented. |
| Service schemas | gRPC requires server reflection or a descriptor file containing the service. |
| Authentication | OAuth2 supports client credentials, the password grant, and refresh tokens. NTLM and Java keystores are not implemented; certificates use PEM. |
| Async events | Signal/listen queues are local to a run. A separate mock process cannot signal that queue. |
| Visual/server features | Image comparison and HTML templating/server applications are not implemented. HTTP feature mocks are supported. |
| Spec files | `gi spec` writes a feature from an acceptance scenario that names one HTTP call, and from an OpenAPI contract. Prose without a call, a required value or credential without an example, and requirements marked as needing clarification, are reported and not executed. |

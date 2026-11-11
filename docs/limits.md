# Limits

| Area | Current limits |
| --- | --- |
| Java integration | No `Java.type`, JAR loading or JUnit extensions. Go functions can be registered through the [embedding API](embedding.md). |
| Scenario state | Each scenario has its own variables and cookie jar. Scenarios can share the HTTP transport. |
| XML and JSONPath | XML's JavaScript view is read-only; use XPath set/remove or path/value tables. Jayway-specific JSONPath functions are not implemented. |
| Browser | Chrome is required. Frames, dialogs, extra windows, request interception, and file uploads are supported. A frame must be same-origin. Providers, a full browser cookie jar, and the full fluent retry API are not implemented. |
| Service schemas | gRPC requires server reflection or a descriptor file containing the service. |
| Authentication | OAuth2 supports client credentials, the password grant, and refresh tokens. NTLM and Java keystores are not implemented; certificates use PEM. |
| Async events | Signal/listen queues are local to a run. A separate mock process cannot signal that queue. |
| Visual/server features | Image comparison and HTML templating/server applications are not implemented. HTTP feature mocks are supported. |
| Spec files | `gi spec` writes a feature from an acceptance scenario that names one HTTP call, and from an OpenAPI contract. It does not plan the change or write the service. Prose without a call, a required value or credential without an example, and requirements marked as needing clarification, are reported and not executed. |
| Load generation | One process uses every core on a machine. On an Apple M4, 100 virtual users held 12,436 requests/s for 30 seconds against a local echo. RSS was 111 MB and every request passed. A run that asked for 10,000 starts/s completed 9,794 and missed 6,098 starts. `gi load plan` and `--part` split a profile across machines; `gi load merge` judges the combined histograms. The summary prints that machine's CPU, memory, and goroutines. |
| Load reports | Histograms and aggregates. A load report does not store every HTTP exchange. |

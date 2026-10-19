# Reports

```bash
gi test examples/users.feature --var baseUrl=http://127.0.0.1:18080 \
  --report report.html --junit junit.xml --json report.json --cucumber cucumber.json
```

The same flags work on `gi load`. Load HTML adds the latency table and the thresholds. It does not log every request.

## HTML

The file is self-contained. It uses no external assets.

Each scenario shows its steps, the step error, the match diff, and the HTTP exchange: method, URL, status, request headers, request body, response headers, and response body. JSON bodies are indented.

A `call` step is a tree. The called feature's steps are nested under it, including calls that feature made.

The timeline places every scenario that ran on one axis, from the start of the suite to the end. Overlapping bars are features that ran at the same time. A green bar passed. A red bar failed. Skipped scenarios are omitted.

## JUnit and JSON

JUnit is a `testsuites` document. One `testsuite` per feature. Duration is seconds. A skipped scenario is a skipped testcase. A failure carries the scenario error.

JSON is the same structure as the HTML view. Durations are milliseconds. The timeline is included for a functional run.

Cucumber JSON is one feature object per file. Step duration is nanoseconds. A passed step is `passed`. A failed step is `failed` and carries `error_message`. Expected failures accepted by `@fail` export as passed. Scenario failures without a failed step (including an unexpectedly passing `@fail`) export a failed `after` hook so consumers preserve the runner outcome.

`karate.embed(value, mime, name)` and `screenshot()` add attachments to the step that produced them. The same applies to `beforeScenario`, `afterScenario`, `afterScenarioOutline`, `afterFeature`, and `onStepFailure`: the attachment stays on that hook step. The HTML report shows text inline and images inline. JSON stores the bytes as base64. Cucumber JSON stores them on the step as `embeddings`. Attachments from nested feature calls are included on the enclosing call step.

## OpenTelemetry

`--otel-endpoint` posts an OTLP/HTTP JSON trace of the functional run. The collector must accept `application/json`. A failure to export is a warning. It does not fail the run. Load reports send the aggregated endpoint spans, not one span per request.

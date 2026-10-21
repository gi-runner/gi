# Reports

Use HTML to investigate a run, JUnit for CI test results, or JSON for another tool. Reports are written only when you request them.

## Try it

With `hello.feature` and the mock from the [quick start](quickstart.md):

```bash
gi test hello.feature --var baseUrl=http://127.0.0.1:18081 \
  --report report.html --junit junit.xml --json report.json --cucumber cucumber.json
```

A passing run exits `0` and writes all four files in the current directory. Open `report.html` locally, upload `junit.xml` to your CI test viewer, or use the JSON formats in other tools. A scenario failure still produces reports and exits `1`.

The same flags work on `gi load`. Load HTML adds the latency table, open-loop scheduled latency, missed and late starts, and the thresholds. It does not log every request. Scheduled latency runs from the due time until the HTTP response is recorded. It is not the HTTP percentile.

## HTML

The file is self-contained. It uses no external assets.

Each scenario shows its steps, the step error, the match diff, and the HTTP exchange: method, URL, status, request headers, request body, response headers, and response body. JSON bodies are indented.

A `call` step is a tree. The called feature's steps are nested under it, including calls that feature made.

The timeline places every scenario that ran on one axis, from the start of the suite to the end. Overlapping bars are scenarios that ran at the same time. A green bar passed. A red bar failed. Skipped scenarios are omitted.

## JUnit and JSON

JUnit is a `testsuites` document. One `testsuite` per feature. Duration is seconds. A skipped scenario is a skipped testcase. A failure carries the scenario error.

JSON is the same structure as the HTML view. Durations are milliseconds. The timeline is included for a functional run.

Cucumber JSON uses Cucumber's feature/step format. Choose it when another tool expects Cucumber results.

??? note "Cucumber result mapping"

    Cucumber JSON is one feature object per file. Step duration is nanoseconds. A passed step is `passed`. A failed step is `failed` and carries `error_message`. Expected failures accepted by `@fail` export as passed. Scenario failures without a failed step (including an unexpectedly passing `@fail`) export a failed `after` hook so consumers preserve the runner outcome.

## Attachments

```gherkin
* gi.embed({ orderId: 42 }, 'application/json', 'created order')
* gi.embed('payment accepted', 'text/plain', 'receipt')
```

`gi.embed(value, mime, name)` and `screenshot()` add attachments to the step that produced them. Attachments created by a hook stay on that hook's report step. Each attachment is limited to 4 MiB. HTML displays text and PNG, JPEG, GIF and WebP images inline. JSON stores the bytes as base64. Cucumber JSON stores them on the step as `embeddings`. Attachments from nested feature calls are included on the enclosing call step.

## OpenTelemetry

`--otel-endpoint` sends traces to an OpenTelemetry collector using OTLP over HTTP with JSON. The collector must accept `application/json`. A failure to export is a warning. It does not fail the run. Load reports send the aggregated endpoint spans, not one span per request.

## Common mistakes

| Symptom | Check |
| --- | --- |
| No report file | Reports are opt-in. Pass the appropriate flag and a writable path. Argument and parse errors can stop a run before reports are written. |
| CI loses reports on failure | Upload artifacts with `if: always()`; see [CI and automation](automation.md). |
| Load HTML has no per-request detail | Load reports keep aggregates rather than every exchange. Reproduce with `gi test` for step-level debugging. |
| An attachment is rejected | Each attachment must be at most 4 MiB. |
| An image is not shown inline | Use PNG, JPEG, GIF or WebP with the matching MIME type. |

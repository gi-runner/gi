---
template: home.html
hide:
  - navigation
  - toc
search:
  exclude: true
---

## Start here

Jump straight to a working example or a core feature.

<div class="gi-topic-grid" markdown="1">

- [**Your first test**<br>Start a local mock, make a request, and check the response.](quickstart.md)
- [**JSON assertions**<br>Match complete responses with schemas, predicates, and fuzzy markers.](match.md)
- [**Reusable flows**<br>Share login, setup, and helpers with feature calls.](suite.md#call)
- [**Data-driven tests**<br>Run a scenario for each table row or generated input.](data.md#scenario-outline)
- [**Load thresholds**<br>Set a request rate and fail on latency or errors.](load.md#metrics)
- [**Tests in CI**<br>Run a tagged suite and keep its reports when a job fails.](automation.md)

</div>

## Explore the documentation

Browse by section, from your first request to running gi inside Go.

<div class="gi-section-grid" markdown="1">

<section markdown="1">

### Get started

Install gi and run a working example against a local mock.

[Install](install.md) · [Quick start](quickstart.md)


</section>

<section markdown="1">

### Write tests

Build requests, validate responses, and work with test data.

[Language](language.md) · [HTTP, GraphQL and SOAP](http.md) · [Assertions](match.md) · [JSON, XML and data](data.md) · [Calls and configuration](suite.md)


</section>

<section markdown="1">

### Run tests

Select scenarios, run in parallel, and inspect the results.

[Command line](cli.md) · [Parallel execution](suite.md#parallel) · [Reports](reports.md) · [CI and automation](automation.md)


</section>

<section markdown="1">

### Test more

Reuse feature files under load, stub dependencies, or exercise a browser and messaging services.

[Load testing](load.md) · [HTTP mocks](mock.md) · [Browser automation](browser.md) · [WebSocket, gRPC and Kafka](protocols.md)


</section>

<section markdown="1">

### Use gi from Go

Run features in your application or tests, and expose Go helpers to scenarios.

[Embedding API](embedding.md) · [Register a helper](embedding.md#register-a-helper)


</section>

<section markdown="1">

### Know the boundaries

Check current limits or report a problem with a small reproducer.

[Limits](limits.md) · [GitHub Issues](https://github.com/gi-runner/gi/issues)


</section>

</div>

# Your first test

Start a local API, test its response, then run the same test under load.

## Install

```bash
go install github.com/gi-runner/gi/cmd/gi@latest
```

See [Install](install.md) to build from source.

## Start a mock API

Save this as `hello.mock.feature`:

```gherkin
Feature: Hello API

Scenario: greet
  * methodIs GET
  * pathMatches '/hello'
  * def response = { message: 'Hello, Ada!' }
```

Start the server:

```bash
gi mock hello.mock.feature --addr 127.0.0.1:18081
```

## Write a test

Save this as `hello.feature`:

```gherkin
Feature: Hello API

Scenario: get a greeting
  * url baseUrl
  * path 'hello'
  * method get
  * status 200
  * match response == { message: 'Hello, Ada!' }
```

In another terminal, run:

```bash
gi test hello.feature --var baseUrl=http://127.0.0.1:18081 --report report.html
```

Open `report.html` to inspect the steps and HTTP exchange. Change the expected message and run again to see a match failure.

## Run under load

```bash
gi load hello.feature --var baseUrl=http://127.0.0.1:18081 \
  --vus 10 --duration 10s --report load.html
```

The assertions stay the same. The load report adds throughput, latency percentiles and errors. Stop the mock with Ctrl+C when finished.

## Next steps

- [HTTP requests](http.md): headers, authentication, multipart, retry and SOAP.
- [Assertions](match.md): validate complete responses with schemas and predicates.
- [Reuse and configuration](suite.md): calls, setup, tags and parallel execution.
- [Load testing](load.md): rates, warmup and pass/fail thresholds.

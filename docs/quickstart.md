# Your first test

Start a local mock API, test its response, then run the same test under load. You need Go 1.27.2 or newer and two terminals; no API deployment or Chrome is needed.

Create an empty directory for the two files below. Keep both terminals in that directory.

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

The command exits with status `0` and marks `get a greeting` as passed. It writes `report.html` in your current directory.

Open `report.html` to inspect the steps and HTTP exchange. Change the expected message and run again to see a match failure.

## Run under load

```bash
gi load hello.feature --var baseUrl=http://127.0.0.1:18081 --vus 10 --duration 10s --report load.html
```

The assertions stay the same. The load report adds throughput, latency percentiles and errors. Stop the mock with Ctrl+C when finished.

## If it fails

| Symptom | Check |
| --- | --- |
| Connection refused | Keep the mock running in the first terminal. The test URL must use the same port. |
| `baseUrl` is not defined | Include `--var baseUrl=http://127.0.0.1:18081`. Variable names are case-sensitive. |
| HTTP 404 | The mock route is `/hello`; use `path 'hello'`. |
| Match failure | Open the report for the actual response and failing path. Restore the expected message before trying load mode. |

## Next steps

- [HTTP requests](http.md): headers, authentication, multipart, retry and SOAP.
- [Assertions](match.md): validate complete responses with schemas and predicates.
- [Reuse and configuration](suite.md): calls, setup, tags and parallel execution.
- [Load testing](load.md): rates, warmup and pass/fail thresholds.

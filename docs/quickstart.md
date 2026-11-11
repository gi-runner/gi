# Your first test

One feature file is a functional test and a load test.

```bash
gi demo
```

`gi demo` starts a greeting mock, checks the response, then runs that same check for two seconds with four users. It exits 0 and prints `demo passed`.

Install gi with the [install script](install.md) or, when Go 1.27.2 or newer is already on the machine:

```bash
go install github.com/gi-runner/gi/cmd/gi@latest
gi demo
```

## Write the same test yourself

`gi init` writes `hello.feature` and `hello.mock.feature` in the current directory. The steps below are those files. You need two terminals and no Chrome.

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

The command exits with status `0` and writes `report.html` in the current directory.

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

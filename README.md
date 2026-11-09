<p align="center">
  <img src="docs/mascot.png" alt="gi" width="180">
</p>

<h1 align="center">gi</h1>
<p align="center"><strong>Write API tests once. Run them as functional tests and load tests.</strong><br>Karate for Go. One native binary. No JVM.</p>

<p align="center">
  <a href="https://go.dev/dl/"><img src="https://img.shields.io/badge/Go-1.27.2-00ADD8?logo=go&logoColor=white" alt="Go 1.27.2"></a>
  <a href="https://github.com/gi-runner/gi/actions/workflows/ci.yml"><img src="https://github.com/gi-runner/gi/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue" alt="Apache 2.0"></a>
  <a href="https://gi-runner.github.io/gi/"><img src="https://img.shields.io/badge/docs-online-009688" alt="Documentation"></a>
  <a href="https://pkg.go.dev/github.com/gi-runner/gi"><img src="https://pkg.go.dev/badge/github.com/gi-runner/gi.svg" alt="Go Reference"></a>
</p>

**[Documentation](https://gi-runner.github.io/gi/)** · [Quick start](https://gi-runner.github.io/gi/quickstart/) · [Examples](examples/)

Keep requests, test data and assertions in one feature file. Run it with `gi test` for regression testing, then `gi load` to check the same behavior under load—without maintaining a separate load script.

[REST, GraphQL and SOAP](https://gi-runner.github.io/gi/http/) · [Browser automation](https://gi-runner.github.io/gi/browser/) · [WebSocket, gRPC and Kafka](https://gi-runner.github.io/gi/protocols/)

```bash
brew install gi-runner/tap/gi
gi demo
```

`gi demo` checks a greeting, then runs that same check under load. [Install](https://gi-runner.github.io/gi/install/) also covers the shell script, Go, and editors.

|  | gi | Karate | godog | k6 |
| --- | --- | --- | --- | --- |
| API test written in the feature file | yes | yes | Go steps | a script |
| That same file is the load test | yes | Gatling |  | a second script |
| Install | one binary | a JVM | `go test` | one binary |
| Local echo, 50 VU for 5s | about 27k rps, 65 MB |  |  | about 29.5k rps, 150 MB |

gi is for a new API suite that should be both the functional test and the load test. A soak and a spike in one file run in the same window. godog fits suites whose steps call domain code. k6 fits a load platform. Karate fits a team staying on the JVM.

For example, `examples/users.feature` creates a user and reads it back:

```gherkin
Feature: Users API

Background:
  * url baseUrl
  * header Accept = 'application/json'
  * def email = 'ada-' + uuid() + '@example.com'

Scenario: create a user and read it back
  * path 'users'
  * request { name: 'Ada Lovelace', age: 36, email: '#(email)', tags: ['math', 'poet'] }
  * method post
  * status 201
  * match response contains { id: '#uuid', name: 'Ada Lovelace' }
  * def id = response.id
  * path 'users', id
  * method get
  * status 200
  * match response.id == id
```

From a checkout of this repository, start the example mock service:

```bash
gi mock examples/users.mock.feature --addr :18080
```

In another terminal, run the same test once, then under load. Both runs check the assertions above:

```bash
gi test examples/users.feature --var baseUrl=http://127.0.0.1:18080 --report report.html
gi load examples/users.feature --var baseUrl=http://127.0.0.1:18080 --vus 20 --duration 30s
```

Use your service's URL as `baseUrl` to test it instead. From `go test`, call [`gi.Test`](https://gi-runner.github.io/gi/embedding/). See [load testing](https://gi-runner.github.io/gi/load/) for workload options and [supported scope and limits](https://gi-runner.github.io/gi/limits/) before migrating existing Karate tests.

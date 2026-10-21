<p align="center">
  <img src="docs/mascot.png" alt="gi" width="180">
</p>

<h1 align="center">gi</h1>
<p align="center">Karate for Go. API tests, mocks, and load tests. One binary. No JVM.</p>

<p align="center">
  <a href="https://go.dev/dl/"><img src="https://img.shields.io/badge/Go-1.27.2-00ADD8?logo=go&logoColor=white" alt="Go 1.27.2"></a>
  <a href="https://github.com/gi-runner/gi/actions/workflows/ci.yml"><img src="https://github.com/gi-runner/gi/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue" alt="Apache 2.0"></a>
  <a href="https://gi-runner.github.io/gi/"><img src="https://img.shields.io/badge/docs-online-009688" alt="Documentation"></a>
  <a href="https://pkg.go.dev/github.com/gi-runner/gi"><img src="https://pkg.go.dev/badge/github.com/gi-runner/gi.svg" alt="Go Reference"></a>
</p>

**[Documentation](https://gi-runner.github.io/gi/)** · [Quick start](https://gi-runner.github.io/gi/quickstart/) · [Examples](examples/)

[REST, GraphQL and SOAP](https://gi-runner.github.io/gi/http/) · [Browser automation](https://gi-runner.github.io/gi/browser/) · [WebSocket, gRPC and Kafka](https://gi-runner.github.io/gi/protocols/)

```bash
go install github.com/gi-runner/gi/cmd/gi@latest
```

`examples/users.feature` against `examples/users.mock.feature`:

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

```bash
gi mock examples/users.mock.feature --addr :18080
```

In another terminal:

```bash
gi test examples/users.feature --var baseUrl=http://127.0.0.1:18080 --report report.html
gi load examples/users.feature --var baseUrl=http://127.0.0.1:18080 --vus 20 --duration 30s
```

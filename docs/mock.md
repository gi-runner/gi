# Mock

`gi mock` serves a feature file as HTTP. Each scenario is a route. The first matching scenario answers the request.

## Try it

Save as `users.mock.feature`:

```gherkin
Feature: users mock

  Scenario: create
    * methodIs POST
    * pathMatches '/users'
    * def response = { id: '1', name: request.name }
    * def responseStatus = 201

  Scenario: read
    * methodIs GET
    * pathMatches '/users/{id}'
    * def response = { id: pathParams.id, name: 'Ada' }
```

```bash
gi mock users.mock.feature --addr 127.0.0.1:18080
```

In another terminal:

```bash
curl -i http://127.0.0.1:18080/users \
  -H 'Content-Type: application/json' -d '{"name":"Ada"}'
curl -i http://127.0.0.1:18080/users/1
```

The POST returns status `201` and `{ "id": "1", "name": "Ada" }`. The GET returns status `200` with the same body. This example uses a fixed ID and name; it does not persist created users. For a stateful version, see [the repository's users mock](https://github.com/gi-runner/gi/blob/main/examples/users.mock.feature).

## Routing and state

The default listen address is `:18080`.

| Step | Effect |
| --- | --- |
| `methodIs('post')` | The scenario matches only that method. |
| `pathMatches('/users/{id}')` | The path must match. `{id}` is captured into `pathParams.id`. |

`request` is the parsed body when its contents are valid JSON, regardless of the request content type; otherwise it is raw text. An empty body is null. Set `response` and, when the status is not 200, `responseStatus`.

`@setup` and `@ignore` scenarios are not routes.

`Background` executes once when the mock starts. User variables remain in one runtime across requests. Requests run one at a time, so state mutations are safe, and a cancelled request returns without waiting for the one in progress. Request and response metadata are reset for each request. Routes can also use expressions in their scenario names, such as `Scenario: pathMatches('/users/{id}') && methodIs('get')`.

Available request variables include `requestPath`, `requestMethod`, `requestUri`, `requestBytes`, `requestHeaders`, `requestParams`, and `requestCookies`. `paramValue(name)` reads the first query value and `paramExists(name)` checks for it. Set `responseHeaders` and `responseDelay` when needed. A matcher exception returns HTTP 500. Request bodies over 1 MiB return HTTP 413.

## Common mistakes

| Symptom | Check |
| --- | --- |
| HTTP 404 with `no mock scenario matched` | Check the method and path, then route order. The first match wins. |
| `request.name` is missing | Check that the body is valid JSON and contains `name`. Non-JSON bodies remain text. |
| A route answers more methods than expected | Add `methodIs GET` or the appropriate method. A path alone does not restrict methods. |
| HTTP 500 while matching | Check the scenario-name expression for a JavaScript error. |
| Address already in use | Stop the other server or choose another `--addr` port. |

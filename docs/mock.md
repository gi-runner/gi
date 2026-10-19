# Mock

`gi mock` serves a feature file as HTTP. Each scenario is a route. The first matching scenario answers the request.

```gherkin
Feature: users mock

  Scenario: create
    * methodIs POST
    * pathMatches '/users'
    * def response = { id: '1', name: request.name }
    * def responseStatus = 201

  Scenario: read
    * pathMatches '/users/{id}'
    * def response = { id: pathParams.id, name: 'Ada' }
```

```bash
gi mock examples/users.mock.feature --addr :18080
```

The default address is `:18080`.

| Step | Effect |
| --- | --- |
| `methodIs('post')` | The scenario matches only that method. |
| `pathMatches('/users/{id}')` | The path must match. `{id}` is captured into `pathParams.id`. |

The request body is `request` when it is JSON, and the raw text otherwise. Set `response` and, when the status is not 200, `responseStatus`.

`@setup` and `@ignore` scenarios are not routes.

`Background` executes once when the mock starts. User variables remain in one runtime across requests; requests execute serially so state mutations are safe. Request and response metadata are reset for each request. Routes can also use expressions in their scenario names, such as `Scenario: pathMatches('/users/{id}') && methodIs('get')`.

Available request variables include `requestPath`, `requestMethod`, `requestUri`, `requestBytes`, `requestHeaders`, `requestParams`, and `requestCookies`. `paramValue(name)` reads the first query value and `paramExists(name)` checks for it. Set `responseHeaders` and `responseDelay` when needed. A matcher exception returns HTTP 500. Request bodies over 8 MiB return HTTP 413.

# HTTP requests

A scenario builds one request at a time. `method` sends it and clears the path, query, body, form, and multipart parts. The URL, headers, cookies, and auth stay for the next request.

## Try it

Start the mock from the [quick start](quickstart.md), then save this as `http.feature`:

```gherkin
Feature: HTTP greeting

Scenario: read a greeting
  * url baseUrl
  * path 'hello'
  * param language = 'en'
  * header Accept = 'application/json'
  * method get
  * status 200
  * match response.message == 'Hello, Ada!'
```

```bash
gi test http.feature --var baseUrl=http://127.0.0.1:18081 --report http.html
```

The request is `GET /hello?language=en`. The mock returns status `200` and a JSON message; both assertions pass. Open `http.html` to inspect the URL, headers, and response.

## Response values

After `method`:

- `response`, `responseStatus`, `responseHeaders`, `responseBytes`, `responseCookies`, and `responseTime` are set.
- `responseTime` is milliseconds and includes reading the body.
- JSON and XML are parsed from the content type. Any other body stays text.
- `responseHeaders` values are lists. `match header Name` reads the first value.
- Cookies stay in a scenario jar: host, domain, path, expiry, and Secure, including across redirects.
- A body over 8 MiB fails the step.
- A path segment is escaped once. `path 'a b'` requests `/a%20b`.

## Request

| Step | Effect |
| --- | --- |
| `url expression` | Base URL. A relative `path` is joined to it. |
| `path a, b` | Appends segments. Each segment is evaluated. |
| `param name = expression` | One query parameter. |
| `params expression` | An object of query parameters. |
| `header name = expression` | Sets one header. |
| `headers expression` | An object of headers. |
| `cookie name = expression` | Adds a cookie. An object with a `value` field uses that field. |
| `cookies expression` | An object of cookies. |
| `form field name = expression` | One form field. |
| `form fields expression` | An object of form fields. |
| `request expression` | Body. A string is sent as text, a native XML document as XML, and other data as JSON. |
| `method name` | Sends the request. `get`, `post`, `put`, `patch`, `delete`, `head`, `options`. |
| `status code` | Fails unless `responseStatus` equals the code. |

`GET` and `HEAD` put form fields on the query string and drop multipart parts. `POST`, `PUT`, and `PATCH` use the body in this order: multipart parts, then an explicit `request`, then `application/x-www-form-urlencoded` form fields.

Null query, header, and form values are skipped. Lists produce repeated values. A null configured header removes that name from the request overlay.

## Multipart

```gherkin
* multipart field message = 'hello'
* multipart file note = { read: 'note.txt', filename: 'note.txt', contentType: 'text/plain' }
* method post
```

`multipart fields` takes an object. `multipart files` takes a list of `{ name, read, filename, contentType }` or an object whose keys are the part names. A string part is `text/plain`. An object or list part is JSON. The filename defaults to the base name of `read`. The content type defaults from the file extension, or `application/octet-stream`.

The multipart `Content-Type`, including the boundary, is set on the request. A `configure headers` function can still replace it.

## Auth

`configure auth` sets credentials for the scenario. Choose one of these forms; each replaces the previous auth setting. An explicit `Authorization` header takes precedence.

```gherkin
* configure auth = { type: 'basic', username: 'ada', password: 'secret' }
* configure auth = { type: 'bearer', token: token }
* configure auth = { type: 'oauth2', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret', scope: 'read' }
```

OAuth2 is the client-credentials grant. The first request of the scenario posts `grant_type`, `client_id`, `client_secret`, and `scope` to `accessTokenUrl`. A relative token URL is resolved from the request's scheme and host. The access token is cached for the rest of the scenario, including retries and called features. A non-2xx token response fails the step.

## Retry

```gherkin
* configure retry = { count: 5, interval: 200 }
* retry until responseStatus == 200
* method get
```

`retry until` applies to the next `method` only. `count` is the maximum total number of attempts. `interval` is the pause between attempts, in milliseconds. Each attempt uses the request timeout. Header hooks and request hooks run on every attempt.

## Headers from config

`configure headers` accepts an object or a function. The function runs before every request and its object is merged onto the request. For the same header name, the configured value replaces the scenario value. Auth runs first, so the configured headers can replace `Authorization` too.

`method` also accepts an expression that returns the method name. `configure readTimeout` aliases `timeout`; values must be positive integer milliseconds within Go's duration range. `configure strictResponseParsing = true` fails on malformed JSON or XML for the corresponding response content type.

## TLS, proxy and SOAP

For a private CA, configure its PEM file:

```gherkin
* configure ssl = { trustAll: false, caCert: '/path/ca.pem' }
```

Add `cert` and `key` for mutual TLS. `algorithm` accepts `TLS`, `TLSv1.2` or `TLSv1.3`. `configure ssl = true` disables certificate verification for test servers; use a CA when you want verification. Java keystores are not supported.

| Setting | Effect |
| --- | --- |
| `configure connectTimeout = 5000` | TCP connection timeout in milliseconds. |
| `configure proxy = 'http://localhost:8080'` | Send requests through a proxy. |
| `configure proxy = null` | Disable the proxy. |
| `soap action 'urn:operation'` | Set `SOAPAction`, use `text/xml` and send POST. |

A proxy object accepts `uri`, `username`, `password` and `nonProxyHosts` (Go glob patterns). TLS, proxy and connection settings require a standard Go HTTP transport when using a [custom Go client](embedding.md).

??? note "CA trust pools"

    A supplied CA is added to the transport's existing trust pool, or to the system pool if the transport has none.

## GraphQL

Send the query and variables as a JSON request. Check GraphQL's `errors` field as well as HTTP status: a server may return query errors with status 200.

```gherkin
Feature: GraphQL users

Scenario: find a user
  * url baseUrl
  * path 'graphql'
  * def query = 'query User($id: ID!) { user(id: $id) { id name } }'
  * request { query: '#(query)', variables: { id: '42' } }
  * method post
  * status 200
  * match response.errors == '#notpresent'
  * match response.data.user == { id: '42', name: '#string' }
```

Save as `graphql.feature` and run `gi test graphql.feature --var baseUrl=https://your-api.example`. Replace that URL with a server exposing `/graphql` and the `user` query above. GraphQL uses the same HTTP and match steps as REST.

## Common mistakes

| Symptom | Check |
| --- | --- |
| The next request loses its path or body | Set them again before each `method`; these builders are cleared after sending. |
| A header comparison sees a list | `responseHeaders` contains lists. Use `match header Content-Type contains 'application/json'` to check the first value. |
| A malformed JSON response remains text | Enable `configure strictResponseParsing = true` to fail parsing instead. |
| Polling stops too soon | `retry.count` is the total attempts, including the first. `retry until` applies only to the next `method`. |
| A GraphQL request returns 200 but has query errors | Check `response.errors` in addition to HTTP status. |
| Authentication does not use the configured token | An existing `Authorization` header takes precedence over configured auth; a configured header callback can also replace it. |

# HTTP

A scenario builds one request at a time. `method` sends it and clears the path, query, body, form, and multipart parts. The URL, headers, cookies, and auth stay for the next request.

```gherkin
* url baseUrl
* path 'users', id
* param active = true
* header Accept = 'application/json'
* request { name: 'Ada' }
* method post
* status 201
* match response.name == 'Ada'
```

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

`configure auth` sets one credential for the scenario. It is sent only when the request does not already have an `Authorization` header.

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

`configure headers` accepts an object or a function. The function runs before every request and its object is merged onto the request. A header set by the scenario is not removed. Auth fills `Authorization` only when that header is still empty, and it does so before the header overlay, so the function can replace the credential.

`method` also accepts an expression that returns the method name. `configure readTimeout` aliases `timeout`; values must be positive integer milliseconds within Go's duration range. `configure strictResponseParsing = true` fails on malformed JSON or XML for the corresponding response content type.

## TLS, proxy and SOAP

`configure connectTimeout = 5000` sets a positive TCP connection timeout in milliseconds. `configure ssl = true` trusts test certificates. For verification use `configure ssl = { trustAll: false, caCert: '/path/ca.pem' }`; optional PEM `cert` and `key` enable mutual TLS, and `algorithm` accepts `TLS`, `TLSv1.2` or `TLSv1.3`. A CA is added to the transport's current root pool, or to the system pool when that transport has none. Java keystore formats are unsupported.

`configure proxy = 'http://localhost:8080'` selects a proxy. An object accepts `uri`, `username`, `password` and `nonProxyHosts` (Go glob patterns). Null disables proxy use. These transport settings require a standard Go HTTP transport when embedding a custom client.

`soap action 'urn:operation'` sets `SOAPAction`, selects `text/xml` and sends POST. Path arguments can include lists; null segments are skipped.

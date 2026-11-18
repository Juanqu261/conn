# Backend HTTP API

## Responsibility

Serve Conn's backend over Go `net/http`.

## Current state

- `GET /healthz` reports liveness.
- `GET /readyz` returns 200 only when the database is reachable and its applied Conn migrations exactly match the build. Otherwise it returns 503 `not_ready` and logs the reason, without exposing it.
- Authentication routes:
  - `GET /v1/auth/github/client` serves the public GitHub OAuth client ID, so humans configure only the server URL.
  - `POST /v1/auth/github/exchange` takes the temporary GitHub token as a bearer credential, verifies it with GitHub, and returns the only response that carries a raw Conn session token.
  - `GET /v1/humans/me` returns the authenticated human.
  - `DELETE /v1/session` revokes the current session.
- Project routes (`projects.go`) cover projects, members, Objective and Blueprint revisions, charter decisions, status, and events. The route list, body schemas, and error mapping are in the MVP design under "HTTP API".
- Handlers authenticate the session and parse the request. Project authorization happens in `internal/application`, inside the transaction that performs the operation.
- Request bodies must be `application/json`, at most 1 MiB, and valid against their schema before the application sees them.
- Project-scoped responses are `{"data": ..., "as_of_event": n}`. Other successes are `{"data": ...}`. Errors are `{"error": {"code", "message", "pointer"?}}`. Every response document is validated against `contracts/v1` before it is sent.
- Malformed path IDs answer `404 not_found`, like missing resources, so they reveal nothing.
- Each request gets a random `X-Request-Id` and one access log line with route pattern, status, duration, and the actor and project IDs when known. Bodies, query strings, and headers are never logged.
- The server sets header, read, write, and idle timeouts and a 16 KiB header limit.
- On cancellation it drains in-flight requests for up to 10 seconds.

## Deferred

- Rate limiting of the exchange route
- JSON bodies for `405 Method Not Allowed`, which `net/http` answers in plain text
- TLS termination and deployment

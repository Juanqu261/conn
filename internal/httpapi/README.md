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
- Successful responses are `{"data": ...}` and errors are `{"error": {"code", "message"}}`. Both are validated against `contracts/v1` before they are sent.
- The server sets header, read, write, and idle timeouts and a 16 KiB header limit.
- On cancellation it drains in-flight requests for up to 10 seconds.

## Deferred

- Project, objective, blueprint, charter, status, and event routes with project authorization
- Rate limiting of the exchange route
- Request IDs, structured access logs, and body limits
- TLS termination and deployment

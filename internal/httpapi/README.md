# Backend HTTP API

## Responsibility

Serve Conn's backend over Go `net/http`.

## Current state

- `GET /healthz` reports liveness.
- `GET /readyz` returns 200 only when the database is reachable and its applied Conn migrations exactly match the build. Otherwise it returns 503 `not_ready` and logs the reason, without exposing it.
- The server sets header, read, write, and idle timeouts and a 16 KiB header limit.
- On cancellation it drains in-flight requests for up to 10 seconds.

## Deferred

- `/v1` routes, authentication, and project authorization
- Request IDs, structured access logs, and body limits
- TLS termination and deployment

# Backend HTTP API

## Responsibility

Serve Conn's backend over Go `net/http` (decision 0012).

## Current state

- `GET /healthz` reports liveness.
- The server sets header, read, write, and idle timeouts and a 16 KiB header limit.
- On cancellation it drains in-flight requests for up to 10 seconds.

## Deferred

- `/v1` routes, authentication, and project authorization
- `GET /readyz` with the schema-compatibility check
- Request IDs, structured access logs, and body limits
- TLS termination and deployment

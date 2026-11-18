# Local account operations

## Responsibility

Combine GitHub device authorization, the backend client, and the credential store for `conn login`, `logout`, `whoami`, and `doctor`. `Client` gives the project commands a backend client authenticated with the resolved credential. Commands in `internal/cli` only render results.

## Behavior

- Login checks the credential store before contacting GitHub, so a human never authorizes only to find the session cannot be kept.
- Logging in again replaces the stored session and revokes the previous one on the server.
- Logout always removes the local credential and reports whether server-side revocation succeeded.
- Doctor reports server, readiness, credential store, credential source, and session state without printing a credential.

## Test

`TestSessionLifecycle` runs the whole flow against the real HTTP handler, a fake GitHub, and a fresh PostgreSQL database.

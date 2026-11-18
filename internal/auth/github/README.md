# GitHub authentication

## Responsibility

Run GitHub's OAuth device flow from the CLI and verify GitHub tokens on the backend.

## Behavior

- The CLI requests a device code with no OAuth scope, shows the code, and polls at the interval GitHub requires. `slow_down` raises the interval, and denial or expiry stops polling.
- The backend calls `GET /user` once per login to learn the immutable user ID and current login, then discards the token.
- Tokens stay in `secret.String` values and never appear in errors.

## Tests

`githubtest` is a fake GitHub. No test calls the real one.

## Known limitation

`GET /user` accepts any valid GitHub token for the user, including tokens issued to other OAuth apps. Binding exchange to Conn's own OAuth app requires GitHub's token-check API and a server-side client secret. That is recommended before Conn serves people beyond the pilot.

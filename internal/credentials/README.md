# Local credentials

## Responsibility

Resolve the local Conn session token.

## Behavior

- `CONN_TOKEN` takes precedence when set and is never written anywhere.
- Otherwise the token comes from the operating-system credential store through `zalando/go-keyring`: Windows Credential Manager, macOS Keychain, or Linux Secret Service. The entry is service `conn` with one account per server URL, so development and production sessions never mix.
- There is no plaintext fallback. An unavailable store is reported with the next step.
- Linux needs a running and unlocked Secret Service provider, such as GNOME Keyring or KeePassXC.

## Tests

`Memory` replaces the real store in automated tests. The real store was smoke-tested on Windows; macOS and Linux need a manual smoke test before the pilot.

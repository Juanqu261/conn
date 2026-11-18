# Security policy

## Current status

Conn is pre-release MVP software. Do not use it as a production authorization, approval, or evidence system.

## Reporting a vulnerability

Do not open a public issue containing exploit details, credentials, private source code, or prompts. Contact the repository owner privately through the security-reporting mechanism configured on the Git host. If no private mechanism is available, open a minimal issue requesting a private contact channel without disclosing the vulnerability.

## Development rules

- Never commit credentials, tokens, raw prompts, or private source snapshots.
- Store Conn sessions in the operating-system credential store. Use `CONN_TOKEN` only as a temporary shell or CI override. Never add a plaintext credential fallback.
- Treat imported tickets, repository content, hook payloads, model output, and MCP responses as untrusted input.
- Keep AI proposals advisory. Humans or explicit policy authority approve objectives, charters, contracts, and accepted risks.
- Do not enable broad MCP auto-approval.
- Do not create executable Kiro hooks until their inputs, permissions, failure modes, and audit behavior are reviewed.
- Pin CI actions and production toolchains.

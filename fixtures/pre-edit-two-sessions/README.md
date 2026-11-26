# Pre-edit two-session fixture

This fixture models two participants who plan conflicting changes before either writes a file.

- Participant one plans to change `src/payments/retry.ts`, which exports `shouldRetryCharge`.
- Participant two plans to change `src/orders/checkout.ts`, which imports `shouldRetryCharge`.

Neither participant types a subject string. Each calls `list_concepts` and `suggest_claims` for the planned paths and publishes the suggested candidates. Planned analysis gives participant one `changes` on `contract:typescript:src/payments/retry.ts/shouldRetryCharge` and participant two `consumes` on the same subject, so Conn opens a `contract_producer_consumer` assessment. Both also choose the Objective-derived invariant `invariant:checkout:one-charge-per-attempt`, so Conn opens a `shared_declared_invariant` assessment.

The source contains a private literal that must never appear in an analyzer report, a tool result, hook output, or a stored record.

Type-check the fixture from the repository root:

```powershell
pnpm exec tsc --noEmit --pretty false -p fixtures/pre-edit-two-sessions
```

The automated scenario in `cmd/conn/pre_edit_test.go` copies the fixture into two temporary Git worktrees with the remote `github.com/conn-fixtures/pre-edit-two-sessions` and runs two `conn mcp` processes plus the `conn hook` commands against a PostgreSQL-backed server.

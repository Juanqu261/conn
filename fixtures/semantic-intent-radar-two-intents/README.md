# Semantic intent Radar two-intent fixture

This source-free validation fixture models two independently type-checking changes that must coordinate through the declared invariant `invariant:checkout:one-idempotency-key-one-successful-charge`.

- `apps/gateway` changes retry behavior before charging a payment.
- `apps/orders` changes retry behavior while recording an order.

The worktrees use separate TypeScript configurations and do not import one another, so both can pass their local compiler check without proving that their combined behavior preserves the shared invariant. Conn receives only the normalized declared invariant and reference-only evidence, never the source in this fixture.

Run each local check from the repository root:

```powershell
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/gateway
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/orders
```

The vertical-slice integration test creates two session-bound Kiro MCP participants and publishes matching declared claims for this invariant. It verifies the deterministic `shared_declared_invariant` assessment, Radar and context-pack content, a non-binding participant proposal, an attributable human decision, and bounded reference-only evidence.

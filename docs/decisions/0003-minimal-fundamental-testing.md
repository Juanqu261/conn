# Decision: Keep tests minimal and fundamental

- Status: accepted
- Date: 2026-11-16
- Decision owners: project owner
- Related product requirement: assembly evidence and protocol correctness

## Context

Conn needs trustworthy protocol, authority, lifecycle, and evidence behavior. A large volume of generated tests would increase maintenance cost and make important guarantees harder to find. Test count and coverage percentage are poor substitutes for validating the small number of behaviors on which the product depends.

## Options considered

### Option A: Broad unit-test coverage for most implementation units

Advantages:

- Many regressions may be localized quickly
- Conventional coverage metrics are easy to report

Disadvantages:

- Encourages tests of implementation details and framework behavior
- Produces large suites that are costly to revise
- High test counts can obscure missing system-level guarantees

### Option B: Minimal tests focused on fundamental behavior

Advantages:

- Keeps the validation suite understandable
- Tests survive internal refactoring more often
- Engineering attention remains on protocol and product risks
- Faster local and CI feedback

Disadvantages:

- Requires deliberate judgment about what is fundamental
- Some localized regressions may be found by broader tests or later integration checks

## Recommendation

Use the smallest set of tests that protects Conn's externally visible contracts and critical invariants.

## Decision

Conn will not optimize for test count or blanket coverage. Tests are added only when they protect at least one of these categories:

- public protocol serialization and compatibility;
- authority and approval boundaries;
- deterministic lifecycle or collision-policy transitions;
- persistence integrity and migration safety;
- security-sensitive input validation;
- one critical integration path between components;
- a reproduced defect that could recur.

Testing rules:

- Prefer one table-driven or parameterized test over many repetitive tests.
- Prefer contract and behavior tests over private-function tests.
- Do not test language runtimes, frameworks, generated accessors, or trivial data containers.
- Do not duplicate the same assertion at unit, service, and end-to-end levels without a distinct risk.
- Avoid snapshot tests unless the serialized artifact itself is a reviewed public contract.
- Do not introduce a coverage percentage gate during the MVP.
- Every executable surface may have one smoke test when startup or invocation is part of its contract.
- New tests require a short statement of the invariant or regression they protect.

## Consequences

- Reviewers should reject generated test volume that does not protect a named risk.
- CI remains small and fast.
- Important scenario fixtures may exercise several components instead of being copied into many test layers.
- Lack of a coverage target does not permit critical protocol or security behavior to remain untested.

## Validation

At each milestone, list the fundamental guarantees and map each guarantee to either one automated test, an explicit manual validation, or a documented reason it is not yet verifiable. Remove redundant tests when a smaller test protects the same guarantee.

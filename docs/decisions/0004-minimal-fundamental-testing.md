# 0004: Minimal fundamental testing

- Status: accepted
- Owner: project owner

## Context

Conn needs strong protocol and authority guarantees without a large suite that tests frameworks or private implementation details.

The alternatives were broad unit coverage and coverage-percentage gates. Those metrics add maintenance cost without proving the small set of behaviors the product depends on.

## Decision

Add the smallest test that protects a named risk in one of these areas:

- public contracts and compatibility;
- authority and approval boundaries;
- deterministic lifecycle or collision transitions;
- persistence and migration integrity;
- security-sensitive validation;
- one critical component integration path;
- a reproduced regression.

Prefer parameterized public-behavior tests. Avoid duplicate layer assertions, framework tests, incidental snapshots, and a coverage-percentage gate. Manual validation is acceptable only when the required behavior genuinely needs people or an external client, and its evidence must be recorded.

## Consequences

The suite stays fast and readable. Reviewers must reject test volume without a named invariant, while ensuring critical protocol and security behavior is not left untested.

## Validation

At each milestone, map every fundamental guarantee to one automated test, one explicit manual trial, or a documented external gate. Remove redundant tests when a smaller scenario protects the same guarantee.

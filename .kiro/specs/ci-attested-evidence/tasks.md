# Tasks

- [ ] 1. Review and approve the CI-attested evidence scope
  - Confirm the acceptance goal: independent, signed CI proof attached as evidence, with CI still report-only and writing nothing to Conn.
  - Confirm the opt-in and its plan limitation: public repositories or GitHub Enterprise Cloud, and no attestation for fork pull requests.
  - Confirm that checks come only from the default branch's `ci_checks`.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 2, 8.

- [ ] 2. Record decision 0011
  - Add `docs/decisions/0011-ci-attested-evidence.md`: GitHub artifact attestations as the signing mechanism, the predicate type URI, the job permission split, human-side verification, and deferred server-side verification. Record that it amends 0007 (observation runners) and 0008 (workflow template).
  - Update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md`. Requirements: 4, 6, 8.

- [ ] 3. Spike attestations on a public test repository
  - Create a throwaway workflow on `pull_request` that attests a fixed subject with a custom predicate. Confirm the certificate's workflow identity and source ref, the matching `--signer-workflow` value, lookup with `gh attestation verify --repo`, and whether a fork pull request receives an OIDC token.
  - Record the findings in the design's "GitHub facts" section. Stop for review if a finding changes a requirement. Requirements: 4, 5, 6.

- [ ] 4. Define the contracts
  - Add `ci_checks` to `checks-config`, the `ci-check-attestation` predicate schema, `github_actions` to observation runners with the conditional `attestation` object, and the results file schema.
  - Add accepted and rejected fixtures and Go types. Write `docs/attestations/ci-check-v1.md` at the predicate type URI. Requirements: 2, 4, 6.

- [ ] 5. Implement the subject and `conn ci run-checks`
  - Add `internal/ciattest` with `Subject` and the results writer, and the CI-only command on the shared check runner.
  - Add the subject bytes test and the temporary-repository run-checks test. Requirements: 3, 5.

- [ ] 6. Render workflow template version 2
  - Add the two jobs, the embedded prepare-statement script, SHA-pinned `actions/attest`, `actions/upload-artifact`, and `actions/download-artifact`, and `--attest-checks` / `attest_checks` on install. Replace an unchanged version 1.
  - Add the template tests from the validation plan, including the script's subject format. Requirements: 1, 3, 4, 8.

- [ ] 7. Implement `conn evidence import-ci`
  - Workflow integrity check, `gh` invocation, result filtering, evidence attachment per acceptance key, and the terminal-application action.
  - Backend: accept `github_actions` observations only from CLI and terminal-application Participants.
  - Add the fake-`gh` import scenario. Requirements: 6, 8.

- [ ] 8. Update the review packet
  - Ordering, the CI-attested line with the verify command, `--verify-attestations`, the workflow integrity note, and the fixed limitation note. Extend the existing packet test. Requirements: 7.

- [ ] 9. Update documentation
  - README "CI-attested checks" section with the plan requirement and the reviewer's verify command. `conn doctor` reports `gh` availability and authentication. Requirements: 1, 7.

- [ ] 10. Run the real trial
  - On a public repository: a same-repository pull request with an attested passing check imported and shown in the packet, `--verify-attestations` from a second account, a fork pull request without attestation, and a pull request that edits the workflow being refused.
  - Record the results in `docs/real-trial.md`. Requirements: 9.

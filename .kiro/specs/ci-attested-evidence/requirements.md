# Requirements

Milestone 6. Acceptance goal: a reviewer can trust that a pull request's checks passed without trusting the author's machine. GitHub Actions runs the repository's Conn checks at the pull-request head and signs the results with a Sigstore-backed in-toto attestation. Conn verifies the signature and attaches the results as evidence labelled CI-attested.

Today an observed run is "observed on the Intent owner's machine at the recorded commit; it is not independent CI proof." This milestone adds the independent proof, keeps CI report-only, and keeps CI from writing to Conn.

This milestone depends on `review-proof` (check runner, evidence observations, review packet) and `verification-ci-and-adoption` (CI add-on, installer, default-branch configuration rule).

## 1. Opt-in attested checks

**User story:** As a Project owner, I want to turn on CI-attested checks only when my repository can use them.

### Acceptance criteria

1. WHEN an owner runs `conn ci install --attest-checks` or `install_ci_check` with `attest_checks: true`
   THE SYSTEM SHALL render workflow template version 2 with the report job, a check-run job, and an attestation job
2. WITHOUT that option
   THE SYSTEM SHALL render the report job only, byte-identical in behavior to template version 1
3. WHEN rendering with attested checks
   THE SYSTEM SHALL state that GitHub artifact attestations require a public repository or GitHub Enterprise Cloud, and that pull requests from forks are reported but not attested
4. WHEN a repository has template version 1 installed unchanged
   THE SYSTEM SHALL replace it on install, and otherwise refuse, as for other Conn-managed files

## 2. CI check configuration

**User story:** As a maintainer, I want to choose which checks CI attests, from configuration a pull request can't change.

### Acceptance criteria

1. THE SYSTEM SHALL read a `ci_checks` section of `.conn/checks.json` with an optional `setup` check and up to 20 keys of checks defined in the file
2. THE SYSTEM SHALL read `ci_checks` and the checks it names only from the repository default branch, never from the pull-request branch
3. THE SYSTEM SHALL record the SHA-256 of the default-branch configuration file and its commit in the results
4. WHEN the default branch has no `ci_checks` section
   THE SYSTEM SHALL run no checks and produce no attestation, and the report job SHALL say so

## 3. Check-run job

**User story:** As a reviewer, I want checks to run on a clean GitHub-hosted machine at exactly the commit I'm reviewing.

### Acceptance criteria

1. THE SYSTEM SHALL run the check-run job on GitHub-hosted runners at the pull-request head commit
2. THE SYSTEM SHALL give the check-run job `contents: read` only, no Conn credential, no repository secrets, and no OIDC token
3. THE SYSTEM SHALL run the setup check once, then each configured check through the shared check runner without a shell and without `CONN_` variables
4. THE SYSTEM SHALL produce a results file with, per check, the key, acceptance keys, command hash, exit code, timeout flag, duration, and output hash, and SHALL never include output or logs
5. THE SYSTEM SHALL keep the job successful when a check fails, recording the failure in the results

## 4. Attestation job

**User story:** As a reviewer, I want the results signed by an identity the pull-request code can't impersonate.

### Acceptance criteria

1. THE SYSTEM SHALL run the attestation job only after the check-run job, only for pull requests from the same repository, and only on GitHub-hosted runners
2. THE SYSTEM SHALL give the attestation job `id-token: write`, `attestations: write`, and `contents: read`, and SHALL never check out or run pull-request code in it
3. THE SYSTEM SHALL build the attestation subject from the workflow event context, not from the check-run job's output
4. THE SYSTEM SHALL treat the check-run job's results only as data: validate their shape and bounds, and set the repository, commit, and run fields from the event context
5. THE SYSTEM SHALL sign an in-toto Statement with Conn's CI-check predicate type through `actions/attest`
6. WHEN attestation fails or is unavailable
   THE SYSTEM SHALL keep the job and the pull request's checks successful and SHALL report the reason in the job summary

## 5. Reproducible subject

**User story:** As a verifier, I want to find the attestation for a commit without downloading any file from CI.

### Acceptance criteria

1. THE SYSTEM SHALL define the attestation subject as fixed bytes built only from the repository host and name and the full commit hash
2. THE SYSTEM SHALL produce identical subject bytes in the workflow and in the Conn CLI, so that the CLI can look up the attestation by digest

## 6. Importing attested evidence

**User story:** As the Intent owner, I want to attach CI-attested results to my Intent so that the review packet shows independent proof.

### Acceptance criteria

1. WHEN a human runs `conn evidence import-ci [--intent <intent-id>] [--commit <sha>]`
   THE SYSTEM SHALL verify the attestation for the Intent's head commit, or the given commit, with `gh attestation verify`, requiring the repository, the predicate type, the Conn workflow as signer, and GitHub-hosted runners
2. THE SYSTEM SHALL reject an attestation whose predicate names a different repository or commit than the subject
3. THE SYSTEM SHALL reject the import WHEN the Conn workflow file at the attested commit is not byte-identical to a Conn-rendered template that includes attested checks
4. WHEN verification succeeds
   THE SYSTEM SHALL attach one `check_run` evidence record per acceptance key with an observation whose runner is `github_actions`, carrying the attestation identifier, the workflow run, the signer workflow, and the verification time
5. WHEN several attestations exist for the commit
   THE SYSTEM SHALL use the most recent verified one and report the others
6. THE SYSTEM SHALL use the human's own `gh` authentication, SHALL never read, store, or send a GitHub token, and SHALL run `gh` without a shell
7. THE SYSTEM SHALL accept CI-attested observations only from the CLI and terminal application, never from the MCP adapter

## 7. Review packet

**User story:** As a reviewer, I want to see at a glance which proof is independent and how to check it myself.

### Acceptance criteria

1. THE SYSTEM SHALL label CI-attested evidence distinctly from local observed runs and reported evidence, and SHALL rank it first for an acceptance criterion
2. THE SYSTEM SHALL show, for each CI-attested record, the workflow run, the attested commit, its staleness against the local head, and the `gh attestation verify` command a reviewer can run without Conn
3. WHEN the pull request changes `.conn/checks.json` or the Conn workflow file
   THE SYSTEM SHALL add an integrity note
4. THE SYSTEM SHALL state that CI-attested results prove the default-branch checks ran on a GitHub-hosted runner at that commit and passed or failed as recorded, not that the code under test is correct
5. WHEN a reviewer runs `conn review --verify-attestations`
   THE SYSTEM SHALL re-verify every CI-attested record on the reviewer's machine with `gh attestation verify` and SHALL mark each record verified or unverified in the packet, so that trust does not rest on whoever imported the record

## 8. Report-only CI and authority

### Acceptance criteria

1. THE SYSTEM SHALL never fail a pull request for a check failure, a Conn finding, or an attestation error
2. THE SYSTEM SHALL write nothing to Conn from any CI job
3. THE SYSTEM SHALL keep evidence attachment rules unchanged: any Participant of the Human who owns the Intent may import
4. THE SYSTEM SHALL keep the CI credential scope unchanged; no new CI route is needed

## 9. Milestone validation

### Acceptance criteria

1. A Go test SHALL prove the subject bytes match the workflow's construction for fixed inputs
2. A Go scenario with a fake `gh` SHALL cover a valid import, a wrong commit, a wrong signer, a modified workflow, multiple attestations, and `gh` missing or unauthenticated
3. Template tests SHALL prove the permission split: no OIDC permission and no secrets in the check-run job, and no pull-request checkout in the attestation job
4. A real trial on a public repository SHALL record a same-repository pull request with an attested passing check, a fork pull request without attestation, and the review packet, in `docs/real-trial.md`

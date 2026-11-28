# Design

## Purpose and scope

Review proof has a deliberate gap. `conn evidence run` observes a check on the Intent owner's machine and labels it "not independent CI proof." This milestone closes the gap with established supply-chain tooling: GitHub Actions runs the default-branch Conn checks at the pull-request head, `actions/attest` signs the results as an in-toto Statement through GitHub's Sigstore instance, and Conn verifies the signature with `gh attestation verify` before attaching the results as evidence.

The CI add-on stays report-only and still writes nothing to Conn. Verification happens on human machines: the importer's when evidence is attached, and optionally the reviewer's when the packet is read.

## Dependencies

- `review-proof`: the shared check runner, `evidenceObservation`, staleness, and the review packet loader.
- `verification-ci-and-adoption`: the workflow template and installer, the default-branch configuration rule from `verify-pair`, and `gh` as an external CLI (already used by `--github-secret`).

## Architecture decision gate

Decision 0011 must be accepted before implementation.

1. **Signing mechanism.**

   | Option                                                                                   | Advantages                                                                                                                                        | Disadvantages                                                                                                                                            | Operational cost     |
   | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
   | GitHub artifact attestations: `actions/attest` and `gh attestation verify` (recommended) | No keys; Sigstore certificates bound to the workflow identity; GitHub stores attestations and looks them up by digest; reviewers verify with `gh` | Public repositories, or private ones on GitHub Enterprise Cloud only; not available on GitHub Enterprise Server; fork pull requests are not attested     | None                 |
   | `cosign` keyless signing against the public Sigstore instance                            | Works for any repository plan                                                                                                                     | Publishes private repository names and commits to the public transparency log; Conn must store and distribute bundles; adds a Sigstore client to the CLI | Bundle storage       |
   | CI writes evidence to Conn with the CI credential                                        | No signing                                                                                                                                        | Reverses decision 0008 item 3; trust rests on a bearer secret that reviewers cannot check                                                                | A write route for CI |

   Recommendation: GitHub artifact attestations. The plan limitation is acceptable because the add-on is opt-in and the hackathon and open-source audience is public repositories.
2. **Predicate type URI.** Recommendation: `https://github.com/Juanqu261/conn/blob/main/docs/attestations/ci-check-v1.md`, a resolvable page documenting the predicate, versioned in the path. The URI is fixed once published.
3. **Server-side signature verification is deferred.** The backend records what a human-direct CLI verified; `conn review --verify-attestations` lets any reviewer re-verify independently. Verifying in the backend would add a Sigstore library to `conn server` and is deferred until a real need appears.

## GitHub facts this design relies on

Confirmed from GitHub documentation and the `actions/attest` and `gh` manuals:

- `actions/attest@v4` creates a custom attestation from `subject-path` (or `subject-digest` + `subject-name`), `predicate-type`, and `predicate-path`. The predicate may be up to 16 MB. Outputs include `attestation-id`, `attestation-url`, and `bundle-path`.
- It needs `id-token: write` and `attestations: write`. The `id-token` permission mints the OIDC token for the Sigstore signing certificate.
- "Artifact attestations are available in public repositories for all current GitHub plans." Private and internal repositories need GitHub Enterprise Cloud. They are not supported on GitHub Enterprise Server.
- `gh attestation verify <file> --repo <owner/name>` looks the attestation up by the file's digest. `--predicate-type` is required for a non-SLSA predicate. `--signer-workflow`, `--deny-self-hosted-runners`, and `--format json` are available. The JSON output contains `verificationResult.statement` with `subject`, `predicateType`, and `predicate`, plus the certificate and verified timestamps.
- The predicate is workflow-controlled data. Only the certificate and timestamps are unforgeable by the workflow. This design therefore treats the signer identity, not the predicate, as the root of trust, and constrains what the signing job can be told.

Not confirmed, and resolved by the spike in task 3:

- Whether `pull_request` runs from forks are denied an OIDC token. The design assumes they are and skips the attestation job for forks.
- The certificate fields for a `pull_request` run (`refs/pull/<n>/merge` as the source ref) and the exact `--signer-workflow` value that matches them.
- Whether `gh attestation verify` finds an attestation created by a pull-request run when called with `--repo`.

## Configuration

`.conn/checks.json` gains a `ci_checks` section with the same shape as `pair_verification`:

```json
{
  "version": 1,
  "checks": {
    "unit-tests": {
      "command": ["pnpm", "test"],
      "timeout_seconds": 600,
      "acceptance_keys": ["fr-001", "gateway-tests"]
    }
  },
  "ci_checks": {
    "setup": { "command": ["pnpm", "install", "--frozen-lockfile"], "timeout_seconds": 600 },
    "checks": ["unit-tests"]
  }
}
```

CI reads it only from the default branch, through a second sparse checkout. A pull request that edits `ci_checks` changes nothing until it merges. The review packet already flags a changed `.conn/checks.json`.

Checks run with the runner image's preinstalled toolchains plus the setup command. Pinned per-repository toolchain setup is deferred.

## Workflow template version 2

Rendered by `conn ci install --attest-checks`. Top-level `permissions: {}`; each job declares its own. Action references are pinned by commit SHA with a version comment, as in version 1. Workflow expressions reach scripts only through `env`, never by interpolation into a `run` body.

```yaml
# Generated by conn ci install (template v2, attested checks). Conn source @@CONN_REF@@.
name: Conn check
on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]
permissions: {}
concurrency:
  group: conn-check-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  conn-check:            # unchanged report job from version 1
    ...

  conn-run-checks:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    permissions:
      contents: read
    steps:
      - checkout PR head into repo/ (persist-credentials: false)
      - checkout default branch into base/ with sparse-checkout: .conn (persist-credentials: false)
      - check out Conn and build it (same distribution steps as the report job)
      - name: Run Conn CI checks
        env:
          DEFAULT_BRANCH_SHA: (from the base checkout)
        run: '"$RUNNER_TEMP/conn" ci run-checks --worktree repo --config base/.conn/checks.json --config-commit "$DEFAULT_BRANCH_SHA" --output "$RUNNER_TEMP/conn-ci-results.json"'
      - upload-artifact conn-ci-results (retention-days: 7)

  conn-attest-checks:
    needs: conn-run-checks
    if: github.event.pull_request.head.repo.full_name == github.repository
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions:
      contents: read
      id-token: write
      attestations: write
    steps:
      - download-artifact conn-ci-results
      - name: Prepare statement
        env:
          SERVER_URL: ${{ github.server_url }}
          REPOSITORY: ${{ github.repository }}
          HEAD_SHA: ${{ github.event.pull_request.head.sha }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          RUN_ID: ${{ github.run_id }}
          RUN_ATTEMPT: ${{ github.run_attempt }}
        run: (fixed script, below)
      - uses: actions/attest@<sha> # v4
        continue-on-error: true
        with:
          subject-path: ${{ runner.temp }}/conn-ci-subject.txt
          predicate-type: <predicate type URI>
          predicate-path: ${{ runner.temp }}/conn-ci-predicate.json
      - write the attestation URL or the failure reason to the job summary
```

Properties the template tests hold:

- `conn-run-checks` has `contents: read` only, references no `secrets.*`, and has no `id-token` permission. It is the only job that runs pull-request code.
- `conn-attest-checks` has no `actions/checkout` step and runs no `conn` binary or repository file. Its only inputs are the event context and the artifact, read as data.
- Every step that could fail for Conn reasons has `continue-on-error: true` or exits 0, so the workflow never fails a pull request.
- Version 1 installed unchanged is replaced on install. Removal deletes only byte-identical content of either version.

### Prepare-statement script

A fixed POSIX shell script embedded in the template. It uses only `printf`, `tr`, `sha256sum`, and `jq`, all preinstalled on GitHub-hosted Ubuntu images.

1. Subject: `host` is `SERVER_URL` without its scheme. `repository` is `host/REPOSITORY`, lowercased. `HEAD_SHA` must match `^[0-9a-f]{40}([0-9a-f]{24})?$`. Write `conn-ci-subject.txt` with `printf 'conn-ci-subject/v1\nrepository %s\ncommit %s\n'`.
2. Results: refuse a file over 64 KiB. Validate with `jq` that it is an object with `results_version` 1, at most 21 check entries, each with the expected scalar types and keys matching `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`, and SHA-256 fields of 64 hex characters.
3. Predicate: `jq` builds the predicate from the validated results, **overwriting** `repository`, `commit`, `pull_request`, and `workflow_run` from the environment. A results file that claims another commit cannot change what is signed.
4. On any validation failure, write the reason to the summary and skip the attest step.

## Subject

`internal/ciattest.Subject(host, owner, name, commit string) []byte` returns exactly the bytes the script writes. The CLI derives host, owner, and name from the Intent's RepositoryConnection canonical remote. A Go test pins the bytes for fixed inputs, and a template test extracts the script's `printf` format and compares it.

## Predicate `ci-check-attestation`

New schema `contracts/v1/ci-check-attestation.schema.json`, documented at the predicate type URI:

```json
{
  "predicate_version": 1,
  "repository": "github.com/owner/name",
  "commit": "<head sha>",
  "pull_request": 42,
  "workflow_run": { "id": 123, "attempt": 1 },
  "config": {
    "path": ".conn/checks.json",
    "source_commit": "<default-branch sha>",
    "sha256": "<hex>"
  },
  "worktree_dirty_before": false,
  "setup": {
    "check_key": "setup",
    "command_sha256": "...",
    "exit_code": 0,
    "timed_out": false,
    "duration_ms": 41000,
    "output_sha256": "...",
    "started_at": "..."
  },
  "checks": [
    {
      "check_key": "unit-tests",
      "acceptance_keys": ["fr-001", "gateway-tests"],
      "command_sha256": "...",
      "exit_code": 0,
      "timed_out": false,
      "duration_ms": 18000,
      "output_sha256": "...",
      "started_at": "..."
    }
  ]
}
```

No output, logs, environment, or source. `conn ci run-checks` writes the same structure as `results_version: 1`, without the context fields the attestation job overwrites.

## `conn ci run-checks`

A CI-only command that needs no Conn credential and makes no network call. It loads the given configuration file, refuses when `ci_checks` is absent (writing an empty result that the attestation job skips), records whether the worktree was dirty, runs setup and each named check through the shared runner from `review-proof` (argument arrays, no shell, `CONN_` variables stripped, timeouts, 10 MiB local log cap), and writes the results file. It exits 0 regardless of check outcomes.

## `conn evidence import-ci`

Human-direct CLI and terminal application only.

1. Resolve the Intent (focus or `--intent`) and the commit (`--commit`, or the Intent's head revision). Require the Intent's RepositoryConnection to be a GitHub remote.
2. Integrity: with read-only Git, read `.github/workflows/conn-check.yml` at the commit and at the default branch. Each must equal a version 2 rendering for the `Conn source` reference named in its header, with either comment setting (compared with `lineend.Equal`). Otherwise refuse and say which file differs.
3. Write the subject bytes to a temporary file. Run `gh attestation verify <file> --repo <owner/name> --predicate-type <uri> --signer-workflow <value confirmed in task 3> --deny-self-hosted-runners --format json` with no shell and a 60-second timeout. Missing `gh` or failed `gh` authentication is reported with the command to fix it.
4. From the JSON array, keep entries whose statement subject digest equals the subject's SHA-256, whose predicate validates against `ci-check-attestation`, and whose predicate repository and commit equal the subject's. Pick the latest by verified timestamp and list the rest.
5. For each check result, attach one `check_run` evidence record per acceptance key that the Intent's Objective defines, with state `passing` for exit 0 and not timed out, otherwise `failing`. Acceptance keys the Objective lacks are reported and skipped.
6. Observation: the existing fields with `runner: "github_actions"`, `worktree_dirty` from `worktree_dirty_before`, and a new `attestation` object: `attestation_id`, `attestation_url`, `predicate_type`, `subject_sha256`, `signer_workflow`, `workflow_run_url`, `verified_at`.

Contract change: `evidenceObservation.runner` becomes `["local", "github_actions"]`, and `attestation` is required exactly when the runner is `github_actions`. The backend accepts a `github_actions` observation only from a CLI or terminal-application Participant. The MCP adapter already refuses every observation.

## Review packet

- Evidence ordering per acceptance criterion: CI-attested, then local observed, then reported.
- A CI-attested line shows "Attested by GitHub Actions run <n> at <commit short>", its staleness, and the exact `gh attestation verify` command.
- `--verify-attestations` re-runs step 3 of the import for each CI-attested record and marks it `verified` or `unverified (<reason>)`. Without the flag, the packet says the records were verified at import by the named Participant.
- New integrity note when the branch changes `.github/workflows/conn-check.yml`.
- New fixed note: "CI-attested runs prove the default-branch checks ran on a GitHub-hosted runner at the recorded commit with the recorded result. They do not prove the tests are adequate."

## Security and privacy

- **Pull-request code never shares a job with the OIDC token.** If it did, it could mint its own signing certificate under the Conn workflow identity and sign anything.
- **The signing job does not trust the results' context fields.** It overwrites them from the event, so a forged results file can lie only about outcomes. Code under test can already influence its own test outcomes in any CI system.
- **Workflow tampering.** On `pull_request`, GitHub runs the workflow from the merge ref, so a pull request can edit the workflow. The importer's byte-identity check at the head commit and the default branch, plus the integrity note, detect this.
- **Fork pull requests** get the report job and the check-run job but no attestation.
- **The GitHub token** used by `gh` stays in `gh`. Conn never reads it.
- **Bounds.** Results up to 64 KiB, 20 checks plus setup, and `gh` output up to 10 MiB.

## Error behavior

- No `ci_checks` on the default branch: the check-run job writes an empty result, the attestation job skips, and the report job says attested checks are not configured.
- Attestation unavailable (private repository on an unsupported plan): the attest step fails with `continue-on-error`, and the summary names the plan requirement.
- Import with no attestation found: explain that it may be a fork pull request, the run may not have finished, or the repository may be unsupported, and print the `gh` command used.

## Validation plan

- Subject bytes test and template script comparison.
- Template tests for the permission split, the absence of secrets and checkouts where required, `continue-on-error`, `env`-only expression use, and SHA-pinned actions.
- `conn ci run-checks` test on a temporary repository: passing, failing, and timed-out checks, environment stripping, and no network.
- Import scenario with a fake `gh` executable on `PATH` returning recorded JSON: valid, wrong commit in the predicate, wrong subject digest, modified workflow, several attestations, `gh` missing, `gh` unauthenticated.
- Contract fixtures for `ci-check-attestation`, the `github_actions` observation, and its rejection from an MCP Participant.
- Real trial on a public repository.

## Deferred

- Signature verification inside `conn server`.
- Attesting the review packet itself.
- Pinned per-repository toolchain setup in the check-run job.
- SCIP indexing in CI.

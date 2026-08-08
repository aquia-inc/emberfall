# Go-Native Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development and superpowers:test-driven-development. Every production behavior must begin with a failing test, and every task requires independent specification and quality review.

**Goal:** Implement issue #49 with an entirely Go-native release controller that versions Emberfall from Conventional Commits, synchronizes source and test version literals, creates a canonical changelog, atomically publishes the release commit and tag, feeds GoReleaser deterministic notes, and optionally enhances the published notes with Anthropic.

**Architecture:** A repository-local administrative binary at `cmd/emberfall-release` orchestrates pure release policy, a narrow Git transaction adapter, and optional HTTP clients. GoReleaser remains the sole GitHub Release and Homebrew publisher. Parallel implementation uses isolated worktrees with exclusive file ownership; the integration branch receives only independently reviewed commits.

**Tech Stack:** Go 1.25.0 standard library, Git, GitHub Actions, GoReleaser v2, BATS, Anthropic Messages API, GitHub REST API.

## Global Constraints

- Do not add Node, npm, JavaScript, `.releaserc.json`, or release-time linker injection.
- Preserve synchronized literal versions in `cmd/root.go` and `tests/cli.bats`.
- Release mapping: breaking marker/footer to major; `feat` to minor; `fix`, `perf`, `docs`, and `refactor` to patch; `chore`, `test`, `ci`, merge commits, and malformed commits to no release.
- Generated commit message: `chore(release): bump version to X.Y.Z`; never include a CI skip marker.
- `CHANGELOG.md` is canonical; GoReleaser receives the matching version section via `--release-notes`.
- GoReleaser alone creates the GitHub Release, artifacts, checksums, and Homebrew PR.
- Anthropic enhancement is best effort and must preserve deterministic notes on every failure path.
- Use standard-library Go where practical; do not add third-party release dependencies without explicit approval.
- Preserve unrelated dirty files and never commit secrets or credential values.
- Use signed Conventional Commits for implementation commits.

---

### Task 1: Shared Release Contracts

**Files:**
- Create: `internal/release/types.go`
- Test: `internal/release/types_test.go`

**Interfaces:**
- Produce `Bump` values `None`, `Patch`, `Minor`, `Major`.
- Produce `Version`, `Commit`, and `Plan` structs used by every later task.
- Produce narrow `Repository` and `Enhancer` interfaces without implementation.
- Lock CLI output field names: `releaseNeeded`, `previousVersion`, `version`, `tag`, `bump`, and `commits`.

- [ ] Write compile-time and JSON-contract tests and verify they fail because the types do not exist.
- [ ] Implement only the shared types/interfaces and verify the focused tests pass.
- [ ] Run `go test ./internal/release` and commit as `feat(release): define release automation contracts`.

### Task 2: Release Policy, Changelog, and Version Files

**Files:**
- Create: `internal/release/policy.go`, `internal/release/version.go`, `internal/release/changelog.go`, `internal/release/version_files.go`
- Test: matching `*_test.go` files

**Interfaces:**
- Consume Task 1 types.
- Produce pure commit parsing, bump selection, next-version calculation, changelog rendering/extraction, and atomic version-file preparation.

- [ ] Add failing table tests for every release mapping, breaking header/footer, precedence, ignored/generated commits, malformed subjects, and stable semantic-version parsing.
- [ ] Implement the minimal pure policy and version functions.
- [ ] Add failing tests for deterministic changelog ordering, commit links, prepending, extraction by tag, and duplicate prevention; then implement them.
- [ ] Add failing tests proving both version literals are validated before either write, exactly one target is replaced in each file, unrelated content/permissions/newlines are preserved, and failures leave files unchanged; then implement atomic writes.
- [ ] Run `go test ./internal/release` and commit as `feat(release): add version planning and changelog policy`.

### Task 3: Git Repository and Atomic Publisher

**Files:**
- Create: `internal/release/repository.go`, `internal/release/publisher.go`
- Test: matching `*_test.go` files

**Interfaces:**
- Implement Task 1 `Repository` with `os/exec` Git commands.
- Produce release planning inputs and `Publish(version)` behavior without owning commit classification.

- [ ] Build temporary Git and bare-remote fixtures, then add failing tests for reachable annotated/lightweight tags, commit ranges, clean-state enforcement, missing baselines, tag collisions, non-fast-forward updates, retries, and branch checks.
- [ ] Implement the repository adapter minimally.
- [ ] Add failing tests proving the publisher commits only `cmd/root.go`, `tests/cli.bats`, and `CHANGELOG.md`, creates the required message/tag, and pushes `main` plus tag atomically.
- [ ] Implement publication without force pushes or tag replacement.
- [ ] Run the focused temporary-repository suite and commit as `feat(release): add atomic git publisher`.

### Task 4: Go-Based Release Notes Enhancement

**Files:**
- Create: `internal/release/anthropic.go`, `internal/release/github.go`, `internal/release/enhancer.go`
- Test: matching `*_test.go` files

**Interfaces:**
- Implement Task 1 `Enhancer` using injected standard-library HTTP clients.
- Consume deterministic notes and tagged commit context; return validated Markdown only.

- [ ] Add failing `httptest` cases for Anthropic headers/payloads, success, timeout, `429`, `5xx`, malformed/empty output, bounded retries, and secret-safe errors.
- [ ] Implement the Anthropic client.
- [ ] Add failing GitHub tests for tag lookup, release retrieval, body-only patching, API headers, error classification, and no PATCH after enhancement failure.
- [ ] Implement the GitHub client and orchestration.
- [ ] Add failing tests for bounded UTF-8 commit/diff context, required-link preservation, the `<!-- emberfall-claude-notes:v1 -->` marker, and idempotent reruns; then implement validation.
- [ ] Run `go test ./internal/release` and commit as `feat(release): add optional Claude release notes`.

### Task 5: Administrative CLI and End-to-End Orchestration

**Files:**
- Create: `cmd/emberfall-release/main.go`, `internal/release/service.go`
- Test: `cmd/emberfall-release/main_test.go`, `internal/release/integration_test.go`

**Interfaces:**
- Expose `plan --json`, `prepare --github-output`, `publish --version`, `notes --tag`, and `enhance-notes --tag` exactly.
- `prepare` may change managed files but never refs; `publish` validates prepared state before committing/pushing.

- [ ] Add failing CLI tests for help, required flags, JSON/output contracts, exit codes, no-release behavior, and dependency failures.
- [ ] Implement command parsing and service orchestration using only the reviewed Task 1-4 interfaces.
- [ ] Add failing temporary-repository integration tests for plan/prepare/publish, exact managed files, dry/no-op retry behavior, and atomic remote refs; then implement missing glue.
- [ ] Run `go test ./...` and commit as `feat(release): add Go release administration CLI`.

### Task 6: GitHub Actions and GoReleaser Integration

**Files:**
- Create: `.github/workflows/semantic-release.yml`
- Modify: `.github/workflows/release.yaml`, `.github/workflows/tests.yaml`

**Interfaces:**
- Consume Task 5 commands and outputs exactly.
- Preserve `.github/workflows/pipeline.yaml` tag gating and GoReleaser ownership.

- [ ] Add workflow validation expectations before authoring YAML.
- [ ] Add the `main`/manual semantic release workflow with non-cancelling concurrency, App-token checkout, full history, existing analysis/tests gates, Go setup, prepare verification, and publish.
- [ ] Change tests compilation to `go build -o ./emberfall .` so the administrative command is not emitted into the repository root.
- [ ] Generate `release-notes.md` from `notes --tag` before GoReleaser and pass it with `--release-notes`.
- [ ] Add a post-GoReleaser best-effort `enhance-notes` job using `ANTHROPIC_API_KEY`, optional `RELEASE_NOTES_MODEL`, and `GITHUB_TOKEN`.
- [ ] Run Actionlint and GoReleaser configuration checks, then commit as `ci(release): automate semantic releases`.

### Task 7: Documentation, Integration Verification, and Acceptance

**Files:**
- Modify: `CONTRIBUTING.md`
- Test: extend integration tests only when a missing acceptance behavior is first reproduced as a failure.

**Interfaces:**
- Document the final, verified CLI and workflow behavior; do not document speculative recovery paths.

- [ ] Document commit-to-version rules, required secrets, optional model variable, generated release commit/tag, deterministic fallback, and operator recovery.
- [ ] Run the complete serial verification suite from the plan, including race tests, build, BATS when available, Actionlint, GoReleaser snapshot, and tracked-module diff.
- [ ] Perform a requirement-by-requirement completion audit against issue #49 and this plan.
- [ ] Commit documentation as `docs(release): document automated releases`.


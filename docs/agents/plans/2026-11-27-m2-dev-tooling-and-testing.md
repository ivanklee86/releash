# Plan: M2 dev tooling and testing

Started 2026-11-27. Status: in progress. Milestone: [M2](../milestones/M2-dev-tooling-and-testing.md).

## Resume here

Updated 2026-11-27. PRs 1–3 were built in parallel worktrees and reviewed, then rebased into one linear stack, because PR 1 and PR 2 both edit `docs/agents/internals.md`: `m2/quality` (PR 1), then `m2/compose` (PR 2), then `m2/integration-tests` (PR 3). All three are committed locally and pass `task`, `prek run --all-files` and `task go:test-ci` against the stack. Next: the user ships them in order, and each later branch is rebased onto `main` after the one before it merges. PR 4 (CI jobs) starts once PR 3 merges. When resuming, start the first PR in [Steps](#steps) that is not checked off and whose dependencies are merged. Before ending a session, update this section and check off finished steps.

## How to work through this plan

- One PR per step. Read this plan, the M2 milestone and `AGENTS.md` first.
- Branch from an up-to-date `main`, named as in the step. PR 3 may be stacked on PR 2's branch before PR 2 merges.
- Before handing off, run `task` and `prek run --all-files`, plus what the step's checks list.
- The user runs `/ship` to commit, push, open the PR and watch it. Claude never merges. The user's merge is the checkpoint before a step that depends on it.
- PRs 1 and 2 are independent and may be built in parallel in separate git worktrees, but they are merged in order, because both edit `docs/agents/internals.md`. PR 3 needs PR 2. PR 4 needs PRs 1 and 3.
- Only PR 1 edits this plan while PRs 1–3 are open, so parallel branches do not conflict on it. Later PRs update it after rebasing.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing. Record the answer here, or in an ADR if it has real trade-offs.

## Reference

argonap (`ivanklee86/argonap`) is the pattern for prek, markdownlint, Renovate, the CI jobs and octocov. It has no compose stack: the Unleash stack is new to releash.

Decisions made while planning:

- The seed script is Python (standard library only) in a pinned `python:<version>-alpine` image, because the seed needs HTTP and JSON handling and the image needs nothing installed at runtime. The fixture flags live in `unleash/fixtures.json` next to `unleash/seed.py`.
- Compose images are pinned by tag and digest (`image: name:tag@sha256:...`), so Renovate can update both.
- The fixed dev tokens use Unleash's token format (`<project>:<environment>.<secret>` for client tokens, `*:*.<secret>` for the admin token), with secrets that say they are not secret, e.g. `default:production.releash-dev-not-a-secret`.
- Renovate's `devcontainer` manager is disabled, so there is no `devcontainer` group. Devcontainer features are owned by gantry's shared base (`ivanklee86/devcontainers`) and updated there; Renovate still updates the base image digest in `.devcontainer/Dockerfile` through the `dockerfile` manager.
- Only minor, patch, pin and digest updates automerge, as the milestone says. There is no blanket rule that automerges major updates.
- Go version (`golang-version`) updates never automerge, whatever their size. A `go` directive bump must land together with the devcontainer base image tag, because the devcontainer's golangci-lint is built with the image's Go.
- The Unleash server creates the client tokens through `INIT_BACKEND_API_TOKENS` instead of the deprecated `INIT_CLIENT_API_TOKENS`. PR 2 rewords the milestone line that names the old variable.
- Postgres is pinned to major 18 rather than 19, because 19 had been out only 8 days when the plan was checked.

Pinned versions, checked 2026-11-27. Check for newer releases at the start of each PR:

| Tool or image | Version |
|---|---|
| `unleashorg/unleash-server` | 8.3.1 |
| `postgres` | 18 (exact tag chosen in PR 2) |
| `python` (seed) | newest stable `-alpine` |
| `github.com/Unleash/unleash-go-sdk/v6` | v6.5.1 (M4 adds it; listed for the Renovate group) |
| Docker Compose (devcontainer) | v5.8.0 |
| markdownlint-cli2 hook | v0.24.2 |
| renovatebot/pre-commit-hooks | 45.1.1 |
| prek | v0.5.12 (CI pins this in prek-action) |
| j178/prek-action | v3.1.1 |
| EnricoMi/publish-unit-test-result-action | v2.26.0 |
| k1LoW/octocov-action | v1.5.2, octocov v0.83.5 |
| go-junit-report | v2.1.0 |

## Steps

### PR 1: quality hooks and Renovate

- [ ] Branch `m2/quality`.
- Contents: this plan; `.pre-commit-config.yaml` gains `markdownlint-cli2` and `renovate-config-validator`, as in argonap; `.markdownlint-cli2.yaml` (MD013 off, MD024 siblings only); markdown fixes so the hooks pass on all files; `renovate.json` as the milestone's Renovate section describes, adapted from argonap's (runs on Fridays, `minimumReleaseAge` 5 days, groups `go`, `ci` and `unleash`, the devcontainer manager disabled, automerge rules, regex managers for `go install` pins and `# <datasource>/<depName>` comments, digest pinning for compose images and the devcontainer base image); `docs/agents/internals.md` sections for prek and Renovate.
- Checks: `prek run --all-files` passes, including the Renovate validator.
- Milestone criteria: Renovate; prek hooks pass on all files.
- Depends on: nothing.

### PR 2: local Unleash stack

- [ ] Branch `m2/compose`.
- Contents: `docker-compose.yaml` (Postgres, Unleash on `localhost:4242`, a one-shot `seed` service, and a `ready` service that `depends_on` the seed with `condition: service_completed_successfully`); `unleash/seed.py` and `unleash/fixtures.json`; `tasks/unleash.yaml` with `services`, `services:down` and `releash`; the root Taskfile includes it; `.env.example`; `docs/agents/internals.md` sections for the stack and the fixture flags.
- Fixture flags: everything the milestone lists. Check whether the pinned open-source image supports a second project. If it doesn't, drop that fixture and add the note to spec 0001 that the milestone asks for.
- Checks: `task services` exits 0 from a clean state and again on a running stack; `task services:down` removes the volumes; re-running the seed is a no-op; the Client API returns the expected flags for each client token (checked by hand with `curl`).
- Milestone criteria: Local Unleash; `task services`, `task services:down`, `task releash`, `.env.example`.
- Depends on: nothing.

### PR 3: integration test scaffolding

- [ ] Branch `m2/integration-tests`, stacked on `m2/compose`.
- Contents: an `integration` package or `_test.go` files behind the `integration` build tag; helpers for the fixture-flag catalog (read from `unleash/fixtures.json`), client and admin tokens from the environment with compose defaults, a reachability check that fails naming `task services`, and a fake HTTP server that accepts connections and never responds; scenario stubs named after pipeline usage that skip with a note naming M3 or M4; one passing smoke test that fetches flags through the Client API with each client token; `tasks/go.yaml` gains `test-integration` and `test-ci` (JUnit report and coverage profile, as argonap's, with `pipefail`) and installs go-junit-report in `install-ci`; `docs/agents/internals.md` section on running each kind of test.
- Checks: `go test ./...` passes with Docker stopped; `task go:test-integration` passes against the stack; `task go:test-ci` writes `report.xml` and `coverage.out`.
- Milestone criteria: Tests; `task go:test`, `task go:test-integration`, `task go:test-ci`.
- Depends on: PR 2.

### PR 4: CI jobs

- [ ] Branch `m2/ci`.
- Contents: `ci.yaml` gains a `pre-commit` job (prek-action, SHA-pinned), an `integration` job (`task services`, `task go:test-ci`, compose logs on failure, publish-unit-test-result-action and octocov, both SHA-pinned, with the write permissions they need), and `.octocov.yml`.
- Checks: every job passes on the PR, and on `main` after merging.
- Milestone criteria: Quality and CI.
- Depends on: PRs 1 and 3.

### Close out

- [ ] Every M2 exit criterion holds. Set M2 to `done`, and update this plan's status. Confirm the Renovate app is enabled for the repository (milestone risk).

# Plan: M1 skeleton MVP

Started 2026-11-27. Status: in progress. Milestone: [M1](../milestones/M1-skeleton-mvp.md).

## Resume here

Updated 2026-11-27. PR 1 (planning docs) is staged on branch `docs/initial-planning` and waiting for the user to run `/ship`. When resuming, start the first PR in [Steps](#steps) that is not checked off and whose dependencies are merged. Before ending a session, update this section and check off finished steps.

## How to work through this plan

- One PR per step, each in its own session. Read this plan, the M1 milestone and `AGENTS.md` first.
- Branch from an up-to-date `main`, named as in the step.
- Before handing off, run what the step's checks list. From PR 4 on, that is `task` (format, lint, build, test).
- The user runs `/ship` to commit, push, open the PR and watch it. Claude never merges. The user's merge is the checkpoint before the next step.
- PRs 2 and 3 are independent, so they may run in parallel in separate git worktrees. PR 4 needs PR 3 merged.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing. Record the answer here, or in an ADR if it has real trade-offs.

## Reference

argonap (`../argonap`) is the pattern for everything in M1. Copy its structure and trim what is Kubernetes- or Argo CD-specific:

- `.devcontainer/` (gantry overlays, Dockerfile, `post_install.sh`)
- `main.go` (cobra root command, viper binding through `initializeConfig` and `bindFlags`) and `pkg/cli/argonap.go` (orchestrator with `Config`, `New`, `NewWithConfig`, `Out`, `Err`)
- `Taskfile.yaml`, `tasks/go.yaml`, `.golangci.yml`
- `.github/workflows/ci.yaml` (`lint` and `test` jobs only) and `.github/actions/setup-task/action.yml`

Known differences from argonap:

- The orchestrator lives in `internal/releash`, not `pkg/cli`.
- Orchestrator methods return an exit code instead of printing and carrying on. `main` is the only place that calls `os.Exit`.
- `Err` defaults to `os.Stderr`. argonap's `New` sets it to `os.Stdin`, which is a bug that should not be copied.
- No go-pretty or colour output. releash prints plain text.
- Cobra's default exit code for argument and flag errors is 1. releash maps them to 2 (ADR 0004).
- releash's environment variables are `UNLEASH_URL` and `UNLEASH_API_TOKEN` (spec 0001), not a `RELEASH_` prefix. M1 has no options to bind yet, but the binding code should not assume a single prefix.

Pinned versions, matching the devcontainer base image `ghcr.io/ivanklee86/devcontainer/go:1.27` (pinned by digest in `.devcontainer/Dockerfile`) where it has the tool:

| Tool or module | Version |
|---|---|
| Go | 1.27.4 |
| `github.com/spf13/cobra` | v1.10.2 |
| `github.com/spf13/viper` | v1.21.0 |
| `github.com/stretchr/testify` | v1.12.1 |
| golangci-lint | v2.15.0 |
| go-junit-report | v2.1.0 |
| Task (CI) | 3.54.0 |

Check for newer releases at the start of each PR, and use them if they are available, unless the base image pins an older one.

## Steps

### PR 1: planning docs

- [ ] Branch `docs/initial-planning`.
- Contents: `AGENTS.md`, `docs/specs/0001-rollout-decisions.md`, `docs/specs/0002-distribution.md`, `docs/adrs/0001`–`0005`, `docs/agents/milestones/` (template and M1–M5), `docs/agents/plans/`.
- Checks: links between docs resolve.
- Depends on: nothing.

### PR 2: devcontainer

- [ ] Branch `devcontainer`.
- Contents: `.devcontainer/gantry.yaml`, `releash.libsonnet`, `Dockerfile`, `post_install.sh`, and the generated `devcontainer.json` and `devcontainer-lock.json` (if gantry or the devcontainer CLI produces one). These were already generated on 2026-11-27 and are untracked in the working tree.
- Checks: `gantry build --config gantry.yaml --write` in `.devcontainer/` gives no diff; `docker build .devcontainer` succeeds.
- Milestone criteria: devcontainer.
- Depends on: PR 1.

### PR 3: Go skeleton

- [ ] Branch `go-skeleton`.
- Contents: `go.mod` and `go.sum` (`module github.com/ivanklee86/releash`, `go 1.27.4`); `main.go` with `NewRootCommand`, `initializeConfig`, `bindFlags` and the `version` variable; `internal/releash` with `Config`, `Releash`, `New` and `NewWithConfig`; tests for `main` and `internal/releash`; README usage section.
- Behaviour:
  - `releash` and `releash --help` print help to stdout and exit 0.
  - `releash --version` prints the injected `version` if set. Otherwise it prints the main module version from `runtime/debug.ReadBuildInfo` when that is not empty or `(devel)`, and otherwise `dev`. It exits 0.
  - An unknown command or option prints an error to stderr and exits 2.
- Tests, written first as a list in the PR description, then as code: no arguments; `--help`; `--version` with an injected value, with only build info, and with neither (make the build-info lookup a variable or parameter the test can replace); unknown command; unknown option. Each asserts stdout, stderr and the exit code.
- Checks: `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` is empty.
- Milestone criteria: module layout, cobra and viper, help, version, exit 2, `go build` and `go test`.
- Depends on: PR 1.

### PR 4: Task, lint and CI

- [ ] Branch `ci`.
- Contents: `Taskfile.yaml` and `tasks/go.yaml` (`install`, `install-ci`, `fmt`, `lint`, `build`, `test`); `.golangci.yml` (with `gofmt` and `goimports` formatters); `.github/actions/setup-task/action.yml`; `.github/workflows/ci.yaml` with `lint` and `test` jobs; `.gitignore` additions for `bin/`, `dist/`, `.task/` and coverage files; `docs/agents/internals.md` covering the layout, the orchestrator pattern, tasks and CI.
- Checks: `task` passes locally; CI passes on the PR.
- Milestone criteria: Task, golangci-lint, CI, `internals.md`.
- Depends on: PR 3.

### Close out

- [ ] Every M1 exit criterion holds. Set M1 to `done`, check off its scope, and update this plan's status.

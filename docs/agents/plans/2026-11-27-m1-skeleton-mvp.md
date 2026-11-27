# Plan: M1 skeleton MVP

Started 2026-11-27. Status: in progress. Milestone: [M1](../milestones/M1-skeleton-mvp.md).

## Resume here

Updated 2026-11-27. PRs 1–3 are merged (#2, #4, #5). PR 4 is open as #7 on branch `ci`, and is waiting for CI and the user's merge. After PR 4 merges with CI green, check it off and do the close-out step. The user decided that a stray argument together with `--help` or `--version` may exit 0, and the M1 exit criterion now says so. When resuming, start the first PR in [Steps](#steps) that is not checked off and whose dependencies are merged. Before ending a session, update this section and check off finished steps.

## How to work through this plan

- One PR per step, each in its own session. Read this plan, the M1 milestone and `AGENTS.md` first.
- Branch from an up-to-date `main`, named as in the step.
- Before handing off, run what the step's checks list. From PR 4 on, that is `task` (format, lint, build, test).
- The user runs `/ship` to commit, push, open the PR and watch it. Claude never merges. The user's merge is the checkpoint before the next step.
- PRs 2 and 3 are independent, so they may run in parallel in separate git worktrees. PR 4 needs PR 3 merged.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing. Record the answer here, or in an ADR if it has real trade-offs.

## Reference

argonap (`../argonap`) is the pattern for everything in M1. Copy its structure and trim what is Kubernetes- or Argo CD-specific:

- `.devcontainer/` (gantry overlays, Dockerfile). releash has no `post_install.sh`: argonap's sets up GPG signing, which releash doesn't want, and its `git config --global --add` appends to the host `~/.gitconfig` (bind-mounted into the container) on every container create.
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
| `github.com/spf13/pflag` | v1.0.10 (v1.1.0 renders bool flags as `--help[=true\|false]` in help output, and a test guards against it) |
| `github.com/stretchr/testify` | v1.12.1 |
| golangci-lint | v2.15.0 |
| go-junit-report | v2.1.0 |
| Task (CI) | 3.54.0 |

Check for newer releases at the start of each PR, and use them if they are available, unless the base image pins an older one.

## Steps

### PR 1: planning docs

- [x] Branch `docs/initial-planning`. Merged as #2.
- Contents: `AGENTS.md`, `docs/specs/0001-rollout-decisions.md`, `docs/specs/0002-distribution.md`, `docs/adrs/0001`–`0005`, `docs/agents/milestones/` (template and M1–M5), `docs/agents/plans/`.
- Checks: links between docs resolve.
- Depends on: nothing.

### PR 2: devcontainer

- [x] Branch `devcontainer`. Verified 2026-11-27: gantry regenerates with no diff, and the image builds with Go 1.27.4, golangci-lint v2.15.0 and prek 0.5.11.
- Contents: `.devcontainer/gantry.yaml`, `releash.libsonnet` (sets only the name), `Dockerfile`, the generated `devcontainer.json`, and `devcontainer-lock.json`, which pins the devcontainer features by digest and is written by VS Code's Dev Containers extension when it builds the container. Also a minimal `.pre-commit-config.yaml` (whitespace, YAML, merge-conflict and large-file checks, plus `golangci-lint fmt`, `golangci-lint run` and `go mod tidy -diff`), because the devcontainer's post-create step runs `prek install && prek run`, which fails without a config. M2 adds markdownlint and the Renovate config hook.
- Checks: `gantry build --config gantry.yaml --write` in `.devcontainer/` gives no diff; `docker build .devcontainer` succeeds.
- Milestone criteria: devcontainer.
- Depends on: PR 1.

### PR 3: Go skeleton

- [x] Branch `go-skeleton`. Merged as #5. Panics in commands are recovered and exit 1. Cobra's default `completion` command is disabled because it exits 0 on unknown subcommands. Cobra's hidden `__complete` command can't be disabled and is left in place.
- Contents: `go.mod` and `go.sum` (`module github.com/ivanklee86/releash`, `go 1.27.4`); `main.go` with `NewRootCommand`, `initializeConfig`, `bindFlags` and the `version` variable; `internal/releash` with `Config`, `Releash`, `New` and `NewWithConfig`; tests for `main` and `internal/releash`; README usage section.
- Behaviour:
  - `releash` and `releash --help` print help to stdout and exit 0.
  - `releash --version` prints the injected `version` if set. Otherwise it prints the main module version from `runtime/debug.ReadBuildInfo` when that is not empty or `(devel)`, and otherwise `dev`. It exits 0.
  - An unknown command or option prints an error to stderr, leaves stdout empty, and exits 2. A stray argument together with `--help` or `--version` prints help or the version and exits 0 instead (see the M1 milestone). The `true`/`false` stdout contract covers only `releash check` (spec 0001).
- Tests, written first as a list in the PR description, then as code: no arguments; `--help`; `--version` with an injected value, with only build info, and with neither (make the build-info lookup a variable or parameter the test can replace); unknown command; unknown option. Each asserts stdout, stderr and the exit code.
- Checks: `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` is empty.
- Milestone criteria: module layout, cobra and viper, help, version, exit 2, `go build` and `go test`.
- Depends on: PR 1.

### PR 4: Task, lint and CI

- [ ] Branch `ci`, rebased onto `main`. Open as #7. CI runners are pinned to `ubuntu-26.04`, and only the lint job caches `~/go/bin`, keyed on the Go version and `tasks/go.yaml`. `renovate.json` gains regex managers for the `go install` pins in `tasks/go.yaml` and Task's version in the setup-task action. Third-party GitHub Actions are pinned to full commit SHAs with a `# vX.Y.Z` comment (argonap uses major tags), so a moved or compromised tag cannot change what CI runs. `install-ci` installs only golangci-lint, because go-junit-report is needed only by M2's test reports.
- Contents: `Taskfile.yaml` and `tasks/go.yaml` (`install`, `install-ci`, `fmt`, `lint`, `build`, `test`); `.golangci.yml` (with `gofmt` and `goimports` formatters); `.github/actions/setup-task/action.yml`; `.github/workflows/ci.yaml` with `lint` and `test` jobs; `.gitignore` additions for `bin/`, `dist/`, `.task/` and coverage files; `docs/agents/internals.md` covering the layout, the orchestrator pattern, tasks and CI.
- Checks: `task` passes locally; CI passes on the PR.
- Milestone criteria: Task, golangci-lint, CI, `internals.md`.
- Depends on: PR 3.

### Close out

- [ ] Every M1 exit criterion holds. Set M1 to `done`, check off its scope, and update this plan's status.

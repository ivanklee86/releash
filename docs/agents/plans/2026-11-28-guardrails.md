# Guardrails that run without being asked

- Status: done
- Issue: [#37](https://github.com/ivanklee86/releash/issues/37), part 2
- Branch: `guardrails` (one branch per pull request below)

## Goal

Rules that today live only as prose in `CLAUDE.md`, or as one-off steps in a plan, are checked on every pull request: lint rules for the Go style `CLAUDE.md` asks for, a coverage floor and workflow linting. Milestones get a complexity budget so a new feature states up front how much code it may add.

## Baseline

Measured on `main` at the start of this plan, with golangci-lint 2.15.0, actionlint 1.7.12 and zizmor 1.33.1.

| Check | Findings today |
|---|---|
| `revive` (default rules, incl. `exported`) | 2: `ReleashOptions` stutters, one unused parameter in a test |
| `errorlint`, `gocritic`, `exhaustive` | 0 |
| `gosec` | 4: G101 on the dev-only tokens in `main_test.go` and `internal/integration`, G204 on the `/bin/sh` runs in two tests |
| `gocognit` at 20 | 2: `validateCheck` (40), `evaluate` (23). At 15 it is 6 functions. |
| `cyclop` at 15 | 2, the same functions |
| `nolintlint`, `misspell`, `bodyclose` | 0 |
| `testifylint` | 5, all mechanical (`assert.ErrorIs`, `assert.Empty`) |
| `noctx` | 11, all in `internal/integration` test helpers |
| Coverage (octocov, unit and integration) | 97.0% |
| actionlint | 7, all "unknown runner label `ubuntu-26.04`" |
| zizmor (default persona, offline) | 6 `self-repository` (help level): local actions referenced as `./.github/actions/...` |

The code is already close to every rule, so this is mostly about making the current state stick.

## Pull requests

### 1. Linters and the complexity budget

- `.golangci.yml` enables, on top of `standard`: `revive`, `errorlint`, `gocritic`, `gocognit` (threshold 20), `exhaustive`, `gosec`, `nolintlint` (every `//nolint` names its linter and gives a reason), `misspell`, `bodyclose`, `testifylint` and `noctx`. Each gets a one-line comment saying which `CLAUDE.md` rule or failure it guards against. `cyclop` is left out, because it flags the same two functions as `gocognit` and one complexity metric is enough.
- Fix the findings rather than suppress them:
  - Split `validateCheck` into one validator per option group, so it drops below 20 without changing behaviour; the existing tests in `check_test.go` cover every exit code and must pass unchanged.
  - Bring `evaluate` from 23 to under 20 by moving the post-`Close` handling (evaluation error, address note, register note) into a helper.
  - Rename the unused test parameter to `_`; apply the five `testifylint` fixes; use `NewRequestWithContext(t.Context(), ...)` and `net.ListenConfig` in `internal/integration`.
  - G101 on the dev-only tokens and G204 on the `/bin/sh` tests get `//nolint:gosec // <reason>`, since both are deliberate.
  - `ReleashOptions` stutters on purpose: it names the "Releash options" help heading. Suppress the stutter warning on that one function with `//nolint:revive` and a reason, rather than rename it or turn the check off for the whole repository.
- Complexity budget: add a "Complexity budget" section to `docs/agents/milestones/TEMPLATE.md`, and a line to the specs bullet in `CLAUDE.md` saying new specs carry one too (the repository has no spec template file; specs follow the Rust RFC template). The section states, before work starts: new packages, new exported identifiers, new direct dependencies, and an estimate of product (non-test) lines added. It also states that no function may exceed the `gocognit` threshold, and that raising the threshold needs an ADR.
- Docs: the linting row in `docs/architecture.md` "Key technology" if it changes.

### 2. Coverage floor and workflow linting

- `.octocov.yml`: replace the "no gate yet" comment with `coverage.acceptable: current >= 95% && diff >= -1%`. The floor leaves two points of headroom under today's 97.0%; the diff rule stops a pull request from quietly lowering it. octocov runs in the required `integration` job, so the gate is enforced. Without a baseline from `main` (a fork, or the first run) only the floor applies.
- actionlint: add the `rhysd/actionlint` prek hook, pinned by tag. Add `.github/actionlint.yaml` declaring `ubuntu-26.04` as a known runner label, with a comment to remove it once actionlint knows the label.
- zizmor: add the `zizmorcore/zizmor-pre-commit` prek hook, pinned by tag, run offline. Turn off `self-repository` (decision 2), so it starts at zero findings.
- Both run in CI through the existing `pre-commit` job, because it runs every prek hook. Check during implementation that the actionlint hook runs there without Go installed; if it does not, use the hook's Docker variant. Renovate's `pre-commit` manager already updates hook versions, in the `ci` group.
- Docs: the `pre-commit` row of the CI table in `docs/architecture.md`, and remove the M5 plan's "run by hand" note only if it is stated as current anywhere outside history.

## Deferred

Fuzz tests for the parsers in `internal/variant` and `internal/releash`, and a short non-required `fuzz` job in CI, are tracked in [#41](https://github.com/ivanklee86/releash/issues/41).

## Decisions

1. Complexity threshold: `gocognit` at 20, refactoring `validateCheck` and `evaluate` to get under it.
2. zizmor `self-repository`: turned off in `.github/zizmor.yml` for now, with a comment saying why.

## Verification

- `task` and `prek run --all-files` pass with every new check enabled, with no blanket exclusions.
- The `pre-commit` job runs actionlint and zizmor, and fails on a deliberately broken workflow (checked once on a throwaway commit).
- The `integration` job fails when coverage drops below the floor (checked once locally with `octocov` against a trimmed profile, or by reading the job's output).

## Outcome

- Pull request 1 merged as #42.
- Pull request 2: actionlint 1.7.12 and zizmor 1.33.1 run as prek hooks and pass on every workflow. Both fail on a throwaway workflow with an unpinned action and a template injection. The coverage floor was checked with octocov 0.83.5, which only enforces `acceptable` when `CI` is set: with `CI=true`, a 99% floor fails on a 96.1% profile with "the condition in the `coverage.acceptable:` section is not met", and the 95% floor passes. With no earlier report, `diff` is measured against 0%, so only the floor applies.

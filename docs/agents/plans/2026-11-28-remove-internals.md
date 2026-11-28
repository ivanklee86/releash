# Remove `docs/agents/internals.md`

- Status: done
- Issue: [#37](https://github.com/ivanklee86/releash/issues/37), part 1
- Branch: `docs/remove-internals`

## Goal

Delete `docs/agents/internals.md` without losing anything a reader needs. Each fact in it ends up in one of four places, or is dropped because the code, a spec, an ADR or git history already says it:

- **Spec or ADR**: anything a user or pipeline relies on (output, exit codes, accepted values).
- **Code comment**: the *why* of an implementation choice, next to the code it explains.
- **Config comment**: the *why* of a setting in `renovate.json`, a workflow, `docker-compose.yaml` and so on, next to the setting.
- **`docs/architecture.md`**: the code layout, how tests are organised, CI, the release workflow, and repository settings that live outside the repository (the `main` ruleset).

## Mapping

| internals.md section | Destination | Notes |
|---|---|---|
| Status | Dropped | Milestones, plans and release notes hold it. |
| Documentation layout | Dropped | `AGENTS.md` already defines it. |
| Code layout | `architecture.md`, new "Code layout" section | One line per path, no function lists. |
| The orchestrator pattern | Mostly dropped; gaps into `main.go` comments | `architecture.md` already states the pattern; `run`, `answerCommandIn` and the panic path are commented in `main.go`. |
| The check command | Spec 0001 for any validation rule it lacks; `main.go` and `check.go` comments for the rest | `countedValue`, `plainValue` and `parseRFC3339` are already commented. |
| The variant command | Spec 0003 for any output or exit rule it lacks; `variant.go` comments for the rest | |
| Evaluation (storage, time limit, readiness, fallback reasons, remoteAddress, registration, metrics) | `internal/releash` comments; spec 0001 "Implementation" for user-visible effects | Most is already in `evaluate.go`, `metrics.go` and `remoteaddress.go`. Known gaps: why `Ready()`/`WaitForReady` are not used, sub-second metrics interval sent as `0`, proxies and `remoteAddress`. |
| Variant details (stickiness, `ListFeatures`, `groupId`) | `variant.go` comments | |
| stderr line table | Dropped | Decision 1. |
| Versions | Spec 0002, and `main.go` comments | |
| Options and environment variables | Dropped | Spec 0001 "Configuration" covers it. |
| Variant variables | `internal/variant` comments | Spec 0003 and ADR 0006 hold the rules. |
| prek, markdownlint | Comments in `.pre-commit-config.yaml` and `.markdownlint-cli2.yaml` | |
| Renovate | `description` fields in `renovate.json` | Group rationale, what never automerges and why, refreshing `devcontainer-lock.json`. |
| Tasks | `desc:` fields in the Taskfiles | `task --list` shows them. |
| CI | `architecture.md`, new "CI" section | Jobs in one line each, required checks and the ruleset, SHA pinning. The actionlint/zizmor paragraph is dropped: part 2 of #37 runs them in CI. |
| Release build | Dropped, with gaps into `.goreleaser.yaml` comments | Spec 0002 is the contract. |
| Release workflow | `architecture.md` "Release pipeline"; `release.yaml` comments | Keep the tap-token preflight, the "never re-run, tag the next version" recovery rule, cosign v3 wording and the README checkout SHA reminder. |
| Local Unleash stack, tokens, tasks | `docker-compose.yaml`, `.env.example`, `tasks/unleash.yaml` comments; a short "Local development" part of `architecture.md` | Upgrade procedure goes next to the Unleash image pin. |
| Fixture flags | Dropped, with Admin API quirks into `seed.py` comments | `fixtures.json` is the source and the smoke test checks it. |
| Tests | `architecture.md`, new "Tests" section | Two kinds of test, the build tag, how to run one, when to use `CreateFlag` or `FakeUnleash`. The per-file tables are dropped. |

## Decisions

1. The exact stderr lines (`releash: unleash unreachable: ...; returning false` and so on) stay in the code and the tests that assert them, not in a spec. Spec 0001 keeps its one example, and the wording is not a contract.
2. All five steps go in one pull request without pausing between them.

## Steps

The steps went in as one pull request (decision 2).

1. Code comments in `main.go`, `internal/releash` and `internal/variant` for the gaps found.
2. Spec 0001, spec 0002 and spec 0003 additions, if any contract is missing.
3. Config comments: `renovate.json`, Taskfiles, `docker-compose.yaml`, `.env.example`, `seed.py`, workflows, `.goreleaser.yaml`, prek and markdownlint.
4. `docs/architecture.md`: Code layout, Tests, CI, Release pipeline, Local development.
5. Delete `docs/agents/internals.md`, remove its line from `AGENTS.md`, and fix any remaining links to it outside plans and milestones.

## Verification

- `task` and `prek run --all-files` pass.
- `grep -rn internals.md` finds only plans and milestones.
- Every section of internals.md is ticked off against the mapping above.

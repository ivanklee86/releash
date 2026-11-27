# Plan: M3 check command contract

Started 2026-11-27. Status: in progress. Milestone: [M3](../milestones/M3-check-command-contract.md).

## Resume here

Updated 2026-11-27. PR 1 (the `check` command) is implemented and committed on branch `m3/check`, not yet pushed. `task`, `prek run --all-files` and `task go:test-integration` pass locally. Next: the user runs `/ship` for PR 1; once it is merged, do the close-out step. When resuming, start the first PR in [Steps](#steps) that is not checked off and whose dependencies are merged. Before ending a session, update this section and check off finished steps.

Decisions made while building PR 1, within the ones below:

- Option names, the two option groups and the defaults are exported from `internal/releash` (`OptionURL` and friends, `ReleashOptions`, `ContextOptions`, `DefaultTimeout`, `DefaultEnvironment`, `DefaultAppName`), so help order, duplicate detection and validation share one list.
- `CheckOptions.Given` counts how many times each option was set. `main.go` fills it by wrapping each flag's `pflag.Value`. A value that `bindFlags` sets from `UNLEASH_URL` or `UNLEASH_API_TOKEN` counts once, which only happens when the option was not given, so it never makes a repeat. `--properties` is not wrapped: it may be repeated, and repeats are found by property name.
- `--properties` is a `StringArray` flag, not a `StringSlice`, so values containing commas are kept whole.
- Check reports every problem it finds, one line starting with `releash:` each, and exits with the lowest code. The `Run 'releash check --help' for usage.` line is added when the exit code is 2.
- `releash check --version` is an unknown option of `check` (exit 2, `false` on stdout); `--version` belongs to the root command only.
- `releash check ""` counts as a missing flag name (exit 2).

## How to work through this plan

- One PR per step. Read this plan, the M3 milestone, spec 0001 and `AGENTS.md` first.
- Branch from an up-to-date `main`, named as in the step.
- Before handing off, run `task`, `prek run --all-files`, and `task go:test-integration` against the stack (`task services`).
- The user runs `/ship` to push, open the PR and watch it. Claude never merges.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing. Record the answer here, or in an ADR if it has real trade-offs.

## Reference

M3 is one PR. Parsing, validation, exit-code precedence and the stdout contract depend on each other closely enough that splitting them across parallel branches would cost more in integration than it saves.

Decisions made while planning:

- Validation lives in `internal/releash`: a `CheckOptions` value holds what the command line gave (including which options were given and how many times), and validation returns every problem with its exit code. The lowest code wins (spec 0001, "Exit codes"), so the result does not depend on validation order.
- cobra and pflag silently keep the last value of a repeated option, so the `check` command counts occurrences itself (a custom `pflag.Value`, or by inspecting the parsed arguments) to report a repeated releash option (exit 2) or a repeated context option (exit 5).
- Errors from cobra's own parsing of `releash check` (unknown option, missing or extra arguments) exit 2 and print `false`. `run` knows which command ran from `ExecuteC`, so bare `releash` keeps its M1 behaviour.
- A panic during `releash check` prints `false` and exits 1.
- stderr lines start with `releash:`, as in spec 0001's fallback example. Exit-2 errors add one line pointing at `releash check --help`.
- The temporary `--loud` line is `releash: evaluation is not implemented yet (M4); returning false`. M4 removes it.
- `--current-time` accepts RFC 3339 (`time.RFC3339Nano`, which also accepts whole seconds). `--remote-address` accepts IPv4 and IPv6 addresses (`net/netip.ParseAddr`, without a zone).
- `--url` must be an absolute `http` or `https` URL with a host.
- The M3 scenario stubs in `main_integration_test.go` become real tests in this PR. They need no Unleash, so they move to ordinary unit tests unless they need the stack, and the stubs are removed.

## Steps

### PR 1: the check command

- [x] Branch `m3/check`.
- Contents: `internal/releash` gains `CheckOptions`, validation with exit codes, defaults, and `Check`, which prints `false` (and the `--loud` line) and returns the exit code; `main.go` gains the `check` subcommand with the two option groups in its help and usage, `UNLEASH_URL` and `UNLEASH_API_TOKEN` bound through the `releash_env` annotation, repeated-option detection, and the stdout contract for cobra errors and panics; unit tests for every exit code and behaviour in the milestone; the M3 stubs in `main_integration_test.go` turned into tests or removed; `README.md` usage; `docs/agents/internals.md` for the command and its validation; this plan.
- Checks: `task`, `prek run --all-files`, `task go:test-integration` with the stack up.
- Milestone criteria: all of them.
- Depends on: nothing.

### Close out

- [ ] Every M3 exit criterion holds. Set M3 to `done`, check off its scope, and update this plan's status.

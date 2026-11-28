# Plan: report an unknown flag without `--loud`

Started 2026-11-28. Status: implemented, in review. Tracks [#36](https://github.com/ivanklee86/releash/issues/36).

## Resume here

Updated 2026-11-28. Every step is done on branch `feat/report-unknown-flag`, and unit and integration tests pass against the local Unleash stack. What is left is review. The PR carries the `enhancement` label: spec 0002 counts additional stderr output as non-breaking.

## Goal

A flag that Unleash does not know, which is usually a typo or an archived or renamed flag, is reported on stderr by `check` and `variant` with or without `--loud`. Stdout, the exit code and every other fallback stay as they are.

## Decisions

- 2026-11-28, the user: ship option 1 of the issue only, always reporting not-found on stderr. Recorded in [ADR 0009](../../adrs/0009-report-unknown-flag-without-loud.md), which amends ADR 0003. `--strict` and the nearest-name suggestion stay open for later.
- 2026-11-28, taken as the consistent choice, for the user to confirm in review:
  - The line is the existing `--loud` line, unchanged: `releash: flag "<flag>" not found; returning false` (or `...; returning disabled variant`). With `--loud` it is written once.
  - Notes, such as the default-stickiness warning, stay under `--loud`. A not-found fallback has none anyway.
  - From review: when Unleash sends no flags at all, the line adds `(unleash returned no flags for this token's project and environment)`, because a token scoped to the wrong project or environment, or a proxy that has not loaded its flags, would otherwise read as a typo.

## Steps

1. `internal/releash/evaluate.go`: add `outcome.alwaysReported`, set only by `notFound`.
2. `internal/releash/check.go`: `reportOutcome` writes the fallback line when `--loud` is set or the outcome has `alwaysReported`; notes stay under `--loud`. Update the doc comments of `Check` and `Variant`.
3. Tests: the fallback tables in `evaluate_test.go` and `variant_test.go` expect the line without `--loud` only for the unknown flag. In `main_test.go`, valid cases, which check the unknown flag `x`, expect the not-found line either way, and `TestUnknownFlagIsReportedWithoutLoud` covers both commands. The integration scenarios for an unknown flag expect the same line with and without `--loud`; the other fallbacks in those tables stay silent without it.
4. Docs: ADR 0009, amend ADR 0003, spec 0001 (guide example, "Fallback to `false`", test plan, drawbacks), spec 0003 (fallback section, test plan), README ("Check a flag" and the exit code table), `docs/architecture.md` and `docs/agents/internals.md`.

## Verification

- `task` passes with 0 lint issues.
- `go test -tags integration -count=1 ./...` passes against `task services`.
- The fallback tests for an unreachable server, an unresponsive server, a rejected token, server errors and a malformed response still assert empty stderr without `--loud`, so the behaviour during an outage is unchanged.

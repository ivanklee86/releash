# Plan: one `main.go` in the module root

Started 2026-11-28. Status: implemented, not yet committed or merged.

## Resume here

Updated 2026-11-28. Steps 1 to 6 are done in the working tree: the test lists and the root package coverage (91.0%) match the baseline. What is left is the pull request, which the user ships.

## Goal

The module root holds one non-test Go file, `main.go`, and one unit test file, `main_test.go`. Today the root has `main.go` (472 lines) and `variant.go` (129 lines), with unit tests split over `main_test.go` (262), `check_test.go` (331) and `variant_test.go` (276).

This is a move only. No identifier is renamed, no behaviour changes, and no test is added, removed or rewritten.

## Decisions

- 2026-11-28, the user: move `variant.go` into `main.go`, and combine `check_test.go` with `main_test.go`.
- 2026-11-28, taken as the consistent choice, for the user to confirm in review:
  - `variant_test.go` also moves into `main_test.go`. With `variant.go` gone it would test a file that no longer exists, and leaving it would keep the split the change is removing.
  - `main_integration_test.go` stays a file of its own. Its `//go:build integration` tag applies to the whole file, so it cannot share a file with the unit tests.
  - Everything stays in package `main`. Moving the wiring to an internal package is a different change.

## Target layout

| File | Before | After |
|---|---|---|
| `main.go` | Root command, `check`, shared option code | The same, plus the `variant` command and its argument scan, about 600 lines |
| `variant.go` | `variantSynopsis`, `newVariantCommand`, `variantFallback`, `scanArgs` | Deleted |
| `main_test.go` | Root command, panics, version, `bindFlags` | Every root unit test, about 870 lines |
| `check_test.go`, `variant_test.go` | Command tests through `execute` | Deleted |
| `main_integration_test.go` | Pipeline scenarios | Unchanged apart from one comment |

Order inside `main.go`, so each command reads top to bottom:

1. Package comment, imports, `version`, the annotation constants.
2. `checkSynopsis` and `variantSynopsis` together.
3. `main`, `execute`, `exitCodeError`, `run`, `answerCommands`, `optionBeforeCommand`, `fallbackAnswer`.
4. `NewRootCommand`, `plainHelpAndVersion`, `plainValue`.
5. `newCheckCommand`, then the code both commands share: `addCheckFlags`, `countValues`, `checkArgs`, `usage`, `countedValue`, `mustAnnotate`.
6. `newVariantCommand`, `variantFallback`, `scanArgs`.
7. `resolveVersion`, `initializeConfig`, `bindFlags`.

Order inside `main_test.go`: `TestMain` and the shared helpers and constants first (`testURL`, `unleashEnv`, `buildInfo`, `noBuildInfo`, the fixture constants of both command files; `newEnvCommand` stays next to `TestBindFlags`, its only caller), then the root tests, the `check` tests and the `variant` tests, each group in its current order.

## Steps

One PR for the whole change, so the tree never has a half-moved command.

1. Record the baseline. Run `go test -list '.*' .` and `go test -tags integration -list '.*' .` and keep both lists, and note the root package's coverage from `go test -cover .`.
2. Move the contents of `variant.go` into `main.go` in the order above, merge the imports, and delete `variant.go`. Every doc comment moves with its declaration, unchanged.
3. Move the contents of `check_test.go` and `variant_test.go` into `main_test.go` in the order above, merge the imports, and delete both files. Keep each file's leading comment only where it still explains something, attached to the declaration it describes.
4. Fix the comments that name the removed files:
   - `main_integration_test.go`, lines 10 and 11: "the unit tests in check_test.go and variant_test.go" becomes `main_test.go`.
   - `internal/releash/variant_scenarios_integration_test.go`, line 12, also says "unit tests in variant_test.go", but it means `internal/releash/variant_test.go` in its own package. Leave it.
5. Update the docs:
   - `docs/agents/internals.md`: the `main.go`, `variant.go` row of the code layout table, the first sentence of "The variant command", and the `main_test.go`, `check_test.go` and `variant_test.go` rows of the test table, which become one row.
   - `docs/architecture.md`: the components diagram shows `main.go` and `variant.go` as two nodes. Make it one `main.go` node for the root, `check` and `variant` commands.
6. Verify, as in [Verification](#verification).

## Verification

- `task` passes (format, lint, build, unit tests).
- `prek run --all-files` passes.
- `task go:test-integration` passes against the local stack (`task services`). Never run `task services:down`, because the stack is shared.
- The two `go test -list` outputs match the baseline exactly, apart from order, so no test was lost in the move.
- Root package coverage is unchanged.
- `git diff --stat` shows only the files named in this plan, and `ls *.go` in the root prints `main.go`, `main_test.go` and `main_integration_test.go`.

## Risks

- A test helper or constant defined in more than one test file would already fail to compile, since all three files are in package `main`, so merging cannot introduce a name clash.
- `go install github.com/ivanklee86/releash@<version>` and the goreleaser build both use the package, not a file name, so neither is affected.
- The README, specs and ADRs do not name the moved files. Historical plans do, and stay as written.

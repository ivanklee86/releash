# Plan: leave command-line parsing to cobra

Started 2026-11-28. Status: implemented, in review. Tracks [#35](https://github.com/ivanklee86/releash/issues/35).

## Resume here

Updated 2026-11-28. Every step is done on branch `cli/persistent-options`, and unit and integration tests pass against the local Unleash stack. What is left is review. The PR needs the `breaking-change` label, and the next release is v0.3.0.

## Goal

Delete the two hand-written copies of pflag's parsing rules in `main.go`: `optionBeforeCommand`, which rejected an option before `check` or `variant`, and `scanArgs`, which re-read the raw arguments to find the format and prefix of `variant`'s fallback when cobra could not parse the command line. Existing exit codes keep their meaning.

## Decisions

- 2026-11-28, the user: take options 1 and 3 from the issue, plus a lookup by command name for errors cobra blames on the root command. Recorded in [ADR 0008](../../adrs/0008-leave-command-line-parsing-to-cobra.md).
  - Option 1: the shared options become persistent flags of the root command, so they may go before the command name.
  - Option 3: `variant` prints nothing on exit 2. This is a breaking change to output, so it ships in v0.3.0.
  - `releash --nope check x` and `releash --token check x` (empty `$TOKEN`) fail on the root command. `run` prints the fallback of the first argument that names an answer command, so `check` still prints `false`.
- 2026-11-28, taken as the consistent choice, for the user to confirm in review:
  - Option 3 covers every exit 2 of `variant`, including the ones validation finds (`--format` twice, an invalid `--timeout`), not only cobra's. Otherwise stdout on exit 2 would depend on who found the problem.
  - The shared options are hidden from the root command's help and the `help` command's usage, so the root command's help still matches the M1 contract.
  - A recovered panic in `variant` still prints the fallback. It now reads `--format` and `--prefix` from the flag set cobra already parsed, instead of from the raw arguments.
  - A shared option given to the bare root command, as in `releash --loud` or `releash --timeout abc`, exits 2 with `--<option> needs a command`, as an unknown option did before. The check runs before `bindFlags`, so `--url` and `--token` from the environment do not trip it.
  - `releash foo check` prints `false`: any argument named `check` picks the fallback, even after a mistyped command. Exit 2 is right either way, and a narrower rule would have to know which options take values.
  - When cobra reports an unknown command and the argument before `check` or `variant` is an option, stderr adds `releash: <option> may be missing its value and have taken "<command>" instead`.
  - No fuzz test, because no hand-written scanning is left. `answerCommandIn` compares whole arguments with command names and does not interpret options.

## Steps

1. `main.go`: move `addCheckFlags` onto the root command's persistent flags, owned by `NewRootCommand` and passed to `newCheckCommand` and `newVariantCommand`. Hide the persistent flags, and have `usage` unhide them and set the per-command `--loud` help text. Delete `optionBeforeCommand`, `answerCommands`, `variantFallback` and `scanArgs`. Add `answerCommandIn`, and give `fallbackAnswer` the exit code instead of the arguments.
2. `internal/releash/variant.go`: `Variant` prints nothing on exit 2 and `variant.Fallback` in the validated format and prefix on exits 3 to 5. `VariantFallback` stays for the panic path.
3. Tests: in `main_test.go`, options before each command now run, root-command errors print `false` or nothing, the scan cases become "exit 2 prints nothing", and root help hides the shared options. Update `internal/releash/variant_test.go` and the integration scenario for a disabled flag with a command line error.
4. Docs: ADR 0008, amend ADR 0004, spec 0001 (option placement, the exit 2 row), spec 0003 (option placement, "Output and exit codes", test plan, versioning, rationale), README ("Options" and "Exit codes"), `docs/architecture.md` and `docs/agents/internals.md`.

## Verification

- `task` (format, lint, build, unit tests) passes with 0 lint issues.
- `go test -tags integration -count=1 ./...` passes against `task services`.
- With a built binary: `releash --loud check x` and `releash --format shell variant x` run; `releash --nope check x` and `releash --token check x` exit 2 and print `false`; `releash variant x --bogus --format shell` and `releash variant x --format shell --format shell` exit 2 with empty stdout; `releash variant x --current-time nope --format shell` exits 4 and prints the fallback as `export` lines.

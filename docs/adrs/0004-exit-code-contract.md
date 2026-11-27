---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# Always print a boolean and use exit codes for command and configuration errors

## Context and Problem Statement

`releash check` has to report three kinds of outcome: an answer (`true` or `false`), a runtime fallback (see [ADR 0003](0003-fail-safe-false.md)), and a mistake in how it was called, such as a missing token or an invalid context value. How should stdout and the exit code express these?

## Decision Drivers

* A gated step must stay off in every shell form, including ones that discard the exit code, e.g. `if [ "$(releash check x)" = "true" ]` and `echo "go=$(releash check x)" >> "$GITHUB_OUTPUT"`.
* A `false` answer must not abort scripts that use `set -e`.
* Mistakes should fail the first run of a pipeline that checks the exit code.
* Pipelines should be able to tell a missing secret apart from a broken command.

## Considered Options

* Exit code as the answer: 0 for `true`, 1 for `false`.
* Always exit 0; report mistakes only on stderr.
* Print nothing on stdout and exit non-zero for mistakes.
* Always print `true` or `false`; exit 0 for answers and a distinct non-zero code for each kind of mistake.

## Decision Outcome

Chosen option: "Always print `true` or `false`; exit 0 for answers and a distinct non-zero code for each kind of mistake", because stdout is always a usable boolean, `false` answers are safe under `set -e`, and mistakes are visible to any script that checks the exit code.

| Exit | Meaning |
|---|---|
| 0 | Answer, including runtime fallbacks |
| 1 | Unexpected internal error |
| 2 | Bad command line |
| 3 | No Unleash URL or API token (missing or empty) |
| 4 | Invalid context value, property name or Unleash URL |
| 5 | Duplicate context field or property |
| 6 | `releash variant` only: the flag is disabled, so the pipeline skipped its `releash check` gate ([ADR 0007](0007-refuse-variant-of-disabled-flag.md)) |

Codes 1 and 2 follow common CLI convention. For `releash variant`, "print a boolean" means "print the disabled variant in the requested format" ([spec 0003](../specs/0003-variant-variables.md)), and the same codes apply. Exit codes are part of the public contract: new codes may be added, but existing codes keep their meaning.

Use standard exit codes where possible. A new kind of error reuses an existing code only when it fits that code's documented meaning above exactly, e.g. a new kind of bad command line uses 2. Exit 1 stays reserved for internal errors and is never reused for user mistakes. Otherwise a new kind of error gets the next free code above 5. Codes reserved by shells and common tools are never used for releash errors: 126 and 127 (command not executable or not found), 128 and above (killed by a signal), and 124 (timed out, as used by `timeout(1)`). The BSD `sysexits.h` codes (64–78) were considered and not adopted, because few CI tools recognise them and a small range of low codes is easier to remember.

### Consequences

* Good, because downstream consumers of the output never see an empty value.
* Good, because a pipeline can, for example, treat exit 3 on fork pull requests differently from other errors.
* Bad, because the same mistake fails a pipeline that assigns the output under `set -e` but only skips the step in forms that discard the exit code. In those forms, the stderr message is the only signal.
* Bad, because pipelines where secrets are intentionally absent, such as fork pull requests, must guard the call if they assign the output under `set -e`.
* Neutral, because an unrecovered Go panic exits 2. releash recovers panics on its own goroutine and exits 1, but a panic on an SDK goroutine would still exit 2 with empty stdout. The "always a boolean" guarantee therefore covers every exit releash controls, not crashes it cannot recover from. `--help` and `--version` are also exempt, since they print information instead of an answer.

### Confirmation

Tests assert stdout, stderr and the exit code for every row of the exit code tables in spec 0001 and spec 0003.

## Pros and Cons of the Options

### Exit code as the answer

* Good, because `releash check x && ./deploy.sh` works.
* Bad, because `false` aborts scripts under `set -e`, and an error looks the same as `false`.

### Always exit 0

* Good, because releash can never abort a script.
* Bad, because a missing token or a typo in the command is only visible with `--loud` and can go unnoticed indefinitely.

### Print nothing and exit non-zero for mistakes

* Good, because mistakes are distinct from answers on stdout.
* Bad, because consumers such as `$GITHUB_OUTPUT` get an empty value instead of a boolean.

### Always print a boolean, distinct exit codes for mistakes

* Good, because it satisfies every decision driver.
* Bad, because the exit code is lost in common shell forms, so the guarantee there is only that the step stays off.

## More Information

See [spec 0001](../specs/0001-rollout-decisions.md), "Exit codes", and [spec 0003](../specs/0003-variant-variables.md), "Output and exit codes". An opt-in `--exit-code` option that makes `releash check x && ./deploy.sh` work is listed in spec 0001 under future possibilities.

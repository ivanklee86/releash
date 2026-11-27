---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# Refuse to load the variant of a disabled flag with exit 6

## Context and Problem Statement

Pipelines are meant to gate with `releash check` and then load configuration with `releash variant` (see [spec 0003](../specs/0003-variant-variables.md)). Unleash gives a disabled flag the variant `disabled`, so `variant` could simply print that and exit 0. But a pipeline that calls `variant` on a disabled flag has skipped its gate, and would then run the gated step with default settings. What should `variant` do when it evaluates the flag and finds it disabled?

## Decision Drivers

* A missing gate should fail the first run, not look like success.
* An Unleash outage must not fail pipelines ([ADR 0003](0003-fail-safe-false.md)).
* Consumers of stdout, such as `$GITHUB_ENV`, should still get a usable value ([ADR 0004](0004-exit-code-contract.md)).

## Considered Options

* Print the disabled variant and exit 6, with an error on stderr naming `releash check`.
* Print the disabled variant and exit 0, with a hint under `--loud`.
* Print the disabled variant and reuse an existing non-zero code.

## Decision Outcome

Chosen option: "Print the disabled variant and exit 6", because it makes a missing gate fail loudly while stdout stays the safe fallback.

* Exit 6 applies only to `variant`, and only when the flag was evaluated and is disabled for the context. `check` never exits 6.
* Runtime fallbacks (Unleash unreachable, token rejected, unknown flag, unusable payload) still exit 0.
* The SDK returns the same `disabled` variant for an unknown flag and a disabled one, so releash tells them apart from the flags it fetched: only a flag that is in them and disabled for the context exits 6.
* An enabled flag with no variants is an answer and exits 0 with `VARIANT=disabled`.
* Exit 6 ranks after 1–5: when the command line is also wrong, the lower code wins and the flag is never evaluated.
* The error goes to stderr with or without `--loud`, like codes 1–5.

### Consequences

* Good, because forgetting `check` fails the step under `set -e` the first time the flag is off, as long as the step sees releash's exit code. `eval "$(releash variant ...)"` does not, so spec 0003's shell example captures the output before `eval`.
* Good, because stdout is the disabled variant, so steps that ignore the exit code still fall back to their defaults.
* Bad, because spec 0001's rule that an evaluated flag always exits 0 no longer holds for `variant`.
* Bad, because a flag turned off between `check` and `variant` fails the step. This is rare and visible.
* Bad, because pipelines must pass the same context to `check` and `variant`, or `check` can say `true` while `variant` exits 6.

### Confirmation

Tests check that a disabled flag prints the disabled variant in each format, writes the error to stderr without `--loud` and exits 6, and that a disabled flag combined with a command line error exits with the lower code.

## Pros and Cons of the Options

### Disabled variant and exit 6

* Good, because it satisfies every decision driver.
* Bad, because it adds a code that only one command uses.

### Disabled variant and exit 0

* Good, because it matches `check`, where `false` is an answer.
* Bad, because a pipeline without a gate runs the gated step with defaults, and nothing reports it without `--loud`.

### Reuse an existing code

* Good, because no new code is needed.
* Bad, because no existing code fits: 2 to 5 mean the command or its configuration is wrong, and 1 is reserved for internal errors ([ADR 0004](0004-exit-code-contract.md)).

## More Information

See [spec 0003](../specs/0003-variant-variables.md), "When there is no variant" and "Output and exit codes".

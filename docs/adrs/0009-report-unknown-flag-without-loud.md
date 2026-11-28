---
status: accepted
date: 2026-11-28
decision-makers: Ivan Lee
---

# Report a flag that Unleash does not know on stderr without `--loud`

## Context and Problem Statement

When Unleash answers but does not know the flag, `check` prints `false` and `variant` prints the disabled variant, and both exit 0 ([ADR 0003](0003-fail-safe-false.md)). The reason, `flag "deploy-pament" not found`, reaches stderr only with `--loud`. So a typo in a flag name, or a flag that was archived or renamed, looks exactly like a flag that is switched off: the gated step is skipped on every run, and nothing in the pipeline log says why ([#36](https://github.com/ivanklee86/releash/issues/36)).

This differs from an outage. When Unleash is down, failing safe and staying quiet is the point. Here Unleash was reachable and gave a definite answer: no flag of that name exists for the token's project and environment. How should releash make that visible?

## Decision Drivers

* An unknown flag is almost always a mistake in the pipeline, and should show up in the log of the first run.
* Stdout and the exit code must not change: ADR 0003 and existing pipelines rely on them.
* An outage, a rejected token, a timeout and a malformed response stay quiet without `--loud`.

## Considered Options

* Always write the not-found line to stderr, with or without `--loud`.
* Add an opt-in option, such as `--strict`, that exits non-zero with a new exit code when the flag is unknown.
* Add the nearest known flag name to the message, e.g. `did you mean "deploy-payments"?`.

## Decision Outcome

Chosen option: "Always write the not-found line to stderr", because it makes the mistake visible in every pipeline log without changing stdout, the exit code or any other fallback.

* Both commands write the line they already wrote under `--loud`: `releash: flag "<flag>" not found; returning false` for `check`, and `...; returning disabled variant` for `variant`. With `--loud` it is written once, as before.
* Only the not-found case changes. It applies only when the first fetch succeeded and the flag is not in the fetched flags. Every other fallback is still written only under `--loud`.
* Stdout and the exit code are unchanged: `check` prints `false` and `variant` the disabled variant, and both exit 0.
* The other two options are not ruled out. They can be added later without changing this decision: `--strict` as a new option with a new exit code (ADR 0004), and the suggestion as extra text on the same line.

### Consequences

* Good, because a typo shows up in the log of the first run, under `set -e` or not.
* Good, because spec 0002 counts additional stderr output as non-breaking, so this ships in a minor release under "Features".
* Bad, because a pipeline that checks a flag before creating it in Unleash, on purpose, now gets a stderr line on every run. It can redirect stderr for that call.
* Bad, because a pipeline that treats any stderr output as failure, which is rare in CI, now fails for an unknown flag. That is the outcome this change wants, but it arrives without an exit code.
* Neutral, because a flag in a project the token cannot see is reported the same way. Unleash does not tell the two apart, and both are mistakes in the pipeline's configuration. When Unleash sends no flags at all, which points at the token's project or environment, or at a proxy that has not loaded its flags yet, rather than at a typo, the line adds `(unleash returned no flags for this token's project and environment)`.

### Confirmation

Unit tests in `internal/releash` and `main_test.go` check that both commands write the not-found line without `--loud`, with unchanged stdout and exit code, and that every other fallback is still silent without `--loud`. Integration scenarios against the compose stack check the same for a flag that does not exist.

## Pros and Cons of the Options

### Always report not-found on stderr

* Good, because it is the smallest change and keeps stdout and the exit code.
* Bad, because a pipeline that ignores stderr still skips the step.

### `--strict` with a new exit code

* Good, because the step fails instead of being skipped.
* Bad, because it only helps pipelines that opt in, which are the ones least likely to have the typo.
* Bad, because it adds an exit code that only applies under an option.

### Suggest the nearest name

* Good, because it often shows the fix as well as the problem.
* Bad, because it needs an edit-distance rule and a threshold, and on its own it is still only visible under `--loud`.

## More Information

This amends [ADR 0003](0003-fail-safe-false.md), which said every fallback is silent without `--loud`. See [spec 0001](../specs/0001-rollout-decisions.md), "Fallback to `false`", and [spec 0003](../specs/0003-variant-variables.md), "Fallback to the disabled variant".

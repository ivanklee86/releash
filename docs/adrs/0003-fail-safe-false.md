---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# Fall back to `false` when releash cannot get an answer

## Context and Problem Statement

At check time, releash may be unable to evaluate the flag: Unleash is down or slow, the token is rejected, the flag does not exist, or the response is malformed. What should `releash check` output, and should it fail the pipeline?

## Decision Drivers

* A problem in releash or Unleash must never turn a gate on.
* An Unleash outage should not block every pipeline that is gated on any flag.
* Problems should still be discoverable without changing the output contract.

## Considered Options

* Print `false` and exit 0, with the reason on stderr under `--loud`.
* Fail the command with a non-zero exit code.
* Print `true` (fail open).

## Decision Outcome

Chosen option: "Print `false` and exit 0", because skipping a gated step is safer than running one by mistake, and pipelines keep running while Unleash is unavailable.

The fallback covers runtime conditions only. Mistakes in the command or its configuration, such as a missing token, still print `false` but exit non-zero (see [ADR 0004](0004-exit-code-contract.md)).

### Consequences

* Good, because no runtime failure can turn a gate on.
* Good, because `set -euo pipefail` scripts keep running during an Unleash outage.
* Bad, because an expired token silently skips the gated step until someone runs with `--loud`. A misspelled flag name did too, until [ADR 0009](0009-report-unknown-flag-without-loud.md) made releash report an unknown flag without `--loud`.
* Bad, because releash must not evaluate against anything except a fresh response from Unleash. The SDK's default persistent backup file is disabled, since a stale backup evaluated `true` with a bad token in the 2026-11-27 spike.

### Confirmation

Tests cover each fallback (unreachable server, unresponsive server, HTTP 401/403, unknown flag, malformed response): each prints `false`, exits 0, stays silent without `--loud` (apart from the unknown flag, see ADR 0009), and finishes within `--timeout`. A test also checks that after a successful check, a failing check still prints `false` and no files are written.

## Pros and Cons of the Options

### Print `false` and exit 0

* Good, because it is safe in every shell form a pipeline might use.
* Bad, because it hides problems by default.

### Fail with a non-zero exit code

* Good, because outages and misconfiguration are visible right away.
* Bad, because an Unleash outage blocks every gated pipeline, including ones whose gates would have been off anyway.
* Bad, because a non-zero exit is easy to confuse with a `false` answer in shell scripts.

### Print `true` (fail open)

* Good, because pipelines behave as if gates were not there during an outage.
* Bad, because a releash or Unleash problem could deploy something that was deliberately held back. This defeats the purpose of a gate.

## More Information

See [spec 0001](../specs/0001-rollout-decisions.md), "Fallback to `false`". Amended by [ADR 0009](0009-report-unknown-flag-without-loud.md) for a flag that Unleash does not know.

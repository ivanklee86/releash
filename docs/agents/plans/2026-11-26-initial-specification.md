# Plan: initial specification

Started 2026-11-26. Status: done.

## Resume here

Done 2026-11-27. Specs 0001 and 0002 are `accepted`, ADRs 0001–0005 are recorded, and milestones M1–M5 are reviewed. Implementation continues in [the M1 plan](2026-11-27-m1-skeleton-mvp.md).

## Goal

Write the first specification(s) for releash, enough to define a first milestone and start implementation.

## What we know

- releash is a small CLI that lets CD pipelines drive releases from Unleash feature flags.
- People change flags in the Unleash UI. releash only reads flag state and turns it into go/no-go decisions for pipeline steps. It never writes to Unleash.
- It talks to Unleash through the Client API with a backend token, using the official Go SDK. Usage metrics are reported and flushed before exit.
- It ships as a single static Go binary that any CI system can call. It is not tied to GitHub Actions or Kubernetes.
- The core decision is a boolean gate. `releash check <flag>` prints `true` or `false` on stdout for use in shell scripts, and exits 0 for both.
- Failure is fail-safe: if releash cannot get an answer (Unleash unreachable, bad token, unknown flag), it prints `false`. `--loud` prints the details of why to stderr.
- The evaluation context is built from one option per standard Unleash context field plus a repeatable `--properties name=value` for custom properties (superseding the original `--context` dot-notation design; see below). Required fields such as `appName` get sensible defaults.
- `--loud` only adds stderr output and never changes the exit code.
- Decided 2026-11-27: stdout of `releash check` is always `true` or `false`. Exit 0 means the value is an answer (evaluated or a runtime fallback). A non-zero exit means the command or its configuration is wrong, and stdout is `false`. The codes are listed below and in ADR 0004.

## Decisions from 2026-11-27

- Missing URL or token prints `false` and exits 3, a code separate from usage errors (2). Fork-PR pipelines that assign the output under `set -e` must guard the call themselves.
- No config file in v1. Options and environment variables only.
- `--env` defaults to `production`. Defaults never count as duplicates.
- The generic `--context key=value` option with dot notation is replaced by one option per standard context field (`--env`, `--app-name`, `--user-id`, `--session-id`, `--remote-address`, `--current-time`) and a repeatable `--properties name=value`. A property named after a standard field exits 4, because the SDK would ignore it.
- `currentTime` defaults to the time of the check (the SDK already does this). `remoteAddress` defaults to the runner's local IP for its connection to Unleash, with no external lookup.
- `--timeout` defaults to `5s` and bounds the whole run: register, first fetch and metrics flush.
- Each kind of error gets its own exit code, and stdout is still `false`: 0 answer (including runtime fallbacks), 1 internal error, 2 bad command line, 3 missing URL/token, 4 invalid context value or property name, 5 duplicate context field or property. Recorded in ADR 0004.

## SDK spike (2026-11-27)

A throwaway probe against Unleash 8.3.1 in Docker, using the Go SDK `github.com/Unleash/unleash-go-sdk/v5` v5.1.0 on Go 1.27.4. Local network, so real round trips will add latency.

| Phase | Time |
|---|---|
| `NewClient` (includes a synchronous `POST /client/register`) | 5–25 ms |
| First feature fetch until ready | 0–2 ms |
| `IsEnabled` | <0.1 ms |
| Metrics flush (waits for the next metrics tick; interval set to 50 ms) | ~50 ms |
| Whole process, wall clock | 50–80 ms |

Findings that change the spec or the implementation:

- `Close()` does not flush metrics. Metrics are only sent on the metrics ticker, so releash has to set a short metrics interval and wait for the SDK's `OnSent` event, bounded by the timeout.
- The SDK persists flags to a backup file in the temp directory and loads it when the server cannot be reached. With a stale backup, a bad token and an unreachable server both evaluated to `true`. releash must use non-persistent storage and only evaluate after the SDK reports ready. Otherwise it falls back to `false`.
- `NewClient` sends the register request synchronously with `http.DefaultClient`, which has no timeout. Against an unroutable address it hung indefinitely. releash must pass an HTTP client with a timeout and enforce the overall budget itself.
- Bad token (401) and connection refused fail fast (<5 ms) and are reported on the SDK's error channel, so releash can fall back right away instead of waiting out the timeout.
- An unknown flag evaluates to `false` with no error from the SDK. releash has to check the flag list itself to report "not found" under `--loud`.

## Open questions

None. Missing URL/token behavior, the SDK metrics flush and the config file question are all resolved above.

## Proposed documents

- `docs/specs/0001-rollout-decisions.md`: the core spec covering the model, commands, evaluation context, output contract and failure behavior.
- ADRs in `docs/adrs` for decisions with real trade-offs: read-only by design, Client vs Frontend API, failure defaults.
- `docs/agents/milestones/M1-<topic>.md`: first milestone, scoped to spec 0001 once it is accepted.

## Steps

- [x] Resolve the core model.
- [x] Draft spec 0001.
- [x] Record ADRs for the decisions made.
- [x] Resolve the spec's unresolved questions.
- [x] Review spec 0001 and move it to `accepted`.
- [x] Draft milestones M1–M5.
- [x] Review milestones with the user (2026-11-27: added M2 dev tooling and testing, dropped targets).
- [x] Draft spec 0002 (distribution) and ADR 0005.
- [x] Review spec 0002 and move it to `accepted`.

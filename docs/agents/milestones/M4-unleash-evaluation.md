---
status: active  # planned | active | done | dropped
---

# M4: Unleash evaluation

## Goal

`releash check` evaluates real flags against Unleash, falls back to `false` safely when it cannot, and reports usage metrics. Spec 0001 is fully implemented.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- [ ] [0001-rollout-decisions](../../specs/0001-rollout-decisions.md), remaining sections: "Fallback to `false`", "Automatic values", "Unleash integration", and the full test plan.

## Exit criteria

- releash uses `github.com/Unleash/unleash-go-sdk/v6` at a pinned version (v6.5.1) through the Client API.
- An enabled flag prints `true`, and a disabled flag prints `false`. Both exit 0 with nothing on stderr.
- A constraint on a custom property (e.g. `region`) gives different results for `--properties region=eu` and `--properties region=us`, and each standard context option reaches evaluation.
- Without `--current-time`, date constraints compare against the time of the check. Without `--remote-address`, an IP strategy listing the runner's address enables the flag, and `--remote-address` overrides it.
- Each fallback prints `false`, exits 0, is silent without `--loud`, and finishes within `--timeout`: unreachable server, unresponsive server, HTTP 401/403, unknown flag, malformed response.
- With `--loud`, each fallback reason has its own stderr line, and stdout and the exit code are the same as without it.
- After a successful check, a check with a bad token or an unreachable server prints `false`, and releash writes no files.
- A successful check sends usage metrics for the flag before exit. A failing or hanging metrics endpoint does not change stdout or the exit code, and does not delay exit past `--timeout`.
- Integration tests cover every criterion above, using the Unleash harness from M2, locally and in CI.
- The temporary "evaluation not implemented" message from M3 is gone.
- Spec 0001 moves to `implemented`.

## Out of scope

- Variants, payloads, multi-flag checks and `--exit-code` (future possibilities in spec 0001).
- Releases (M5).

## Risks / open questions

- The SDK's service-oriented defaults (backup file, HTTP client without a timeout, metrics only sent on a timer) need workarounds that may break on SDK upgrades. The integration tests are the guard.
- Detecting the local address of the Unleash connection depends on hooking the SDK's HTTP client. This needs a short spike to confirm it works through the SDK.

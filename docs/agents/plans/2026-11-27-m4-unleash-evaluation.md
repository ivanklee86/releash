# Plan: M4 Unleash evaluation

Started 2026-11-27. Status: in progress. Milestone: [M4](../milestones/M4-unleash-evaluation.md).

## Resume here

Updated 2026-11-27. A spike on the v6 SDK ran first (findings below). PR 1 (evaluation) is merged. PR 2 (usage metrics) is on `m4/metrics`, rebased onto `main` with the PR 1 review fixes as its first commit. PR 3 (automatic `remoteAddress`) is on `m4/remote-address`, stacked on `m4/metrics`, with its review findings fixed; once PR 2 merges it is rebased onto `main`. Next: the user runs `/ship` for PR 2, then PR 3, then the close-out. When resuming, start the first PR in [Steps](#steps) that is not checked off and whose dependencies are merged. Before ending a session, update this section and check off finished steps.

Decisions made while building PR 1, within the ones below:

- Evaluation lives in `internal/releash/evaluate.go`. `Check` calls it only for a valid invocation; `Releash` has an unexported `evaluate` field that the validation unit tests replace with a stub, so they need no server.
- One run context with the `--timeout` deadline bounds everything. A `boundedTransport` cancels every SDK request when it ends, and the `http.Client` also has `Timeout` set to `--timeout`, as spec 0001 asks. The transport also records the outcome (status or transport error) of the first `/client/features` and `/client/register` request, and the fallback and register lines are built from those records rather than from the SDK's error text.
- The `--loud` lines are `releash: unleash unreachable: <error>; returning false`, `releash: unleash did not answer within <timeout>; returning false`, `releash: unleash rejected the API token (HTTP 401|403); returning false`, `releash: unleash answered the feature fetch with HTTP <status>; returning false`, `releash: unleash sent a malformed feature response: <error>; returning false` and `releash: flag "<flag>" not found; returning false`. A failed register request adds `releash: registering with unleash failed: <HTTP status or error>; the answer is unaffected`, only when the feature fetch succeeded, because otherwise the register request failed for the same reason and the fallback line already says so.
- Metrics stay enabled with the SDK's default interval, because the SDK sends the register request only when metrics are enabled, and spec 0001 has releash register under its `appName`. With the default 60s interval no periodic send happens within a normal run; `Close` makes the SDK's one shutdown flush, bounded by the run context. On a fallback, releash cancels the run context before `Close`, so nothing waits on the flush. PR 2 owns the rest.
- In PR 1, `remoteAddress` stayed unset without `--remote-address`. PR 3 supersedes this: it detects the local address of the connection to Unleash.
- Without `--current-time`, `currentTime` is left empty, and the SDK uses the time of evaluation, which is within `--timeout` of the start of the check.
- Unleash rejects an empty value in a constraint, and the SDK reads an empty property the same as an unset one, so the scenario for `--properties build=` can only check that an empty value is accepted and leaves a `NOT_IN` constraint passing; the unit tests check that the empty value reaches evaluation.

Known limits found in PR 1:

- The SDK delivers listener callbacks only after `NewClient` returns, and `NewClient` waits for the register request. A hanging register used to make the check time out even when the features were served. After review, the register request has its own deadline (a quarter of `--timeout`, at most one second), so the answer is unaffected and a rejected fetch falls back once that deadline passes at the latest; spec 0001 records this.
- Spec 0001 said the SDK "does not flush them on `Close`". That was true of v5; v6.5.1 does flush once on `Close` (finding 6). The bullet now describes the v6 flush and leaves how the send is made reliable to PR 2.
- Review also found that a first fetch answered with 304 left the SDK ready with no flags (reported as "not found"), that a flag the SDK fails to evaluate gave `false` with no `--loud` line, and that a redirected fetch reported the redirect status. Each now has its own `--loud` line or reports the last hop.

## How to work through this plan

- One PR per step. Read this plan, the M4 milestone, spec 0001 and `AGENTS.md` first.
- Before handing off, run `task`, `prek run --all-files`, and `task go:test-integration` against the stack (`task services`).
- PRs 2 and 3 are independent of each other and may be built in parallel worktrees, both stacked on PR 1. Both touch the SDK client setup, so whichever merges second is rebased onto `main` first. They share the one compose stack: neither runs `task services:down`.
- The user runs `/ship`. Claude never merges.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing.

## Reference

Decisions:

- The SDK is `github.com/Unleash/unleash-go-sdk/v6` at v6.5.1, not v5 as spec 0001 and the milestone were first written. The user chose the maintained major on 2026-11-27. PR 1 rewords both.
- The spike (below) confirms or corrects each SDK workaround spec 0001 describes before PR 1 relies on it.
- The integration scenarios that need a flag the fixtures cannot hold (date constraints a few minutes either side of now, constraints on `environment` and `appName`) create it through the Admin API with `integration.AdminToken`, name it with `integration.ScenarioFlagPrefix`, and archive it in `t.Cleanup`.

### Spike findings

The spike ran the v6.5.1 SDK against the local stack and fake servers on 2026-11-27.

1. In-memory storage only works. `unleash.WithStorage` with a `Storage` whose `Init` and `Persist` do nothing and whose `Load` returns `(nil, nil)` leaves the temp directory empty; the fetcher treats a nil state as "start empty". Without it, `NewClient` falls back to `DefaultStorage`, writes `<TempDir>/unleash-repo-schema-v1-<appName>.json`, and loads it before the first fetch: a planted stale backup with a flag enabled made that flag `true` against a refused port. `WithBackupPath` does not stop the file being written, and `BootstrapStorage` wraps `DefaultStorage`, so neither is an alternative.
2. `unleash.WithHttpClient` is used for the feature fetch, the register request and metrics, including the flush in `Close`. Without it the SDK uses `http.DefaultClient`, which has no timeout. The register request runs synchronously inside `NewClient`, so against an unresponsive server `NewClient` blocks for the client's timeout, and `Close` blocks for another one on the metrics flush. `Client.Timeout` bounds each request on its own; a `RoundTripper` that cancels every request when a shared run context ends brought register, first fetch and `Close` together within the run budget. When the run context cancels the feature fetch, the SDK drops the error (`*url.Error` with `context.Canceled`), so releash's own timer must decide that the timeout was reached.
3. Readiness and failures come through a listener (`unleash.WithListener`): `OnReady` after the first successful fetch, `OnError` for a failed fetch. Do not use `Client.Ready()`, which the SDK's own goroutine also reads, or `WaitForReady()`, which cannot be cancelled. Every callback runs on the SDK's single sync goroutine, so none may block. `OnError` also receives transport errors of the register and metrics requests, as a `*url.Error` naming their path, and sometimes before the fetch error; HTTP status errors of register and metrics go to `OnWarning`. Decode errors carry no URL. After a 401, 403 or 404 the fetcher backs off for 10 refresh intervals, so failing on the first error is right. No error type is exported, so classifying by message text would break on SDK upgrades; a `RoundTripper` that records the outcome of the feature request is more robust.
4. The SDK evaluates an unknown flag as `false` and reports nothing. `FeatureOptions.FallbackFunc` is called exactly when the fetched flags do not include the flag, so it is an exact signal; `ListFeatures` also works. `GetVariant` cannot tell an unknown flag from a disabled one. Check only after `OnReady`, when the snapshot is filled.
5. `IsEnabled(name, FeatureOptions{Ctx: ...})` merges the given context over the static one (`WithEnvironment`, default `default`, and `WithAppName`): a non-empty field wins, and a non-nil `Properties` replaces the map. `CurrentTime` is an RFC 3339 string; when it is empty, date constraints use `time.Now()`. A `CurrentTime` that does not parse makes the constraint false silently, so releash's own validation is still needed. `Field` reads standard field names before properties, which confirms rejecting properties named after standard fields. Every standard field and a custom property reached evaluation.
6. Metrics: `WithMetricsInterval` sets the periodic send, a 2xx answer fires `MetricListener.OnSent`, a non-2xx answer goes to `OnWarning` and keeps the bucket, and an interval of 0 or `WithDisableMetrics(true)` disables metrics, and with them the register request. Unlike v5, `Close` in v6.5.1 makes one best-effort flush of buffered counts, bounded by its own 5s timeout and by the HTTP client; it reports nothing to the listener. The flushed counts showed up in the Admin API's raw client metrics for the flag.

## Steps

### PR 1: evaluation

- [x] Branch `m4/evaluate`. Merged.
- Contents: the M3 close-out (M3 to `done`, its scope ticked, the M3 plan's status); this plan; spec 0001 and the M4 milestone reworded for v6; the SDK in `go.mod`; `internal/releash` evaluates the flag through the SDK with in-memory storage only, an HTTP client bounded by `--timeout`, evaluation only after the first successful fetch, an immediate fallback when that fetch fails, unknown-flag detection, and one `--loud` line per fallback reason; register errors only under `--loud`; the temporary M3 `--loud` line removed; every context option reaching evaluation; the M4 integration scenarios except metrics and automatic `remoteAddress` filled in; `docs/agents/internals.md`.
- Milestone criteria: SDK, true/false, context, `--current-time` default, fallbacks, `--loud`, no stale state, M3 message gone.

### PR 2: usage metrics

- [x] Branch `m4/metrics`, stacked on `m4/evaluate`.
- Contents: a short metrics interval and a wait for the SDK to confirm the send before exit, bounded by `--timeout`; a failed or hanging metrics endpoint never changes stdout or the exit code, and is reported with `--loud`; integration tests for each, using a fake metrics endpoint where the stack cannot fail on demand.
- Milestone criteria: metrics.
- Decisions made while building it:
  - The metrics interval is 100ms. After an evaluated answer, `awaitMetrics` (`internal/releash/metrics.go`) waits for the SDK's `OnSent`, a failed metrics request, or the end of the run context. The change to the SDK client setup is one `unleash.WithMetricsInterval` option; the rest is in the listener, the request log (which now also records `/client/metrics`) and `metrics.go`.
  - A failed send is told apart by the transport's record, not the SDK's warning text: `OnWarning` and metrics transport errors in `OnError` only wake the wait, which then reads `requestLog`.
  - The new `--loud` lines are `releash: sending usage metrics to unleash failed: <HTTP status or error>; the answer is unaffected` and `releash: sending usage metrics to unleash did not finish within <timeout>; the answer is unaffected`, after any register line.
  - The wait has no deadline of its own below `--timeout`, as the milestone asks: a hanging metrics endpoint holds a successful check until `--timeout`. A fallback does not wait, including one for a flag the SDK reports it cannot evaluate: that error can arrive during the wait, which then ends at once and drops any metrics note. The run context is always cancelled before `Close`, so the SDK's shutdown flush cannot retry a failed send with the rest of the budget. The cost is that the counts are sent only on a tick: with a `--timeout` near or below 100ms, or one that the fetch and register mostly used up, a healthy Unleash gets no usage from the check and `--loud` reports that the send did not finish.
  - Unleash 8.3.1 drops usage of a flag created a few seconds after another metrics request (it filters by a cached list of flag names), so the stack scenario checks a fixture flag under a unique `--app-name`, read back through the Admin API's raw usage metrics with `integration.WaitForUsage`.

### PR 3: automatic remoteAddress

- [x] Branch `m4/remote-address`, stacked on `m4/evaluate`. Implemented and committed with its review findings fixed, not yet pushed; `task`, `prek run --all-files` and `task go:test-integration` pass locally.
- Contents: without `--remote-address`, the local address of releash's connection to Unleash becomes `remoteAddress`, taken from the SDK's HTTP client; `--remote-address` overrides it; when no address can be found, `--loud` says so and the result does not change; the help text for `--remote-address` and `--current-time` names their defaults; integration tests.
- How it was built: the new `internal/releash/remoteaddress.go` holds a `localAddressTransport` that wraps the cloned `http.DefaultTransport` underneath `boundedTransport` and traces with `httptrace` `GotConn` only `/client/features` requests and their redirect hops, whatever path a hop has, so the address is that of the connection that served the flags (the last hop of a redirect). The change to the SDK client setup in `evaluate` is the one wrapped transport and the `withRemoteAddress` call after a successful fetch, to keep the rebase against PR 2 small. The address is reduced to a plain IP (no port or zone, IPv4-mapped IPv6 read as IPv4). Behind an HTTP proxy it is the address of the connection to the proxy. The `--loud` note is `releash: could not find the local address of the connection to unleash; remoteAddress is unset`, given only when the fetch succeeded and `--remote-address` was not given.
- Tests: unit tests against loopback fake servers over IPv4 and IPv6. A check over TCP always finds an address, so the "no address" note is unit-tested on `withRemoteAddress` only. The integration scenarios use `ip-allowlist` for the runner's address and a scenario flag listing only `203.0.113.7` (RFC 5737, never the runner's) to show the detected address is used and that `--remote-address` overrides it both ways.
- Milestone criteria: `remoteAddress` default and override.

### Close out

- [ ] Every M4 exit criterion holds. Set M4 to `done`, spec 0001 to `implemented`, and update this plan's status.

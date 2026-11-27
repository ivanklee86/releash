- Feature Name: `check_command`
- Start Date: 2026-11-26
- Status: accepted
- Related ADRs: [0001](../adrs/0001-read-only-by-design.md), [0002](../adrs/0002-client-api-over-frontend-api.md), [0003](../adrs/0003-fail-safe-false.md), [0004](../adrs/0004-exit-code-contract.md)

# Summary

`releash check <flag>` evaluates an Unleash feature flag and prints `true` or `false` so CD pipelines can gate steps on it with plain shell scripting. It is read-only, it falls back to `false` when it cannot get an answer, and it reports usage metrics to Unleash like any other client.

# Motivation

Teams already use Unleash to control features at runtime, but release steps in CI/CD (deploying a service, promoting to production, running a migration) are usually gated by hand-edited pipeline config or manual approvals. Moving those gates into Unleash gives one place to see and change them, with Unleash's targeting, audit log and permissions.

Calling the Unleash API directly from a pipeline means handling authentication, flag evaluation and error cases in shell, which is error-prone. releash does that in a single binary with a minimal contract: one string on stdout.

Goals:

- Gate any pipeline step on a flag with one command, in any CI system.
- Never let a releash or Unleash problem turn a gate on. When in doubt, the answer is `false`.
- Evaluate flags exactly as Unleash's own SDKs do, so a flag means the same thing to a pipeline as it does to a service.

Non-goals:

- Changing flags or strategies in Unleash. People do that in the Unleash UI.
- Returning variants, payloads or rollout percentages. These may come later (see [Future possibilities](#future-possibilities)).
- CI-specific integrations such as a GitHub Action.

# Guide-level explanation

Point releash at Unleash with a backend (client) API token, then check a flag:

```sh
export UNLEASH_URL=https://unleash.example.com/api
export UNLEASH_API_TOKEN=default:production.abc123

releash check deploy-payments
# true
```

Gate a step on the result:

```sh
if [ "$(releash check deploy-payments)" = "true" ]; then
  ./deploy.sh
fi
```

releash exits 0 for every answer, including `false`, so `set -euo pipefail` is safe to use. If releash can't reach Unleash, the token is rejected, or the flag doesn't exist, it prints `false`. Add `--loud` to see why on stderr:

```sh
$ releash check deploy-pament --loud
releash: flag "deploy-pament" not found; returning false
false
```

Flags are evaluated against an Unleash context. Each standard Unleash context field has its own option, such as `--env` or `--user-id`, and custom properties are set with `--properties`:

```sh
releash check deploy-payments \
  --env staging \
  --properties region=eu \
  --properties cluster=prod-eu-1
```

This lets the flag's strategies and constraints in Unleash decide per region, cluster or anything else the pipeline knows about, e.g. "enabled for `region=eu` only".

Some fields fill themselves in. `environment` defaults to `production`, `currentTime` is the time of the check, and `remoteAddress` is the runner's IP address, so date constraints and IP-based strategies work without any options.

A mistake in the command or its configuration, such as a malformed timestamp, is not a "no" answer. releash still prints `false`, so the gated step stays off, but it also writes an error to stderr and exits non-zero, so the mistake is caught on the first run:

```sh
$ releash check deploy-payments --current-time tomorrow
releash: invalid --current-time "tomorrow": must be an RFC 3339 timestamp
false
$ echo $?
4
```

Each kind of mistake has its own exit code (see [Exit codes](#exit-codes)), so a pipeline can tell a missing secret apart from a typo in the command.

# Reference-level explanation

## Command

```
releash check <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                     [--env <name>] [--app-name <name>]
                     [--user-id <id>] [--session-id <id>]
                     [--remote-address <ip>] [--current-time <timestamp>]
                     [--properties <name>=<value>]...
```

Options fall into two groups, and the synopsis, `--help` and this spec always list them in this order, under separate headings:

- releash options control how releash connects and reports: `--url`, `--token`, `--timeout`, `--loud`. See [Configuration](#configuration) and [Output and exit codes](#output-and-exit-codes).
- Context options set fields of the Unleash context the flag is evaluated against: `--env`, `--app-name`, `--user-id`, `--session-id`, `--remote-address`, `--current-time`, `--properties`. See [Evaluation context](#evaluation-context).

The grouping is for readability only. Options from both groups may appear in any order on the command line.

## Output and exit codes

This contract covers `releash check`. Other invocations, such as bare `releash` or an unknown command, print help or an error and are not meant for use in scripts. For every exit of `releash check` that releash controls, stdout contains exactly `true\n` or `false\n` and nothing else, whatever the exit code. The exit code says whether that value is an answer: 0 means it is, and anything else means the command or its configuration is wrong and the value is `false`.

Two exceptions apply. `--help` and `--version` are informational: they print help or the version to stdout and exit 0 without evaluating anything, and pipelines should not combine them with a check. A crash that releash cannot recover from (see below) may exit with empty stdout.

### Exit codes

| Exit | Meaning | stdout | stderr |
|---|---|---|---|
| 0 | Answer: the flag was evaluated, or releash fell back to `false` at runtime (see below) | `true` or `false` | nothing (details with `--loud`) |
| 1 | Unexpected internal error, i.e. a bug in releash | `false` | error message |
| 2 | Bad command line: unknown option, missing or extra arguments, an invalid `--timeout`, a releash option given twice | `false` | error message |
| 3 | No Unleash URL or API token configured, or configured as an empty string | `false` | error message |
| 4 | Invalid context value, property name or Unleash URL | `false` | error message |
| 5 | The same context field or property set more than once | `false` | error message |

Exit codes 1–5 always write their error to stderr, with or without `--loud`. `--loud` only adds stderr output and never changes stdout or the exit code.

When one invocation has several mistakes, the lowest non-zero code among them wins (2 before 3, 3 before 4, 4 before 5), so the code does not depend on the order validation runs in.

Exit codes are part of the public contract. New codes may be added for new kinds of error, but existing codes keep their meaning.

An unrecovered Go panic exits 2, which would collide with "bad command line". releash recovers panics on its own goroutine, prints `false`, and exits 1. A panic on one of the SDK's background goroutines cannot be recovered: it exits 2 with empty stdout, so consumers such as `$GITHUB_OUTPUT` get an empty value, and the exit code looks like a bad command line. That case is an SDK bug and is not expected in practice. Comparing against `true`, as the examples do, still keeps the step off.

## Fallback to `false`

releash prints `false` and exits 0 instead of printing an evaluated result when:

- Unleash is unreachable, or a request fails or times out (`--timeout`, default `5s`).
- Unleash rejects the token (HTTP 401 or 403).
- The flag does not exist, or the token cannot see it (e.g. it belongs to a project outside the token's scope). Unleash does not distinguish these.
- The response cannot be parsed.

These are conditions of the environment at the moment of the check, not mistakes in the command, so they exit 0 and leave the gated step off without failing the pipeline. With `--loud`, releash writes one line to stderr naming the reason, e.g. `releash: unleash unreachable: <error>; returning false`.

## Command and configuration errors

These are mistakes in how releash is called, so they exit non-zero (see [Exit codes](#exit-codes)) and still print `false`:

- Exit 2: missing flag name, extra arguments, unknown options, a `--timeout` that is not a positive Go duration (e.g. `abc`, `0`, `-1s`), or a releash option (`--url`, `--token`, `--timeout`, `--loud`) given twice.
- Exit 3: no Unleash URL or no API token, from either options or environment variables. A URL or token set to the empty string, e.g. `UNLEASH_API_TOKEN=` from an unset CI secret, counts as missing. Pipelines where the token is intentionally absent, such as pull requests from forks, should skip the call themselves, e.g. `[ -n "${UNLEASH_API_TOKEN:-}" ] && ...`, which also works under `set -u`. When the output is read with `if [ "$(releash check x)" = "true" ]`, the exit code is ignored and the step is simply skipped.
- Exit 4: an invalid context value or property name (see below), or an Unleash URL that is not an absolute `http` or `https` URL.
- Exit 5: a context option given twice, or the same property name given twice to `--properties`.

## Evaluation context

The Unleash context has a fixed set of standard fields and a `properties` map of strings. Each standard field has its own option:

| Option | Sets | Default | Validation |
|---|---|---|---|
| `--env` | `environment` | `production` | non-empty |
| `--app-name` | `appName` | `releash` | non-empty |
| `--user-id` | `userId` | unset | none |
| `--session-id` | `sessionId` | unset | none |
| `--remote-address` | `remoteAddress` | the runner's IP address (see below) | an IPv4 or IPv6 address |
| `--current-time` | `currentTime` | the time of the check | an RFC 3339 timestamp |

A default applies only when its option is not given, and it never counts as a duplicate.

`--properties <name>=<value>` sets one custom property and can be repeated:

- The name is everything before the first `=` and the value is everything after it, so values may contain `=`. A value may be empty.
- A missing `=` or an empty name exits 4.
- A name that matches a standard field (`userId`, `sessionId`, `remoteAddress`, `environment`, `appName`, `currentTime`) exits 4 with a hint naming the option to use, e.g. `property "userId" is a standard field; use --user-id`. The SDK looks up standard fields before properties, so such a property would be silently ignored.

### Automatic values

- `currentTime`: when `--current-time` is not given, the SDK uses the current time. Date constraints therefore compare against the moment of the check. `--current-time` is for overriding it, e.g. to test a scheduled release ahead of time.
- `remoteAddress`: when `--remote-address` is not given, releash uses the local IP address of its connection to Unleash, i.e. the address of the interface the runner uses to reach Unleash. It does not call any external service. Inside a container this is the container's address, not the host's, and behind NAT it is the private address, not the public one. If no address can be determined, `remoteAddress` is left unset and `--loud` says so; this never changes the result or the exit code.

`environment` in the context is only used by strategy constraints. Which environment's flag configuration releash sees is decided by the API token, because Unleash scopes client tokens to one environment.

## Unleash integration

- releash uses the Unleash Client API (`/api/client/...`) with a backend token, through the official Unleash Go SDK (`github.com/Unleash/unleash-go-sdk/v5`, pinned; v5.1.0 at the time of writing). Flags are evaluated locally by the SDK, so results match Unleash's other SDKs for every strategy and constraint.
- `--timeout` (default `5s`) bounds the whole run: registering with Unleash, the first flag fetch, and the metrics flush. Every HTTP request the SDK makes uses a client with a timeout. The SDK's default client has none, and its synchronous register request otherwise hangs indefinitely against an unresponsive host.
- releash evaluates the flag only after the SDK reports that its first fetch from Unleash succeeded. If that feature fetch fails first (e.g. 401, connection refused), releash falls back to `false` immediately instead of waiting for the timeout. Errors from the register and metrics requests do not affect the answer: the SDK reports them on the same error channel, so releash must tell them apart, and it reports them only under `--loud`.
- releash keeps fetched flags in memory only. The SDK by default persists flags to a backup file in the temp directory and evaluates against it when Unleash cannot be reached. A stale backup on a shared CI runner could turn a gate on, which would break the fail-safe guarantee.
- The SDK does not report unknown flags; it evaluates them as disabled. releash checks the fetched flag list itself so that `--loud` can say the flag was not found.
- Usage metrics: releash reports the evaluation to Unleash like any other client, so the flag's usage data in Unleash shows pipeline checks. The SDK only sends metrics on its metrics interval and does not flush them on `Close`, so releash sets a short interval and waits for the SDK to confirm the send before exiting. Flushing is best-effort, bounded by `--timeout`, and never changes stdout or the exit code. A failed flush is reported with `--loud`.
- releash registers with Unleash under its `appName`, so `--app-name` lets teams tell pipelines apart in Unleash's applications view.

## Configuration

| Setting | Option | Environment variable |
|---|---|---|
| Unleash API URL | `--url` | `UNLEASH_URL` |
| API token | `--token` | `UNLEASH_API_TOKEN` |

Options take precedence over environment variables. Prefer the environment variable for the token, because options are visible in process listings and CI logs.

## Test plan

- Happy path: an enabled flag prints `true`; a disabled flag prints `false`; both exit 0 and write nothing to stderr.
- Context: `--properties region=eu` reaches evaluation, so a constraint on `region` gives different results for `eu` and `us`; each standard field option reaches evaluation; without options, `environment` is `production` and `appName` is `releash`; property values containing `=` are preserved.
- Automatic values: without `--current-time`, a date constraint set just before and just after the check gives the expected results; without `--remote-address`, an IP strategy listing the runner's address enables the flag; `--remote-address` overrides the detected address.
- Fallback: an unreachable server, an unresponsive server (one that accepts connections but never responds), HTTP 401/403, an unknown flag and a malformed response each print `false`, exit 0, are silent without `--loud`, and finish within `--timeout`.
- No stale state: after a successful check, a second check with a bad token or an unreachable server prints `false`, and releash writes no files.
- `--loud`: each fallback reason produces its own stderr line; stdout and the exit code are the same with and without `--loud`.
- Precedence: a missing token combined with an invalid `--current-time` exits 3, and an invalid `--timeout` combined with a missing token exits 2.
- Register errors: a server that rejects the register request but serves features still gives the evaluated answer, and the register error appears only under `--loud`.
- Command and configuration errors: an unknown option and a missing flag name exit 2; missing URL and missing token each exit 3; an invalid `--timeout` and a repeated releash option exit 2; an empty URL or token exits 3; a malformed `--url`, an invalid `--current-time` or `--remote-address`, an empty `--env` or `--app-name`, a property without `=`, an empty property name and a property named after a standard field (with the hint) exit 4; a context option given twice and a repeated property name exit 5. Each prints `false` on stdout and an error on stderr, without needing `--loud`.
- Internal errors: a panic on releash's own goroutine prints `false` and exits 1.
- Metrics: a successful check sends usage metrics for the flag before exit; a metrics endpoint that fails or hangs does not change stdout or the exit code, and does not delay exit past `--timeout`.

# Drawbacks

- Fallback to `false` hides problems. A typo in a flag name or an expired token silently skips the gated step until someone runs with `--loud`. This is deliberate: skipping a step is safer than running one by mistake, but teams should be aware of it.
- Using the SDK means fetching every flag the token can see, not just the one being checked. That's fine for typical Unleash instances, but it is more data than a single-flag lookup.
- The Go SDK is built for long-running services. A one-shot CLI has to work around several of its defaults: the persistent backup file, the HTTP client without a timeout, and metrics that are only sent on a timer.
- Exit codes are lost in common shell forms such as `if [ "$(releash check x)" = "true" ]` and `echo "go=$(releash check x)" >> "$GITHUB_OUTPUT"`. In those forms a command or configuration error behaves like a `false` answer. The error message on stderr is the only signal there.

# Rationale and alternatives

- **Output on stdout, exit 0 for both answers.** Using the exit code for the answer would let `releash check x && ./deploy.sh` work, but `false` would then abort scripts under `set -e` and would look the same as a failure. `git config --bool` follows the same convention.
- **Client API instead of the Frontend API.** The Frontend API returns only enabled flags, so releash could not tell an unknown flag from a disabled one, and `--loud` could not report the most common mistake. It also requires the Frontend API or Unleash Edge to be set up, while backend tokens are what CI usually has.
- **Fail-safe `false` instead of failing the pipeline.** Failing loudly makes outages visible, but it would block every deploy that is gated on any flag while Unleash is down. Defaulting to `false` keeps pipelines running and leaves gated steps off.
- **`false` plus a non-zero exit code for command and configuration errors.** Printing nothing would leave consumers of the output, such as `$GITHUB_OUTPUT`, with an empty value. Exiting 0 would hide the mistake. Printing `false` keeps the step off in every shell form, and the exit code makes the mistake fail scripts that check it.
- **One exit code per kind of error.** A pipeline can react differently to a missing secret (exit 3) than to a broken command (exit 2, 4, 5). Exit 1 is kept for internal errors and 2 for bad command lines, matching common CLI convention.
- **An option per standard field plus `--properties`, instead of one generic `--context key=value` option with dot notation.** Dedicated options can be validated and documented individually, and give releash a place to fill in automatic values for `currentTime` and `remoteAddress`. Dot notation would also accept paths Unleash cannot represent, such as `properties.region.zone`, which then need their own errors.
- **`--properties name=value` instead of separate name and value options.** Separate options are matched up by position, which is easy to get wrong in long commands.
- **Detecting `remoteAddress` locally instead of asking an external service for the public IP.** An external lookup would add a network call, latency and a third-party dependency to every check. The local address is enough to target self-hosted runners by subnet.

# Prior art

- `git config --bool` and similar commands print a normalized boolean for scripts.
- Unleash SDKs evaluate the same flag model locally. releash is a thin CLI over one of them.

# Unresolved questions

None at this time.

# Future possibilities

- `releash variant <flag>` to print the variant name or payload, e.g. to choose an image tag.
- An opt-in `--exit-code` option so `releash check x && ./deploy.sh` works.
- Checking several flags in one call, printing `KEY=VALUE` lines for `$GITHUB_OUTPUT` or dotenv files.
- A config file (e.g. `.releash.yaml`) for the URL, app name and default context. v1 uses only options and environment variables.

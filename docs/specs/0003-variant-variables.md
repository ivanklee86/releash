- Feature Name: `variant_command`
- Start Date: 2026-11-27
- Status: implemented
- Related ADRs: [0001](../adrs/0001-read-only-by-design.md), [0002](../adrs/0002-client-api-over-frontend-api.md), [0003](../adrs/0003-fail-safe-false.md), [0004](../adrs/0004-exit-code-contract.md), [0006](../adrs/0006-variant-variable-names.md), [0007](../adrs/0007-refuse-variant-of-disabled-flag.md), [0008](../adrs/0008-leave-command-line-parsing-to-cobra.md)

# Summary

`releash variant <flag>` evaluates an Unleash flag's variant and prints it, with its payload, as environment variable assignments that a pipeline can append to `$GITHUB_ENV`, a GitLab dotenv report or a shell. JSON payloads are flattened into one variable per leaf, so a nested payload becomes plain variables such as `RELEASH_CHECKOUT_PAYLOAD__IMAGE__TAG=v2`. `releash check` stays the gate, and `variant` configures what runs behind it.

# Motivation

Spec 0001 answers "should this step run?". Pipelines also need "how should it run?": which image tag to deploy, how many canary replicas to start, which config bundle a load test should use. Unleash already models this with variants and payloads, and its UI lets people change them, target them with constraints and split traffic between them. Today a pipeline would have to call the Client API and parse the payload with `jq` itself.

Environment variables are the one configuration interface every CI system and every tool in a pipeline understands. If releash turns a variant into variables, any later step can read the experiment's settings without knowing about Unleash or JSON.

Goals:

- Turn a flag's variant and payload into environment variables with one command, in any CI system.
- Handle JSON payloads of any shape, with variable names a person can predict from the payload.
- Keep the guarantees of spec 0001: read-only, fail-safe, evaluation identical to Unleash's SDKs, and a fixed output contract.

Non-goals:

- Replacing `releash check` as the gate. `variant` refuses to run for a disabled flag, so pipelines must gate on `check` first (see [Rationale and alternatives](#rationale-and-alternatives)).
- Detecting the CI system. releash does not read CI-specific variables. This spec documents which ones to pass as context for each common CI system instead (see [Keep the variant sticky](#keep-the-variant-sticky)).
- Evaluating several flags in one call. That is still a future possibility in spec 0001.
- Writing directly into a CI system's files, such as `$GITHUB_ENV`. releash writes to stdout and the pipeline redirects it.

# Guide-level explanation

A flag `checkout-redesign` in Unleash has two variants, `control` and `treatment`. `treatment` carries a JSON payload:

```json
{"image": {"tag": "v2"}, "replicas": 3, "regions": ["eu", "us"]}
```

Gate on the flag with `check`, then load the variant, passing both the same context:

```sh
if [ "$(releash check checkout-redesign --user-id "$GITHUB_REPOSITORY")" = "true" ]; then
  releash variant checkout-redesign --user-id "$GITHUB_REPOSITORY" >> "$GITHUB_ENV"
fi
```

The variant command prints:

```text
RELEASH_CHECKOUT_REDESIGN_ENABLED=true
RELEASH_CHECKOUT_REDESIGN_VARIANT=treatment
RELEASH_CHECKOUT_REDESIGN_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG=v2
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0=eu
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1=us
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS=3
```

Later steps read them like any other variable, with a default for when the experiment is off:

```sh
./deploy.sh --tag "${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"
```

Nesting is spelled with a double underscore (`__`), and array elements by their index. `RELEASH_CHECKOUT_REDESIGN_PAYLOAD` holds the whole payload as compact JSON, for tools that would rather parse it.

`--prefix` replaces `RELEASH_CHECKOUT_REDESIGN_` with a prefix of your choice:

```sh
releash variant checkout-redesign --prefix CHECKOUT_
# CHECKOUT_ENABLED=true
# CHECKOUT_VARIANT=treatment
# ...
```

`--format shell` prints `export` statements, quoted so they can be passed to `eval` safely. Capture the output first, so that releash's exit code still fails the step: `eval "$(releash ...)"` exits with `eval`'s status, which hides exit 6 and codes 2–5.

```sh
vars=$(releash variant checkout-redesign --user-id "$GITHUB_REPOSITORY" --format shell) && eval "$vars"
```

## Keep the variant sticky

Unleash assigns variants by stickiness. To pick the variant, default stickiness uses `userId`, then `sessionId`; the Go SDK then uses `remoteAddress`, which releash sets to the local address of its connection to Unleash unless `--remote-address` is given, and only then picks at random. A gradual rollout on default stickiness skips `remoteAddress`: without `userId` or `sessionId`, whether the flag is on at all is decided at random on every run. A pipeline usually sets neither ID, so without help a partial rollout turns on and off from run to run, and the variant follows the connection's address rather than the repository; containers and proxies can give every repository the same address, and so the same variant. releash does not guess which CI system it runs on, so pass something that identifies the unit you are experimenting on, usually the repository or pipeline.

The simplest way is to pass the repository name as `--user-id`. Default stickiness uses `userId` first, so this needs no change in Unleash, and each repository always gets the same variant:

```sh
releash variant checkout-redesign --user-id "$GITHUB_REPOSITORY" >> "$GITHUB_ENV"
```

Pass the same `--user-id` to `check`, so that a gradual rollout on default stickiness puts the gate and the variant in the same bucket.

If `userId` already means something else in your Unleash setup, or you want stickiness on something other than the repository, pass a custom property, to `check` too, and set the variant's stickiness to that property in Unleash. Create the property as a context field with stickiness enabled, so that Unleash offers it as a stickiness option; evaluation itself does not need the context field. Custom stickiness does not fall back to `userId`. A run that omits the property gets a random feature variant, and a strategy whose stickiness is that property does not match at all, so unless another strategy matches the flag is off for that run: `check` prints `false` and `variant` exits 6:

```sh
releash variant checkout-redesign --properties repo="$GITHUB_REPOSITORY" >> "$GITHUB_ENV"
```

The repository or pipeline name for each common CI system:

| CI system | Repository or pipeline |
|---|---|
| GitHub Actions | `$GITHUB_REPOSITORY` |
| GitLab CI/CD | `$CI_PROJECT_PATH` |
| Jenkins | `$JOB_NAME` |
| CircleCI | `$CIRCLE_PROJECT_USERNAME/$CIRCLE_PROJECT_REPONAME` |
| Buildkite | `$BUILDKITE_PIPELINE_SLUG` |
| Azure Pipelines | `$BUILD_REPOSITORY_NAME` |

With `--loud`, releash warns when the flag uses default stickiness and neither `--user-id` nor `--session-id` is set.

## Targeting with context

`variant` takes every context option `check` does, including any number of `--properties`, with the same defaults and validation. Strategy constraints and strategy variants in Unleash can therefore give each region, cluster or environment its own variant:

```sh
releash variant checkout-redesign \
  --user-id "$GITHUB_REPOSITORY" \
  --env staging \
  --properties region=eu \
  --properties cluster=prod-eu-1
```

Pass the same context to `check` and `variant`. If they differ, `check` can answer `true` for a context in which `variant` finds the flag disabled, and `variant` exits 6.

## When there is no variant

If the flag is enabled but has no variants, releash prints the disabled variant with `ENABLED=true` and exits 0:

```text
RELEASH_CHECKOUT_REDESIGN_ENABLED=true
RELEASH_CHECKOUT_REDESIGN_VARIANT=disabled
```

If releash cannot get an answer, it prints the disabled variant with `ENABLED=false` and exits 0. This is the variant counterpart of spec 0001's `false`: steps fall back to their defaults.

If the flag is disabled, the pipeline has called `variant` without gating on `check`. releash prints the same disabled variant on stdout, writes an error to stderr and exits 6:

```sh
$ releash variant checkout-redesign
releash: flag "checkout-redesign" is disabled; gate on "releash check checkout-redesign" before loading its variant
RELEASH_CHECKOUT_REDESIGN_ENABLED=false
RELEASH_CHECKOUT_REDESIGN_VARIANT=disabled
$ echo $?
6
```

# Reference-level explanation

## Command

```text
releash variant <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                       [--format dotenv|shell] [--prefix <prefix>]
                       [--env <name>] [--app-name <name>]
                       [--user-id <id>] [--session-id <id>]
                       [--remote-address <ip>] [--current-time <timestamp>]
                       [--properties <name>=<value>]...
```

The connection options, the context options and their defaults, validation, automatic values (including the automatic `remoteAddress`) and configuration are exactly those of spec 0001 and share its implementation. `variant` adds two releash options, `--format` and `--prefix`. `--help` lists them with the other releash options, under the same three headings as `check`: "Releash options", "Context options" and "Other options" (`--help`, `--version`). As with `check`, options may also go before the command name: `releash --format shell variant x` is the same as `releash variant x --format shell`.

## Variables

Every variable name is the prefix followed by a suffix:

| Variable | Value |
|---|---|
| `<prefix>ENABLED` | `true` if the flag is enabled for the context, otherwise `false` |
| `<prefix>VARIANT` | The variant name, or `disabled` |
| `<prefix>PAYLOAD` | The payload value as Unleash stores it; for `json` payloads, the payload re-encoded as compact JSON. Absent when the variant has no payload. |
| `<prefix>PAYLOAD__<path>` | One per leaf of a `json` payload (see [Flattening](#flattening)). |

The default prefix is `RELEASH_` followed by the flag name normalised as below and a trailing `_`, e.g. `RELEASH_CHECKOUT_REDESIGN_`. `--prefix` must match `^[A-Z][A-Z0-9_]*$`. An invalid or empty `--prefix` exits 2, like any invalid releash option.

Flags whose names differ only in case or punctuation, such as `checkout-redesign` and `checkout.redesign`, get the same default prefix, and a flag name ending in `-payload-` can produce names that overlap another flag's payload variables. A pipeline that loads such flags together must give at least one of them its own `--prefix`; releash does not detect it, because each call sees only one flag.

`--prefix` is not checked against a list of dangerous names such as `PATH` or `LD_PRELOAD`. The payload controls only the part of a name after `<prefix>PAYLOAD__`, so no payload can produce such a name with any prefix, and the three names that follow the prefix directly (`ENABLED`, `VARIANT`, `PAYLOAD`) are fixed. See [ADR 0006](../adrs/0006-variant-variable-names.md).

Names are normalised by converting ASCII letters to upper case and replacing every Unicode code point outside `[A-Z0-9_]` with one `_`. So `image-tag`, `image.tag` and `image tag` all become `IMAGE_TAG`, and `größe` becomes `GR__E` (one `_` each for `ö` and `ß`, though each is two bytes in UTF-8).

Variables are printed in a fixed order: `ENABLED`, `VARIANT`, `PAYLOAD`, then the flattened variables sorted by name in byte order.

## Payload types

Unleash variant payloads have a type and a string value:

| Type | Variables |
|---|---|
| `string` | `<prefix>PAYLOAD` with the value unchanged |
| `number` | `<prefix>PAYLOAD` with the value unchanged |
| `csv` | `<prefix>PAYLOAD` with the value unchanged |
| `json` | `<prefix>PAYLOAD` with compact JSON, plus one `<prefix>PAYLOAD__<path>` per leaf |

An unknown payload type is treated like `string`.

## Flattening

A `json` payload is walked from the root, and each leaf becomes one variable:

- An object member adds `__` and the normalised member name to the path.
- An array element adds `__` and its zero-based index to the path.
- A string leaf is the string unchanged, without quotes.
- A number leaf is the number exactly as written in the payload, so `1.0` stays `1.0` and large integers are not rounded.
- `true` and `false` are `true` and `false`.
- `null` is the empty string, so the variable exists and `${VAR-default}` and `${VAR:-default}` give different results.
- An empty object or array produces no variable of its own; it is only visible in `<prefix>PAYLOAD`.
- Arrays get no length variable. Pipelines that loop over an array should parse `<prefix>PAYLOAD`.
- A payload whose root is a scalar produces only `<prefix>PAYLOAD`.

Two paths that normalise to the same name are a collision, e.g. `{"a-b": 1, "a_b": 2}`, or `{"a__b": 1, "a": {"b": 2}}`. An object with a duplicate member name is also a collision. A collision makes the payload unusable (see [Fallback](#fallback-to-the-disabled-variant)), because quietly dropping one of the values would give the pipeline a configuration nobody wrote.

## Output formats

| `--format` | Output | Meant for |
|---|---|---|
| `dotenv` (default) | `NAME=value`, one per line, with no quoting or escaping | `$GITHUB_ENV`, `$GITHUB_OUTPUT`, GitLab `artifacts:reports:dotenv`, `docker run --env-file` |
| `shell` | `export NAME='value'`, one per line, with `'` written as `'\''` | `eval` in POSIX shells |

`dotenv` has no way to represent a newline in a value, and the consumers above disagree on quoting, so values are written as they are. A value that contains a newline (`\n` or `\r`) or a NUL in `dotenv` format makes the payload unusable. `shell` can represent any value but a NUL, which it rejects because shells cannot hold it in a variable.

CI systems can limit how many variables or how many bytes a dotenv file or env file may hold, and every JSON leaf is one variable while `<prefix>PAYLOAD` repeats the whole payload. Keep payloads meant for pipelines small, and check the limits of the CI system before relying on a large one; releash does not know them.

## Output and exit codes

The exit codes and their meanings are those of spec 0001 and [ADR 0004](../adrs/0004-exit-code-contract.md), plus exit 6. For every exit that releash controls, stdout is the variant output in the requested format and nothing else. On exit 0 it is the evaluated variant or the fallback. On exit 2, a bad command line, stdout is empty ([ADR 0008](../adrs/0008-leave-command-line-parsing-to-cobra.md)): the command line may not say which format and prefix to print in, and a pipeline that gets its command line wrong should fail rather than load defaults. On every other exit it is the fallback, in the format and with the prefix that were given, which validation has already accepted by then. As in spec 0001, `--help` and `--version` are informational, and a panic that releash recovers prints the fallback and exits 1, or prints nothing if the options it parsed give no single valid format and prefix, or if it panicked before parsing them.

| Exit | Meaning | stdout | stderr |
|---|---|---|---|
| 2 | Bad command line: the cases of spec 0001 and those below | nothing | error message |
| 6 | The flag was evaluated and is disabled for the context | the fallback | error message naming `releash check` |

Exit 6 is a mistake in the pipeline, not in the command line, so it ranks after codes 1–5: when the command line is also wrong, the lower code wins and the flag is never evaluated. It is new to [ADR 0004](../adrs/0004-exit-code-contract.md) and applies only to `variant`; `check` never exits 6.

Additional exit 2 cases: an invalid `--format` or `--prefix`, or either given twice.

## Fallback to the disabled variant

releash prints the disabled variant (`ENABLED=false`, `VARIANT=disabled`, no payload variables) and exits 0 in every fallback case of spec 0001, and also when:

- A `json` payload is not valid JSON.
- Flattening produces a collision.
- A value cannot be represented in the requested format.

These are problems with what is stored in Unleash, not mistakes in the command, so like spec 0001's fallbacks they exit 0 and are reported only under `--loud`, e.g. `releash: payload of variant "treatment" has colliding keys "a-b" and "a_b"; returning disabled variant`.

A flag that is enabled with no variants is an answer, not a fallback, and prints nothing on stderr even with `--loud`. A disabled flag exits 6 (see [Output and exit codes](#output-and-exit-codes)).

A fallback exits 0 even when it happens after `check` answered `true`, e.g. because Unleash became unreachable between the two calls. Only an evaluated, disabled flag exits 6.

## Unleash integration

- `variant` runs through the same evaluation as `check` and differs only in the final call: everything in spec 0001's "Unleash integration" applies as written, including memory-only storage, the `--timeout` bound and the register deadline, evaluating only after a successful first fetch, telling fetch errors from register and metrics errors, every fallback and its `--loud` line, and the wait for usage metrics.
- Evaluation uses the SDK's `GetVariant` (`github.com/Unleash/unleash-go-sdk/v6`) with the same context as `check`, so strategy variants, feature variants, weights and stickiness behave as in every other Unleash SDK.
- `GetVariant` reports whether the flag is enabled (`FeatureEnabled`) and the chosen variant. `ENABLED` comes from `FeatureEnabled`, so it can be `true` while `VARIANT` is `disabled`.
- The SDK returns the same `disabled` variant for a flag that is unknown and for one that is disabled. releash therefore looks the flag up in the flags it fetched (`ListFeatures`): a flag that is not there is spec 0001's "flag not found" fallback and exits 0; a flag that is there and not enabled for the context exits 6.
- releash sets no `VariantFallbackFunc` or fallback variant. The SDK uses either for unknown and disabled flags, and also for an enabled flag that has no variants, where a releash fallback would hide that the flag is enabled. Without one, the SDK returns its own `disabled` variant with `FeatureEnabled` set as evaluated, which is what `ENABLED` reports. The M6 spike confirms the no-variants case against Unleash 8.3.1.
- A flag the SDK fails to evaluate, e.g. one whose strategy names a segment missing from the response, is reported through the SDK's error channel as for `check`, and falls back to the disabled variant with exit 0. Such a flag is also in the fetched flags and not enabled, so the evaluation error takes precedence: releash decides on exit 6 only after it has checked for one.
- The default-stickiness warning reads the stickiness the SDK hashes with, from the fetched flag: for feature variants, the first variant's `stickiness`; for strategy variants, the strategy's `stickiness` parameter, which the SDK uses in place of the variants' own, or the first variant's `stickiness` when the strategy has none. A value of `default` or an empty one means default stickiness.
- Usage metrics: the SDK counts a `GetVariant` call as one evaluation of the flag and one use of the variant it returned, so Unleash's metrics show which variants pipelines were given. releash waits for the send as `check` does, and only after an evaluated answer, which here means exit 0 with an evaluated variant: a fallback and exit 6 both skip the wait.

## Test plan

- Happy path: a flag with a `json` payload prints the variables in the documented order and format; `string`, `number` and `csv` payloads print only `PAYLOAD`; a variant without a payload prints only `ENABLED` and `VARIANT`.
- Answers without a variant: an enabled flag without variants prints `ENABLED=true` and `VARIANT=disabled`, exits 0 and is silent with `--loud`.
- Disabled flag: prints `ENABLED=false` and `VARIANT=disabled` in the requested format, writes the error naming `releash check` to stderr without `--loud`, and exits 6. A disabled flag combined with a command line error exits with the command line error's code, with empty stdout when that is 2.
- Flattening: nested objects, arrays, arrays of objects, `null`, booleans, empty objects and arrays, a scalar root, number text preserved (`1.0`, `12345678901234567890`), and member names with `-`, `.`, spaces and non-ASCII characters.
- Collisions: names that normalise to the same variable, `__` in a member name that collides with nesting, and duplicate member names each fall back with their own `--loud` line.
- Formats: a value with `'`, `$`, `` ` ``, `\` and spaces survives `eval` of `--format shell` unchanged; a value with a newline falls back in `dotenv` and is preserved in `shell`.
- Prefix: the default prefix normalises the flag name; `--prefix` replaces it; an empty, lower-case or otherwise invalid prefix exits 2.
- Stickiness: with `--user-id` fixed, repeated runs get the same variant; with a custom stickiness property, each value maps to the same variant every time; `--loud` warns when the flag uses default stickiness and neither `--user-id` nor `--session-id` is set.
- Fallback: every fallback case of spec 0001, invalid JSON in a `json` payload, and each unrepresentable value print the disabled variant, exit 0, are silent without `--loud`, and finish within `--timeout`.
- Command and configuration errors: exits 3, 4 and 5 print the fallback in the requested format and with the requested prefix. Exit 2 prints nothing, whether cobra or validation finds the problem: an unknown option with a valid `--format`, a trailing `--format`, `--format` or `--prefix` given twice with the same value, a missing or extra flag name with a valid `--prefix`, and an invalid `--timeout`. A recovered panic prints the fallback in the parsed format and prefix and exits 1, or prints nothing when they are invalid.
- Option placement: shared options and `--format` before `variant` work as they do after it; an option that takes `variant` or `check` as its value does not change the command; an unknown option before `variant` exits 2 with empty stdout.
- Unknown and disabled flags: an unknown flag prints the disabled variant and exits 0 with the not-found `--loud` line; a disabled flag exits 6; a flag the SDK cannot evaluate falls back with exit 0.
- Metrics: a successful variant evaluation sends metrics, including the variant count, before exit; a fallback and exit 6 do not wait for metrics.

## Versioning

`variant` is a new command, so under [spec 0002](0002-distribution.md) it ships in a minor release (the first after `v0.1.2` is `v0.2.0`), and its pull requests carry the `enhancement` label. From that release, the variable names, their order, the formats and exit 6 are public contract: changing the naming rules in [ADR 0006](../adrs/0006-variant-variable-names.md) is a breaking change.

`v0.3.0` changes stdout on exit 2 from the disabled variant to nothing ([ADR 0008](../adrs/0008-leave-command-line-parsing-to-cobra.md)). That is a breaking change, so it ships in a minor release with the `breaking-change` label.

# Drawbacks

- The variable names become public contract with the first release that has `variant`, so mistakes in the naming rules can only be fixed with a breaking change.
- Variable names are upper case, so payload keys that differ only in case (`imageTag` and `imagetag`) collide, and camelCase keys run together (`IMAGETAG`). Teams should use snake_case or kebab-case keys in payloads meant for pipelines.
- The double underscore is unambiguous only because a key containing `__` that would collide is rejected. A payload can still contain such a key if nothing collides with it, which makes the name look nested when it is not.
- `dotenv` cannot carry multi-line values, so a multi-line string payload falls back silently in the default format. `--loud` reports it, and `--format shell` carries it.
- Running `check` then `variant` is two runs: two fetches, two registrations and two sets of metrics. If someone turns the flag off between them, `check` says `true` and `variant` exits 6, which fails the step under `set -e`. That is rare and loud rather than wrong, but the two calls are not atomic.
- Exit 6 breaks the rule from spec 0001 that an evaluated flag always exits 0. It is limited to `variant`, where calling it on a disabled flag means the pipeline skipped its gate.
- Exit 6 only fails a step that sees releash's exit code. `releash variant ... >> "$GITHUB_ENV"` does, but `eval "$(releash variant ... --format shell)"` does not, because the step gets `eval`'s status. The examples capture the output first for that reason.
- Environment variables are visible to every later step and to any process those steps start. Payloads must not hold secrets, which is already Unleash's advice.

# Rationale and alternatives

- **A separate `variant` command instead of options on `check`.** `check`'s stdout contract is exactly `true` or `false`, and pipelines rely on that in `if [ "$(releash check x)" = "true" ]`. Adding variables to it would break that contract.
- **`check` as the gate, `variant` as configuration.** `variant` could gate on its own, since it prints `ENABLED`. Keeping the gate in `check` means every gate in a pipeline looks the same and gets `check`'s exit code contract, and `variant` can be added to a pipeline that is already gated without changing the gate.
- **Exit 6 for a disabled flag instead of printing the disabled variant and exiting 0.** Exiting 0 would let a pipeline that forgot its `check` gate run the gated step with default settings, which looks like success. A dedicated code makes the missing gate fail the first run, and keeps it distinct from command line and configuration errors. Runtime fallbacks still exit 0, as in spec 0001, so an Unleash outage does not fail pipelines.
- **Documenting CI variables instead of reading them.** Defaulting `--user-id` from `GITHUB_REPOSITORY` or `CI_PROJECT_PATH` would make runs sticky without options, but it would be releash's first CI-specific behaviour, would change results when a pipeline moves between CI systems, and would be invisible in the command line. A table of variables per CI system keeps the context explicit.
- **Naming the command `variant`.** Unleash, its SDKs and its UI all call these variants, and variants are used for configuration as well as experiments. `experiment` would match Unleash's `experiment` flag type, but variants work on every flag type. `env` reads as Unleash's environments.
- **Flattening with `__` and indices.** This is the convention of ASP.NET Core configuration and pydantic-settings, so many engineers already know it. A single `_` would make `image_tag` and `image.tag` indistinguishable from nesting. JSON Pointer (`/image/tag`) or dots are not valid in variable names.
- **Also printing the whole payload as JSON.** Tools that understand JSON should not have to rebuild it from flattened variables, and arrays are much easier to loop over from JSON.
- **Falling back on collisions and unrepresentable values instead of dropping or escaping them.** A partially applied payload is a configuration nobody wrote. Falling back to the disabled variant sends the pipeline to its defaults, which is the same safe outcome as spec 0001's `false`.
- **A required, validated prefix and a fixed `PAYLOAD__` segment.** Without them, a payload key such as `path` or `ld_preload` would set `PATH` or `LD_PRELOAD` for every later step, letting anyone who can edit the flag run code in the pipeline. Because every payload variable starts with `<prefix>PAYLOAD__`, no payload can choose a name outside that namespace. The default prefix is derived from the flag name, so flags with different names normally write different variables; flags whose names differ only in case or punctuation need `--prefix` (see [Variables](#variables)).
- **`dotenv` as the default format.** It is accepted as-is by GitHub Actions, GitLab and Docker, and appending it to a file never executes anything. `shell` is opt-in because `eval` of remote data is only safe with exact quoting.
- **Empty stdout on exit 2 instead of the disabled variant.** Up to `v0.2.0`, releash re-read the raw arguments of a command line that failed to parse to find the format and prefix of the fallback. That scan duplicated the option parser's rules and could drift from them, and it served command lines that fail the step anyway. Exits 3 to 5 come after a successful parse, so they keep printing the fallback ([ADR 0008](../adrs/0008-leave-command-line-parsing-to-cobra.md)).

# Prior art

- ASP.NET Core and pydantic-settings map nested configuration to environment variables with `__`.
- GitHub Actions `$GITHUB_ENV` and `$GITHUB_OUTPUT`, and GitLab's dotenv reports, take `NAME=value` lines.
- `aws configure export-credentials --format env` and `direnv` print `export` statements meant for `eval`.
- Unleash's SDKs expose `getVariant` with the same payload types.

# Unresolved questions

None at this time.

# Future possibilities

- A `github` format that writes GitHub's `NAME<<DELIMITER` syntax with a random delimiter, so multi-line values work with `$GITHUB_ENV` and `$GITHUB_OUTPUT` without `--format shell`. Deferred because multi-line string payloads should be rare in pipelines.
- A `json` format that prints the result as one JSON object, `{"enabled": bool, "variant": string, "payload": {"type": string, "value": any} or null}`, for tools such as `jq`. Left out of the first version because pipelines want environment variables; `<prefix>PAYLOAD` already holds the payload as compact JSON for a tool that needs it.
- Selecting part of a payload, e.g. `--path image`, to keep variable names short.
- Several flags in one call, which would make `check` and `variant` atomic for a pipeline.

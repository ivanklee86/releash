# Plan: M6 variant variables

Started 2026-11-27. Status: in progress. Milestone: [M6](../milestones/M6-variant-variables.md).

## Resume here

Updated 2026-11-27. The spike against Unleash 8.3.1 has run (see [Spike findings](#reference)), and PR 1 is on branch `m6/variables`, waiting for review: the acceptance edits, this plan and the `internal/variant` package. Before PR 2 and PR 3, the user answers the open questions from the spike. The plan is three stacked PRs: the variable and format rules, the `variant` command, and the fixtures with the integration scenarios and README. Then `v0.2.0`. When resuming, start the first step in [Steps](#steps) that is not checked off and whose dependencies are done. Before ending a session, update this section and check off finished steps.

## How to work through this plan

- One PR per step. Read this plan, the M6 milestone, spec 0003, ADRs 0004, 0006 and 0007, spec 0001 and `AGENTS.md` first. The spec-writing plan `2026-11-27-variant-variables.md` holds the decisions behind spec 0003.
- Before handing off, run `task`, `prek run --all-files`, and `task go:test-integration` against the stack (`task services`). Never run `task services:down`; the stack is shared.
- The user runs `/ship`. Claude never merges, and pushes a tag only when the user says go.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing.
- Pull requests carry the `enhancement` label, so the `v0.2.0` notes list them under Features.

## Reference

Decisions:

- 2026-11-27, the user: merging #26 and asking to start M6 accepts spec 0003 and ADRs 0006 and 0007 as reworked. PR 1 sets them to `accepted`, removes "Proposed, not yet in effect" from ADR 0004's exit 6 row, and sets M6 to `active`.
- 2026-11-27, PR 1, not covered by spec 0003 and taken as the conservative choice, for the user to confirm in review: every node below the root, containers included, claims its variable name, so `{"a-b": {"c": 1}, "a_b": {"d": 2}}` and `{"a-b": {}, "a_b": 1}` are collisions although no two leaves share a name; `--loud` messages name keys by their JSON Pointer path without the leading `/` (`a/b`, `0/name`); an empty member name normalises to the empty string, giving `<prefix>PAYLOAD__`; only the payload type `json`, in lower case, is flattened; and a carriage return in `dotenv` is unrepresentable like a newline, as spec 0003's "(`\n` or `\r`)" says.

Spike findings, 2026-11-27, against Unleash 8.3.1 at `localhost:4242` with `unleash-go-sdk` v6.5.1. Every flag, segment and context field the spike created was archived or deleted afterwards. Three findings differ from spec 0003 or this plan; they are marked "differs".

1. Feature variants through the Admin API (differs from this plan). On a flag with no environment variants, `PUT /api/admin/projects/default/features/{f}/environments/{env}/variants` is refused with 403 `Environment variants deprecated for feature: {f}. Use strategy variants instead.` `PUT /api/admin/projects/default/features/{f}/variants-batch` with `{"variants": [...], "environments": ["development", ...]}` works on a fresh flag. Once a flag has environment variants in any environment, the per-environment `PUT` works in every environment. The legacy `/features/{f}/variants` path is 404, and `GET .../environments/{env}/variants` reads them back; the feature's own `GET` does not include them. A variant has `name`, `weight`, `weightType` (`variable`), `stickiness` and an optional `payload` of `{type, value}`; all four payload types are accepted. The server rescales variable weights to sum to 1000 (100 and 300 were stored as 500 and 500), the Client API serves variants sorted by name, and an omitted feature-variant stickiness is stored as `default`. Strategy variants go in the `variants` array of `POST .../environments/{env}/strategies`, and each needs a `stickiness` or the server answers 400. Admin API validation: invalid JSON in a `json` payload and an unknown payload type are 400; a `json` payload with duplicate keys and a `number` payload `abc` are accepted and served verbatim; a stickiness naming an unknown field or a field without stickiness enabled is accepted. So for PR 3, `seed.py` needs a per-environment feature variants fixture schema, a comparison against `GET .../environments/{env}/variants` normalised the way the server stores them (sorted by name, the server's weights, payload as `{type, value}` strings, stickiness defaulted to `default`), writes through `variants-batch` with `environments: [env]`, and a `stickiness` key for context fields (it now always writes `false`). Fixture strategy variants must always carry `stickiness`, because `normalize_strategy` compares them as canonical JSON of the raw list. `integration.CreateFlag` needs the same `variants-batch` call.
2. `GetVariant` with no fallback (matches spec 0003). The Client API returns feature variants as the top-level `variants` and strategy variants inside each strategy, as `{name, weight, weightType, stickiness, payload}`, with `payload` omitted when there is none; a variant without a payload reaches releash with an empty `Payload.Type` and `Payload.Value`.

   | Case | Variant | `Enabled` | `FeatureEnabled` | Also |
   |---|---|---|---|---|
   | Enabled flag with variants | the chosen variant | true | true | |
   | Enabled flag without variants | `disabled` | false | true | |
   | Disabled flag | `disabled` | false | false | in `ListFeatures`, not enabled |
   | Unknown flag | `disabled` | false | false | not in `ListFeatures` |
   | Missing segment | `disabled` | false | false | `OnError` "segment does not exist"; in `ListFeatures` and enabled |
   | Segment constraint that does not match | `disabled` | false | false | no error: the exit 6 path |
   | Strategy variants on a strategy without `groupId` (`default`) | `disabled` | false | false | `OnError` "invalid groupId type for feature ...": the exit 0 fallback |

3. Precedence and stickiness fields (differs from spec 0003). Strategy variants win over feature variants. `ListFeatures` exposes `Variants[].Stickiness` and `Strategies[].Variants[].Stickiness` as stored; default stickiness is the literal `default`, never empty, and the SDK also treats `""` as default. For strategy variants the SDK ignores the variants' own stickiness: it uses the strategy's `Parameters["stickiness"]`, and falls back to the variants' stickiness only for a strategy without one, which has no `groupId` and so fails to evaluate. Confirmed live with strategy variants marked `region` on a `flexibleRollout` with stickiness `default`: the variant followed `userId` and ignored `region`. Feature variants use `Variants[0].Stickiness`.
4. Stickiness across runs (differs from spec 0003). A fixed `userId` gave the same variant in three separate processes, and different users spread across variants. With no `userId`, `sessionId` or `remoteAddress`, five runs gave four different variants. A custom stickiness property gave each value a fixed variant across runs, and without it the variant was random even with `userId` set, because custom stickiness does not fall back to `userId`. The SDK hashes the context field without checking the context field definition, so evaluation needs no context field with stickiness enabled; Unleash needs one only to offer the field in its UI. The SDK's default stickiness is `userId`, then `sessionId`, then `remoteAddress`, then random, where spec 0003 says `userId`, then `sessionId`, then random. A fixed `remoteAddress` gave the same variant on every run, and releash sets `remoteAddress` automatically, so runs without `--user-id` are sticky per runner address: stable on a fixed host, varying on ephemeral CI runners.
5. Metrics (matches spec 0003). One `GetVariant` sends a toggle entry `{"yes": 1, "no": 0, "variants": {"j": 1}}`, and Unleash stores the variant counts. An enabled flag without variants counts `yes: 1` and `variants: {disabled: 1}`; a disabled flag, an unknown flag and a missing segment count `no: 1` and `variants: {disabled: 1}`. The SDK counts a fallback evaluation in its buffer even though releash does not wait for the send.
6. `json` payload values (matches spec 0003). The Client API serves `payload.value` as a string holding the JSON text exactly as stored, and it reaches `api.Payload` unchanged, so `1.0` and `12345678901234567890` keep their text. Duplicate keys also arrive verbatim, so releash's duplicate-member check is needed against real Unleash. Invalid JSON and unknown payload types cannot be stored through the 8.3.1 Admin API, so those fallbacks can only be tested with `FakeUnleash`. A `number` payload `1.50` stays `1.50`, and a non-numeric one such as `abc` is served as is.

Open questions from the spike, for the user before PR 2 and PR 3 (none affects PR 1):

- The default-stickiness `--loud` warning: spec 0003 says it reads the stickiness of feature variants and strategy variants, but the SDK decides strategy-variant stickiness by the strategy's `stickiness` parameter. Proposed: read `Strategies[].Parameters["stickiness"]` for strategy variants and `Variants[0].Stickiness` for feature variants, and amend spec 0003's "Unleash integration" to match.
- Spec 0003's "Keep the variant sticky" and its stickiness bullet should add the `remoteAddress` step, and say that without `--user-id` runs stick to the runner's address rather than being random.
- Spec 0003 says a custom stickiness property must exist as a context field with stickiness enabled. Evaluation does not need it; only the Unleash UI does. Proposed: keep the advice, worded as what the UI needs.
- `seed.py` and `integration.CreateFlag` write feature variants through `variants-batch` (finding 1); the milestone's risk section names the per-environment endpoint.

## Steps

### PR 1: variable names and formats

- [x] Branch `m6/variables`.
- Contents: the acceptance edits above; this plan in `docs/agents/plans`; a new package (e.g. `internal/variant`) that turns an evaluated result (enabled, variant name, payload type and value) into the ordered variables of spec 0003 and renders them as `dotenv` or `shell`: prefix validation and the default prefix from a flag name, normalisation per Unicode code point, flattening with `__` and indices, number text preserved, `null` as empty, empty containers and scalar roots, every kind of collision, invalid JSON, unrepresentable values per format, and the fallback (disabled variant) output. Exhaustive table-driven unit tests, including a test that `eval` of `shell` output in `/bin/sh` reproduces values with `'`, `$`, backticks, `\`, spaces and newlines. No network, no CLI. `docs/agents/internals.md`.
- Milestone criteria: flattening, normalisation, collisions, formats.

### PR 2: the variant command

- [ ] Branch `m6/command`, stacked on `m6/variables`.
- Contents: exit code 6 in `internal/releash`; `evaluate` generalised so `check` and `variant` share one path (storage, timeout, register deadline, readiness, error sorting, fallbacks, `remoteAddress`, metrics wait) and differ only in the final call; `variant` calls `GetVariant` with no fallback, tells unknown from disabled through the fetched flags, lets an SDK evaluation error take precedence over exit 6, skips the metrics wait for fallbacks and exit 6, and emits the default-stickiness `--loud` warning. `main.go`: the `variant` command with `--format` and `--prefix` plus every `check` option, sharing their definitions, validation and `countedValue`; options before the command rejected for both commands; the three help headings and plain `--help` and `--version`; the raw-argument scan for `--format` and `--prefix` (reading option values as pflag does) that decides the fallback's format and prefix for every exit 1 to 5 and recovered panics; empty stdout where the spec says so. Unit tests against fake Unleash servers for every row of spec 0003's test plan that needs no real Unleash, and the root-command tests for the CLI rules. `docs/agents/internals.md`.
- Milestone criteria: help, shared options, exit 6, unknown versus disabled, fallbacks, command errors, the scan, metrics, stickiness warning.

### PR 3: fixtures, integration scenarios and docs

- [ ] Branch `m6/scenarios`, stacked on `m6/command`.
- Contents: `unleash/fixtures.json` and `unleash/seed.py` gain flags with `json`, `string`, `number` and `csv` payloads, a weighted split, custom stickiness and a disabled flag with variants, through the Unleash 8.3.1 Admin API the spike confirms; `TestClientTokensSeeTheFixtureFlags` checks their variants; `integration.CreateFlag` can create scenario flags with variants; integration scenarios for every M6 criterion that needs Unleash (stickiness with `--user-id` and a custom property, weighted split, each payload type, disabled flag exit 6, fallbacks, metrics with the variant count); the README's gated `$GITHUB_ENV` example and stickiness guidance; `docs/agents/internals.md`.
- Milestone criteria: fixtures, integration coverage, README.

### Release: v0.2.0

- [ ] After PRs 1 to 3 are merged and main's CI is green, with the user's go, Claude tags `v0.2.0`. The workflow passes every step; spec 0002's commands verify it; the release notes list the M6 PRs under Features.

### Close out

- [ ] Every M6 exit criterion holds. Set M6 to `done`, spec 0003 to `implemented`, and update this plan, internals.md and the README status.

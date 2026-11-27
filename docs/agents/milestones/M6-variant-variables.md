---
status: planned  # planned | active | done | dropped
---

# M6: Variant variables

## Goal

Pipelines can run `releash variant <flag>` after `releash check` and get the flag's variant and payload as environment variables, with nested JSON payloads flattened into one variable per leaf. Spec 0003 is fully implemented.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- [ ] [0003-variant-variables](../../specs/0003-variant-variables.md)

## Exit criteria

- Spec 0003 and ADRs 0006 and 0007 are `accepted` before implementation starts, after their rework against the code that M3 to M5 built.
- `releash variant --help` lists the releash options (`--url`, `--token`, `--timeout`, `--loud`, `--format`, `--prefix`), the context options of spec 0001 and the other options (`--help`, `--version`) under the same headings as `check`, with `--help` and `--version` printed plainly as for `check`. Both commands share one implementation of the connection and context options, their validation and `countedValue` repeat counting, and an option given before `variant` exits 2 as it does before `check`.
- A `json` payload prints `ENABLED`, `VARIANT`, `PAYLOAD` and one `PAYLOAD__<path>` variable per leaf, in the order spec 0003 defines, with the default prefix derived from the flag name.
- `string`, `number` and `csv` payloads print only `PAYLOAD`, and a variant without a payload prints only `ENABLED` and `VARIANT`.
- An enabled flag without variants prints `ENABLED=true` and `VARIANT=disabled`, exits 0 and writes nothing to stderr.
- A disabled flag prints `ENABLED=false` and `VARIANT=disabled`, writes an error naming `releash check` to stderr, and exits 6. Combined with a command line error, the lower code wins.
- Exit 6 behaves as ADR 0004 and ADR 0007 describe, and the exit code constants include it.
- Unit tests cover the flattening rules in spec 0003: nesting, arrays, scalars, `null`, empty containers, preserved number text, name normalisation, and every kind of collision.
- `--format shell` output survives `eval` unchanged for values containing `'`, `$`, `` ` ``, `\`, spaces and newlines.
- Each fallback of spec 0001, invalid JSON in a payload, a collision and an unrepresentable value prints the disabled variant, exits 0, is silent without `--loud`, and has its own `--loud` line.
- Each command and configuration error of spec 0001 prints the fallback in the requested format with the spec's exit code, and an invalid `--format` or `--prefix` exits 2. The format and the prefix for stdout come from a scan of the raw arguments that reads option values as the parser does, so a parse error before or after them still prints the fallback, and an invalid or ambiguous format or prefix, or no flag name to derive a prefix from, prints nothing. A recovered panic prints the fallback and exits 1.
- With `--user-id` fixed, repeated runs get the same variant, a custom stickiness property gives each value a fixed variant, and `--loud` warns when the flag uses default stickiness and neither `--user-id` nor `--session-id` is set.
- An unknown flag falls back with exit 0 and the not-found `--loud` line, while a disabled flag exits 6: releash tells them apart from the fetched flags, because the SDK returns the same variant for both.
- `variant` uses the evaluation path `check` uses (`internal/releash/evaluate.go`), not a copy: memory-only storage, the `--timeout` bound and register deadline, readiness and error sorting, fallbacks, the automatic `remoteAddress`, and the metrics wait. A successful evaluation sends usage metrics, including the variant count, before exit, and a fallback or exit 6 does not wait for them.
- `unleash/fixtures.json` and `unleash/seed.py` gain flags with `json`, `string`, `number` and `csv` payloads, a weighted split, custom stickiness and a disabled flag with variants, and `TestClientTokensSeeTheFixtureFlags` checks their variants. `integration.CreateFlag` can create scenario flags with variants for cases the fixtures cannot hold.
- Integration tests cover every criterion above that needs Unleash, locally and in CI.
- The README has a pipeline example that gates with `check` and loads the variant into `$GITHUB_ENV`, and documents passing the repository as `--user-id` or a custom property for stickiness, with the variables for each CI system listed in spec 0003.
- `variant` is released in `v0.2.0` through the release workflow, which passes every step, and `v0.2.0` verifies with the commands in spec 0002. Its pull requests carry the `enhancement` label, so the release notes list it under Features.
- Spec 0003 moves to `implemented`.

## Out of scope

- Several flags in one call, selecting part of a payload, and the `github` and `json` formats (future possibilities in spec 0003).
- Changes to `releash check`'s output or exit codes.

## Risks / open questions

- M5 has shipped, so `variant` arrives in the minor release `v0.2.0`, and its variable names become public contract from that release.
- Variable naming is the hardest part to change later. Pipelines will hard-code names such as `RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG`, so the rules in ADR 0006 must be implemented exactly, and any change to them is a breaking change.
- Reading the v6.5.1 SDK settled two points: `GetVariant` returns the same `disabled` variant for an unknown and a disabled flag, and calls `VariantFallbackFunc` for both, so releash must check the fetched flags itself; and it counts the variant in usage metrics. A short spike against Unleash 8.3.1 should still confirm strategy variants, every payload type as the Client API returns it, and the stickiness fields the default-stickiness warning reads.
- `unleash/seed.py` already compares strategy variants. Feature-level variants, if the fixtures use them, need the per-environment variants endpoint of the Unleash 8.3.1 Admin API and more seed code, and `integration.CreateFlag` needs the same.

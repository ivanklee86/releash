# Plan: variant variables

Started 2026-11-27. Status: in progress.

## Resume here

Updated 2026-11-27. The spec was first written after M2, before M3 to M5 built the `check` command, evaluation and releases. It has been reworked against that code (see [Rework after M5](#rework-after-m5)), so spec [0003](../../specs/0003-variant-variables.md) is back to `draft` and ADRs [0006](../../adrs/0006-variant-variable-names.md) and [0007](../../adrs/0007-refuse-variant-of-disabled-flag.md) to `proposed`, for the user to re-accept. ADR 0004 lists exit 6. Milestone [M6](../milestones/M6-variant-variables.md) is `planned`. Next: the user reviews the rework; once accepted, set spec 0003 to `accepted` and the ADRs to `accepted`, then start M6 with the spike against Unleash 8.3.1 and an M6 implementation plan.

## Goal

Let pipelines set environment variables from an Unleash flag's variant and payload, so experiments and configuration in Unleash can drive CI/CD steps. Users gate with `releash check` and then load the variant.

## What we know

- The request: a new experiment-like command, run after `check`, that exports the JSON payload as environment variables in a format that handles nested JSON.
- `check`'s stdout contract (`true` or `false` only) cannot carry variables, so this is a separate command.
- Unleash variants have a name, a payload type (`string`, `json`, `csv`, `number`) and a string value. A disabled flag, or one without variants, gives the variant `disabled`.
- Variants are assigned by stickiness, by default `userId`, then `sessionId`, then random. Pipelines usually set neither, so runs are not sticky unless they pass something.

## Proposed in the draft

- Variables `<prefix>ENABLED`, `<prefix>VARIANT`, `<prefix>PAYLOAD` (raw or compact JSON) and `<prefix>PAYLOAD__<path>` per JSON leaf.
- Default prefix `RELEASH_<FLAG>_`, overridable with `--prefix`, which must match `^[A-Z][A-Z0-9_]*$`.
- Nesting with `__`, array indices, names upper-cased with other characters replaced by `_`.
- Collisions, invalid JSON and values the format cannot represent fall back to the disabled variant and exit 0, like spec 0001's `false`.
- Formats `dotenv` (default), `shell` and `json`.
- stdout is always the variant output in the requested format, except for an invalid `--format`, which exits 2 with empty stdout.

## Decisions from 2026-11-27

- The command is `releash variant`.
- A disabled flag is refused: stdout has the disabled variant, stderr names `releash check`, and the exit code is a new 6. Runtime fallbacks still exit 0. Exit 6 ranks after 1–5 and applies only to `variant`.
- JSON `null` becomes an empty variable.
- No `__LENGTH` variable for arrays.
- releash does not read CI-specific variables. For stickiness, the spec recommends passing the repository name as `--user-id`, or a custom property with matching stickiness in Unleash, and lists the variable for each common CI system. `variant` takes the same context options as `check`, including `--properties`.
- The `github` format is deferred to a future possibility, because multi-line string payloads should be rare and `--format shell` carries them.
- `--prefix` has no denylist, because the payload controls only names after `<prefix>PAYLOAD__` (ADR 0006).

## Open questions

None.

## Next steps

- The user re-accepts spec 0003 and ADRs 0006 and 0007, or asks for changes.
- Spike `GetVariant` against Unleash 8.3.1: strategy variants, every payload type as the Client API returns it, and the stickiness fields. Reading the SDK already settled the unknown-versus-disabled question and the variant metrics (see below).
- Write the M6 implementation plan. M6 lands after M5, as the minor release `v0.2.0`.

## Rework after M5

Done 2026-11-27, at the user's request, against `main` after M5. The design (the command, the variable names, the formats, exit 6) is unchanged; the rework aligns the spec with what the code and the SDK now do.

- `variant` runs through the evaluation `check` uses, in `internal/releash/evaluate.go`: memory-only storage, the `--timeout` bound and register deadline, readiness and error sorting, every fallback and its `--loud` line, the automatic `remoteAddress`, and the usage-metrics wait. A fallback and exit 6 skip the metrics wait.
- The v6.5.1 SDK's `GetVariant` returns the same `disabled` variant for an unknown and a disabled flag, and calls `VariantFallbackFunc` for both. releash looks the flag up in the fetched flags (`ListFeatures`): unknown is the not-found fallback with exit 0, disabled exits 6. ADR 0007 says so.
- A flag the SDK cannot evaluate falls back with exit 0, as for `check`.
- The default-stickiness warning reads the `stickiness` of the fetched flag's feature variants and strategy variants.
- Decision, the user, 2026-11-27: a command line that fails to parse can stop before `--format` is read, so the format for stdout comes from a scan of the raw arguments, as releash already does to find options before the command. No `--format` means `dotenv`; every `--format` naming the same valid value means that format; an invalid value, a trailing `--format` or two different values mean empty stdout with exit 2. Arguments after `--` are not scanned.
- A recovered panic prints the fallback in that format and exits 1, as `check` prints `false`. An option before `variant` exits 2, as before `check`. `--help` uses the same three headings as `check`.
- `variant` ships in the minor release `v0.2.0` under spec 0002, with the `enhancement` label, and its variable names are public contract from then. M6 now ends with that release.
- Fixtures: `seed.py` and `integration.CreateFlag` need feature variants through the Unleash 8.3.1 Admin API.
- Decision, the user, 2026-11-27: the reworked spec and ADRs go back to `draft` and `proposed` until the user re-accepts them.
- Decision, the user, 2026-11-27: pipelines want environment variables, so `--format json` is dropped from the first version and listed under spec 0003's future possibilities. `--format` keeps `dotenv` (the default) and `shell`, which loads the variables into a shell script safely, e.g. where a later step sources a file instead of reading a CI env file.

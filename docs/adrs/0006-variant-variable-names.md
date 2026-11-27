---
status: proposed
date: 2026-11-27
decision-makers: Ivan Lee
---

# Name variant variables with a prefix, a fixed `PAYLOAD__` segment and `__` for nesting

## Context and Problem Statement

`releash variant` turns a flag's variant and payload into environment variables (see [spec 0003](../specs/0003-variant-variables.md)). Payloads are often nested JSON, but environment variable names are flat and portable names are limited to `[A-Z0-9_]`. Pipelines will hard-code the names releash produces, so the scheme is public contract from the first release. How should a payload path become a variable name, and what happens when two paths map to the same name?

## Decision Drivers

* A person should be able to predict a variable name from the payload without running releash.
* Anyone who can edit a flag in Unleash must not be able to set arbitrary variables, such as `PATH` or `LD_PRELOAD`, for later pipeline steps.
* Two flags loaded in the same pipeline must not overwrite each other's variables.
* A payload that cannot be represented must not be applied partially.

## Considered Options

* Prefix, fixed `PAYLOAD__` segment, `__` for nesting, array indices, normalised names, fall back on collisions.
* The same, but with a single `_` for nesting.
* Only the whole payload as one JSON variable.
* No prefix: payload keys become variable names directly.

## Decision Outcome

Chosen option: "Prefix, fixed `PAYLOAD__` segment, `__` for nesting, array indices, normalised names, fall back on collisions", because it is the only option that is predictable, keeps payloads out of other variables' names and handles any JSON shape.

* Every name is `<prefix>ENABLED`, `<prefix>VARIANT`, `<prefix>PAYLOAD` or `<prefix>PAYLOAD__<path>`. The default prefix is `RELEASH_<FLAG>_`, and `--prefix` must match `^[A-Z][A-Z0-9_]*$`.
* Object members add `__<NAME>` and array elements add `__<index>`. Names are upper-cased and every character outside `[A-Z0-9_]` becomes `_`.
* `null` becomes an empty value. Arrays get no length variable; `<prefix>PAYLOAD` holds the whole payload as compact JSON for tools that need it.
* Two paths that normalise to the same name, or duplicate member names, are a collision. A collision falls back to the disabled variant and exits 0, like the other runtime fallbacks in [ADR 0003](0003-fail-safe-false.md).
* `--prefix` is not checked against a denylist, because the payload controls only the part of a name after `<prefix>PAYLOAD__`.

### Consequences

* Good, because the scheme matches ASP.NET Core configuration and pydantic-settings, which many engineers already know.
* Good, because the fixed `PAYLOAD__` segment confines everything a flag editor controls to one namespace, so no denylist is needed.
* Good, because a collision sends the pipeline to its defaults instead of to a configuration nobody wrote.
* Bad, because keys that differ only in case collide, and camelCase keys run together (`imageTag` becomes `IMAGETAG`). Payloads meant for pipelines should use snake_case or kebab-case keys.
* Bad, because a key that contains `__` and collides with nothing still looks nested.

### Confirmation

Unit tests cover each flattening and normalisation rule and every kind of collision in spec 0003, and integration tests check the variables for the fixture flags with `json` payloads.

## Pros and Cons of the Options

### Prefix, `PAYLOAD__`, `__` nesting, fall back on collisions

* Good, because it satisfies every decision driver.
* Bad, because names get long, e.g. `RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG`. `--prefix` shortens the start.

### Single `_` for nesting

* Good, because names are shorter.
* Bad, because `image_tag` and `image.tag` become indistinguishable from `{"image": {"tag": ...}}`, so collisions are common and names are not predictable.

### Only the whole payload as JSON

* Good, because there is nothing to normalise and no collisions.
* Bad, because every consumer needs `jq` or another JSON parser, which defeats the point of exporting variables.

### No prefix

* Good, because names are as short as possible.
* Bad, because a payload key `path` or `ld_preload` would set `PATH` or `LD_PRELOAD`, letting a flag editor run code in the pipeline, and two flags would overwrite each other's variables.

## More Information

See [spec 0003](../specs/0003-variant-variables.md), "Variables" and "Flattening".

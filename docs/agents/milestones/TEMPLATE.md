---
status: planned  # planned | active | done | dropped
---

# M\<N\>: \<Title\>

## Goal

One or two sentences: what is true for users when this milestone is done.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- [ ] [NNNN-\<topic\>](../../specs/NNNN-<topic>.md)

## Exit criteria

Observable, checkable conditions that must all hold before `status` moves to `done` (e.g. "`releash check` prints `false` when Unleash is unreachable").

- ...

## Complexity budget

The most this milestone may add, stated before work starts. Going over it means revisiting the scope, not just the estimate.

- New packages: ...
- New exported identifiers: ...
- New direct dependencies in `go.mod`: ...
- Product (non-test) Go lines added, estimated: ...

No function may exceed the `gocognit` threshold in `.golangci.yml`. Raising the threshold needs an ADR.

## Out of scope

Explicit no-gos, deferred to a later milestone or dropped entirely.

- ...

## Risks / open questions

- ...

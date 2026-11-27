# Internals

How the releash repository is organised. Keep this up to date as the repository changes.

## Status

The repository holds planning documents only. There is no code yet. M1 ([milestone](milestones/M1-skeleton-mvp.md), [plan](plans/2026-11-27-m1-skeleton-mvp.md)) adds the devcontainer, the Go module and CI, and extends this document with the code layout, tasks and CI.

## Documentation layout

| Path | What it holds |
|---|---|
| `AGENTS.md` | Instructions for coding agents and the repository's conventions |
| `docs/specs/` | Specifications in the Rust RFC format, named `NNNN-<topic>.md`. [0001](../specs/0001-rollout-decisions.md) covers the `check` command, and [0002](../specs/0002-distribution.md) covers distribution and versioning. |
| `docs/adrs/` | Architecture decisions in MADR 4.0.0 format, named `NNNN-<title>.md` |
| `docs/agents/milestones/` | Milestones M1–M5 from [`TEMPLATE.md`](milestones/TEMPLATE.md). Each lists the specs it delivers and its exit criteria. |
| `docs/agents/plans/` | Working plans, named `YYYY-MM-DD-<topic>.md`. Each has a "Resume here" section for the next session. |

## Planned code layout

From M1 on, following argonap's CLI pattern:

- `main.go` at the module root holds only the cobra and viper wiring.
- `internal/releash` holds the orchestrator, which does everything the CLI does and returns exit codes. Tests drive it directly.

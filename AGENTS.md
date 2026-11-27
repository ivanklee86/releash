# AGENTS

## Links
- [Unleash Go SDK](https://docs.getunleash.io/sdks/go)

## Documentation

- Specifications should be defined in `docs/specs` folder, following the [Rust RFC template](https://github.com/rust-lang/rfcs/blob/master/0000-template.md) and named `NNNN-<topic>.md` (e.g. `0001-multi-source-applications.md`).
- Milestones should be defined in `docs/agents/milestones`, following [`TEMPLATE.md`](docs/agents/milestones/TEMPLATE.md) and named `M<N>-<topic>.md` (e.g. `M1-multi-source-applications.md`). A milestone is `done` only when every exit criterion holds.
- Decisions should be recorded in [MADR 4.0.0](https://adr.github.io/madr/) format in `docs/adrs`.
- Plans should be stored in `docs/agents/plans`, named `YYYY-MM-DD-<topic>.md` with the date the plan was started (e.g. `2026-11-26-multi-source-applications.md`).
- Keep `docs/agents/internals.md` up to date as repository changes.
- Let's use full-length lines when writing docs.  Documentation should be human-focused and written with technical engineers in mind.

## Coding standards

- Versions should always be pinned.

### Go

- Follow the [Google Go Style Guide](https://google.github.io/styleguide/go/) (Guide, Decisions, and Best Practices).
- Where the Google guide is silent or ambiguous, especially on concurrency and error handling, use the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) as the tie-breaker.
- Structure the module per [Organizing a Go module](https://go.dev/doc/modules/layout)
- Enforce style with [`golangci-lint`](https://golangci-lint.run/) (including `gofmt`/`goimports`, `go vet`, and `staticcheck`), with its version pinned.

### Comments

Write comments for an engineer reading the code for the first time. Be brief and plain, as a careful colleague would write them, not a generated summary.

- Document every exported identifier. Follow [Go Doc Comments](https://go.dev/doc/comment): the first sentence starts with the name and says what it does (`// Sync applies the desired state to the cluster.`).
- Document the contract, not the implementation: what the caller must provide, what they get back, which errors are possible and when, side effects, and whether it is safe for concurrent use. Leave out anything the signature already makes obvious.
- Inside a function, comment only when the *why* is not obvious: a workaround, an invariant, a non-obvious ordering, a link to an issue. Never narrate what the next line does.
- One or two sentences is usually enough. If a function needs a long comment to explain, consider simplifying the function instead.
- Avoid filler and hedging: "This function is responsible for...", "Helper that...", "Note that...", "basically", "simply", "in order to", "ensures that".
- Don't list every parameter in prose, repeat type information, or use decorative formatting (headings, bold, emoji, dividers) in code comments.
- Don't write history or chat into comments ("Updated to handle...", "Fixed bug where...", "As requested..."). That belongs in commit messages and PRs.
- Keep comments true: when code changes, update or delete the comments that describe it.

```go
// Bad: wordy, restates the signature, describes the implementation.

// SyncApplication is a function that is responsible for synchronizing the given
// application. It takes a context and an application as parameters, iterates
// over each of the sources, and applies them. It returns an error if something
// goes wrong.
func SyncApplication(ctx context.Context, app *Application) error

// Good: states the behavior and the contract a caller needs.

// SyncApplication applies every source of app in order and stops at the first
// failure. Sources applied before the failure are not rolled back. It returns
// ErrNotFound if app has been deleted.
func SyncApplication(ctx context.Context, app *Application) error
```

### Testing

#### Philosophy

- Write tests that verify the *observable behavior* of this function from a caller's perspective. Each test should answer: "if this test fails, what promise to callers has been broken?"
- When designing tests:
  - First, list the test cases you plan on covering (happy path, edge cases, error handling).
  - Then write the tests.
  - Flag any behavior in the code that seems untestable or buggy rather than writing tests that enshrine it.

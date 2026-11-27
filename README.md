# releash
A small CLI to drive CI/CD changes via Unleash feature flags

## Usage

releash is in early development. It prints help and its version, and the `check` command arrives in a later milestone (see [`docs/agents/milestones`](docs/agents/milestones)).

```sh
go install github.com/ivanklee86/releash@latest

releash --help     # general help, exit 0
releash --version  # installed version, exit 0
```

An unknown command or option prints an error to stderr and exits 2. Exit codes are listed in [ADR 0004](docs/adrs/0004-exit-code-contract.md).

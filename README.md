# releash

A small CLI to drive CI/CD changes via Unleash feature flags

## Usage

releash is in early development. `releash check` accepts and validates every option and follows the output and exit code contract, but it does not talk to Unleash yet: every valid invocation prints `false`, which is already its fail-safe answer. Flag evaluation arrives in M4 (see [`docs/agents/milestones`](docs/agents/milestones)).

Install a pinned version. There are no releases before v0.1.0, so until then pin a commit SHA.

```sh
go install github.com/ivanklee86/releash@<version>

releash --help     # general help, exit 0
releash --version  # installed version, exit 0
```

Point releash at Unleash with a backend (client) API token, then check a flag and gate a step on it:

```sh
export UNLEASH_URL=https://unleash.example.com/api
export UNLEASH_API_TOKEN=default:production.abc123

if [ "$(releash check deploy-payments)" = "true" ]; then
  ./deploy.sh
fi
```

```text
releash check <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                     [--env <name>] [--app-name <name>]
                     [--user-id <id>] [--session-id <id>]
                     [--remote-address <ip>] [--current-time <timestamp>]
                     [--properties <name>=<value>]...
```

- Releash options: `--url` and `--token` fall back to `UNLEASH_URL` and `UNLEASH_API_TOKEN`, and an option wins over its environment variable. Prefer the environment variable for the token, because options show in process listings and CI logs. `--timeout` is a positive Go duration (default `5s`). `--loud` explains on stderr why the answer is `false`.
- Context options set the Unleash context: `--env` (default `production`), `--app-name` (default `releash`), `--user-id`, `--session-id`, `--remote-address` (an IPv4 or IPv6 address), `--current-time` (an RFC 3339 timestamp), and `--properties name=value`, repeated once per custom property. A property value may contain `=` or be empty.

`releash check` always prints exactly `true` or `false` on stdout, apart from `--help` and `--version`. Options go after `check <flag>`: `releash --loud check deploy-payments` is a bad command line (exit 2). It exits 0 for every answer, so `set -e` is safe. A mistake in the command or its configuration still prints `false`, writes an error to stderr and exits non-zero:

| Exit | Meaning |
|---|---|
| 0 | Answer |
| 1 | Internal error in releash |
| 2 | Bad command line: unknown option, missing or extra arguments, an invalid `--timeout`, a releash option given twice |
| 3 | No Unleash URL or API token, or an empty one |
| 4 | Invalid context value, property name or Unleash URL |
| 5 | A context option or property given twice |

When there are several mistakes, the lowest code wins. Other invocations, such as bare `releash` or an unknown command, print help or an error and are not meant for scripts; an unknown command or option there exits 2. See [spec 0001](docs/specs/0001-rollout-decisions.md) and [ADR 0004](docs/adrs/0004-exit-code-contract.md) for the full contract.

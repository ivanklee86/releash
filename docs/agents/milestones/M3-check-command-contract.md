---
status: planned  # planned | active | done | dropped
---

# M3: `check` command contract

## Goal

`releash check <flag>` accepts every option in spec 0001, validates them, and honours the output and exit code contract, without talking to Unleash yet. Every valid invocation prints `false` and exits 0, which is already the spec's fail-safe answer, so pipelines can adopt the command before evaluation lands.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- [ ] [0001-rollout-decisions](../../specs/0001-rollout-decisions.md), sections "Command", "Output and exit codes", "Command and configuration errors", "Evaluation context" (validation and defaults only) and "Configuration". The rest is M4.

## Exit criteria

- The synopsis and `releash check --help` list releash options (`--url`, `--token`, `--timeout`, `--loud`) and context options (`--env`, `--app-name`, `--user-id`, `--session-id`, `--remote-address`, `--current-time`, `--properties`) as two groups, in that order.
- `--url` and `--token` fall back to `UNLEASH_URL` and `UNLEASH_API_TOKEN` through viper, and options take precedence over environment variables.
- Apart from `--help` and `--version`, stdout is exactly `true\n` or `false\n` for every exit code. In M3 it is always `false\n`.
- Each exit code in spec 0001 has a test asserting stdout, stderr and the code:
  - 2: missing flag name, extra arguments, unknown option, an invalid `--timeout` (`abc`, `0`, `-1s`), a releash option given twice
  - 3: missing URL, missing token, an empty URL or token from an option or environment variable
  - 4: a malformed `--url`, an invalid `--current-time` or `--remote-address`, empty `--env` or `--app-name`, a property without `=`, an empty property name, a property named after a standard field (with the hint)
  - 5: a context option given twice, a repeated property name
- A valid invocation prints `false` and exits 0. With `--loud`, it writes one line to stderr saying evaluation is not implemented yet.
- Defaults apply when options are absent (`environment=production`, `appName=releash`, `--timeout 5s`), and a default never counts as a duplicate.
- Property values containing `=` are preserved, and empty values are allowed.
- A panic on releash's own goroutine prints `false` and exits 1.
- Errors for exit codes 1–5 go to stderr with or without `--loud`.

## Out of scope

- Fetching or evaluating flags, usage metrics, and detecting `remoteAddress` automatically (M4).
- Releases (M5).

## Risks / open questions

- cobra exits 1 for flag errors by default and prints usage on every error. releash must map these to exit 2, print `false` on stdout, and keep stderr to a short message.
- The "evaluation not implemented" `--loud` message is temporary. It must be removed in M4 so it does not become part of the contract.

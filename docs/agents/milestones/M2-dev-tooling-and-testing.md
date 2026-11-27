---
status: active  # planned | active | done | dropped
---

# M2: Dev tooling and testing

## Goal

A local Unleash with tokens and fixture flags starts with one command, both for humans trying releash by hand and for integration tests. Every later milestone can be built and verified with one command, locally and in CI, using unit tests and user-facing integration tests that mimic how pipelines use releash.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- None. M2 provides the tooling that M3 and M4 use to deliver and verify [0001-rollout-decisions](../../specs/0001-rollout-decisions.md).

## Exit criteria

### Local Unleash

- `docker-compose.yaml` at the repository root runs Postgres and the Unleash server, both at pinned image versions, with Unleash on `localhost:4242`.
- API tokens exist as soon as the stack is up, created by the server's `INIT_ADMIN_API_TOKENS` and `INIT_CLIENT_API_TOKENS` settings: one admin token, and one client token for each of the `development` and `production` environments. The tokens are fixed, dev-only values committed to the repository and documented as not secret.
- Fixture flags exist as soon as the stack is up, created by a one-shot seed service in the compose file. It waits for Unleash to be healthy and creates flags through the Admin API. Seeding is idempotent, so restarting the stack or re-running the seed doesn't fail or duplicate anything.
- Fixture flags are defined in one file next to the seed script and named for what they test. At minimum:
  - an enabled flag and a disabled flag
  - a flag constrained on a custom property (e.g. `region IN eu`)
  - a flag targeted by `userId`
  - flags with date constraints in the past and in the future
  - a flag targeted by IP address, covering the runner's address and the Docker network
  - a flag enabled in `development` and disabled in `production`, to show that the token picks the environment
  - a flag the client tokens cannot see, in a second project. Open-source Unleash may support only the `default` project, so check this against the pinned image first. If it can't be done, drop the fixture and the matching test, and note in spec 0001 that this fallback is covered only by the unknown-flag case.
- `task services` exits 0 only once Unleash is healthy and the seed has completed successfully. `docker compose up --wait` treats an exited one-shot service as a failure and does not wait for it to finish, so the task waits on a service that `depends_on` the seed with `condition: service_completed_successfully` (or runs the seed with `docker compose run`). Check this against the pinned Compose version.

### Tasks

- The root Taskfile from M1 also includes `tasks/unleash.yaml`.
- `task services` starts the stack and waits for it to be seeded. `task services:down` stops it and removes its volumes.
- `task releash -- <args>` builds releash and runs it against the compose stack, with `UNLEASH_URL` and the production client token already set, e.g. `task releash -- check deploy-payments --loud`. The same environment is available to humans through a committed `.env.example` for running `bin/releash` directly.
- `task go:test` runs unit tests only. `task go:test-integration` runs integration tests against the running stack, and `task go:test-ci` runs both with a JUnit report and a coverage profile, as argonap's does.

### Tests

- Unit tests (`go test ./...`) need neither Docker nor network access.
- Integration tests are behind the `integration` build tag and run on the host (the devcontainer or a CI runner) against the compose stack. They read `UNLEASH_URL` and the tokens from the environment, defaulting to the compose values, and fail with a message naming `task services` when Unleash cannot be reached.
- Integration tests drive the `internal/releash` orchestrator directly through `NewWithConfig`, with buffers for `Out` and `Err`, and assert stdout, stderr and the returned exit code. A small set of cobra-level tests goes through `main`'s root command to check that options and environment variables reach the config.
- Integration tests are written as user-facing scenarios named after pipeline usage, e.g. "deploy gate opens for an enabled flag", "gate stays closed when the token is rejected", "region-gated rollout opens only for eu". M2 adds the scenario list as test stubs, plus helpers for a fixture-flag catalog, client and admin tokens, and a fake HTTP server that accepts connections and never responds. M3 and M4 fill the scenarios in.
- M2 itself has one passing integration test: a smoke test that fetches the fixture flags through the Client API with each client token and checks that the expected flags are visible.

### Renovate

- `renovate.json` follows argonap's: it extends `config:recommended`, runs on a weekly schedule, and waits `minimumReleaseAge` (5 days) before proposing a release.
- It keeps every pinned version in the repository current:
  - Go modules and the Go version in `go.mod`
  - Go tools installed with `go install <pkg>@<version>` in `tasks/go.yaml`, through a regex manager
  - Docker images in `docker-compose.yaml` (Unleash, Postgres, the seed image), pinned by digest
  - the devcontainer base image, pinned by digest in `.devcontainer/Dockerfile`. Devcontainer features are maintained in the shared gantry base (`ivanklee86/devcontainers`) and are not tracked here.
  - GitHub Actions, and tool versions in workflows and the `setup-task` action marked with `# <datasource>/<depName>` comments, as argonap does
  - pre-commit hooks in `.pre-commit-config.yaml`
- Updates are grouped as in argonap, without its `devcontainer` group: `go` (modules, Go version, base image), `ci` (actions, hooks, CI tools), and a new `unleash` group for the Unleash server image together with the Go SDK. The server image and the seed script are upgraded together (see Risks).
- Minor, patch, pin and digest updates automerge once CI passes. Major updates of Go modules and the base image need a human, and so do Go version updates of any size (the `go` directive must land with the matching base image tag) and Unleash server and SDK updates of any size, because the integration tests are the only guard against behaviour changes there.
- The Renovate config hook in prek validates `renovate.json`.

### Quality and CI

- prek hooks (whitespace, YAML, gofmt, markdownlint, Renovate config) pass on all files.
- M1's `ci.yaml` gains argonap's remaining CI jobs and steps:
  - a `pre-commit` job running the prek hooks through `j178/prek-action` at a pinned version
  - an `integration` job that starts the compose stack with `task services`, runs `task go:test-ci`, and dumps the compose logs on failure
  - test results published with `EnricoMi/publish-unit-test-result-action` and coverage reported with octocov (`.octocov.yml`), both SHA-pinned because they run with write permissions, as in argonap
  - the jobs pass on pull requests and on `main`.
- `docs/agents/internals.md` describes the compose stack, the fixture flags, the tasks, and how to run each kind of test.

## Out of scope

- The `check` command (M3) and Unleash evaluation (M4). The scenario stubs are filled in by those milestones.
- Running tests inside a container on the compose network. Tests run on the host only.
- Release tooling and a snapshot release in CI (M5).

## Risks / open questions

- The Renovate app must be enabled for `ivanklee86/releash` before Renovate can be confirmed working.
- The seed script uses the Unleash Admin API, which can change between server versions. The image pin and the seed script live side by side so they are upgraded together, and the smoke test catches drift.
- IP-targeted fixtures depend on the address the runner uses to reach Unleash, which differs between the devcontainer (Docker-in-Docker), a CI runner and a human's machine. The fixture should list the loopback address and the Docker bridge ranges, and the M4 test should assert against the address releash actually detects.
- The integration job needs Docker on the CI runner. GitHub-hosted Ubuntu runners have it, and the devcontainer has Docker-in-Docker.

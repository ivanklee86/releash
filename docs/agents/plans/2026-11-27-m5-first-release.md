# Plan: M5 first release

Started 2026-11-27. Status: in progress. Milestone: [M5](../milestones/M5-first-release.md).

## Resume here

Updated 2026-11-27. PR 1 (release build and CI) is built on `m5/release-build` and waiting for review; its decisions are under [Reference](#reference), and the ones marked "for review" need the user's confirmation. After it opens, the user adds `release-check` and `release-notes` to the `main` ruleset's required checks. PR 2 (release workflow and docs) follows, stacked on it. When resuming, start the first step in [Steps](#steps) that is not checked off and whose dependencies are done. Before ending a session, update this section and check off finished steps.

## How to work through this plan

- One PR per step. Read this plan, the M5 milestone, spec 0002, ADR 0005 and `AGENTS.md` first.
- Before handing off, run `task`, `prek run --all-files`, and `goreleaser release --snapshot --clean` (goreleaser 2.20.3 is installed locally, with Docker), then smoke-test the snapshot binary and both images with `--version`.
- argonap (`ivanklee86/argonap`, readable with `gh api repos/ivanklee86/argonap/contents/<path>`) is the reference for `.goreleaser.yaml`, `Dockerfile`, `release.yaml` and the `release-check` job. Spec 0002 adds signing, SBOMs, release notes and a versioning policy on top of it.
- Pin every version: actions by full commit SHA with a version comment, tools in workflows with a `# github-releases/<owner>/<repo>` comment that Renovate's existing rule reads, and the image base by digest.
- The user runs `/ship`. Claude never merges. Claude pushes a tag or creates a label only after the user says go in the moment.
- If a step uncovers a decision that is not covered by the specs, ADRs or milestone, stop and ask instead of choosing.

## Reference

Decisions:

- 2026-11-27, the user: the Homebrew tap token is a repository secret named `HOMEBREW_AUTH_TOKEN`, as in argonap. The user adds it to the releash repository before the first tag.
- 2026-11-27, the user: the release is rehearsed with `v0.1.0-rc.1` first. A prerelease publishes a GitHub prerelease and the `0.1.0-rc.1` image, but moves no `latest` tag and publishes no Homebrew cask, so it exercises signing, the image SBOM attestation and verification without committing to v0.1.0.
- 2026-11-27, the user: Claude creates the missing `breaking-change` and `dependencies` labels and pushes each tag, each only when the user says go. `bug` and `enhancement` already exist.
- goreleaser injects `{{ .Version }}` (no leading `v`, e.g. `0.1.0`), as argonap does and as spec 0002's archive and image names use. A `go install` build reports the module version, `v0.1.0`. internals.md and the README say so.

Decisions made while building PR 1, within the ones above. The ones marked "for review" were not covered by the spec, the ADR or the milestone, so the most conservative option was taken and the user should confirm it on the PR:

- A snapshot does not skip goreleaser's `signs` pipe: `goreleaser release --snapshot` sets only the publish, announce and validate skips, and without `--skip=sign` the snapshot fails at `cosign sign-blob` (confirmed locally with goreleaser 2.20.3). `docker_signs` runs in the publish phase, so a snapshot skips it. `task release:snapshot` therefore runs `goreleaser release --snapshot --clean --skip=sign`, and the `release-check` job runs that task, with goreleaser installed by `goreleaser-action` in `install-only` mode, so CI and a local snapshot use the same flags.
- The snapshot version is goreleaser's default `<last tag>-SNAPSHOT-<short commit>`, e.g. `0.0.0-SNAPSHOT-abea5a1` before the first tag. `task release:smoke-test` reads it from `dist/metadata.json`, finds the linux/amd64 binary in `dist/artifacts.json`, and expects exactly `releash version <version>` from the binary and from the `<version>-amd64` and `<version>-arm64` images that a snapshot builds.
- syft is installed by a new composite action, `.github/actions/setup-syft`, from its GitHub release after checking the release's `checksums.txt`, with the version pinned in the action's `version` default the same way `setup-task` pins Task. PR 2's release workflow reuses it. Renovate's regex manager for composite action defaults now covers every `.github/actions/*/action.yml`.
- goreleaser's version in workflows is a `GORELEASER_VERSION` job env value with a `# github-releases/goreleaser/goreleaser` comment, which Renovate's existing workflow regex reads. A bare `version:` input on `goreleaser-action` is not matched by it.
- The `.github/release.yml` check is a separate `release-notes` job. It converts the file to JSON with the runner's preinstalled `yq` (mikefarah yq 4.54.3 on ubuntu-26.04) and compares it with `jq` against the exact document the spec's table describes, so a parse error, a missing or extra category, a changed label or extra keys such as `exclude` all fail it. "Other changes" is the `*` wildcard category. For review: the runner's `yq` is not pinned by this repository; the check uses only `yq -o json '.'`, which has been stable across yq v4.
- The image's OCI `source` label is the fixed URL `https://github.com/ivanklee86/releash` rather than goreleaser's `{{ .GitURL }}`, which is the checkout's remote URL and can be an SSH URL locally. `licenses` is `MPL-2.0`, from `LICENSE`.
- For review: `dockers_v2` defaults to `sbom: true`, which makes buildx attach an unsigned BuildKit SBOM attestation to the image. It is set to `false`, so the only image SBOM is the signed `cosign attest --type spdxjson` attestation that spec 0002 describes and PR 2 adds. buildx's default provenance attestation is left as is.
- For review: argonap's `before.hooks: go mod tidy` is left out. It would run after goreleaser's dirty-tree check, so a release could build from a `go.mod` that differs from the tagged commit; the `lint` job already fails pull requests whose `go.mod` and `go.sum` are not tidy.
- The cask uses `skip_upload: auto`, so a prerelease tag writes the cask into `dist/` but does not push it to the tap. The tap token is read from `HOMEBREW_AUTH_TOKEN` in the environment; PR 2 sets it from the secret of the same name. A snapshot renders the cask without the variable set.
- The cask description is "Drive CI/CD changes with Unleash feature flags", the README tagline without the leading article and final period that `brew audit` rejects.
- `go install github.com/ivanklee86/releash@<commit>` through the module proxy reports the module version (`v0.0.0-20261127165219-abea5a1f7a81` for `main` at the time), which confirms the `go install` half of the `--version` criterion. The tagged `v0.1.0` case is checked at the release step.
- New required checks: `release-check` and `release-notes`. The `main` ruleset is set in GitHub, so the user adds them after PR 1's first CI run.

## Steps

### PR 1: release build and CI

- [x] Branch `m5/release-build`.
- Contents: M5 to `active`; this plan in `docs/agents/plans`; `.goreleaser.yaml` (the five platforms in spec 0002, archive names and contents, `CGO_ENABLED=0`, `-ldflags -X main.version`, SPDX SBOMs per archive with syft, `checksums.txt`, `signs` producing `checksums.txt.sigstore.json` with cosign keyless signing, `dockers_v2` multi-arch image with the tags and OCI labels in spec 0002, `docker_signs`, the Homebrew cask for non-prereleases with the quarantine hook, `changelog.use: github-native`, prerelease detection); a `Dockerfile` on `gcr.io/distroless/static-debian13:nonroot` pinned by digest; `.github/release.yml` with the categories and labels in spec 0002; `ci.yaml` gains `release-check` (snapshot release on every PR, the linux/amd64 binary and both image platforms smoke-tested with `--version`, each checked against the snapshot version) and a job, or a step in an existing job, that checks `.github/release.yml` parses and defines exactly the spec's categories and labels; the new required job names; Renovate gives its PRs the `dependencies` label and keeps the Dockerfile digest and the new tool pins current; a `task` target for the snapshot release; `docs/agents/internals.md`.
- A snapshot neither signs nor publishes, so signing config must be skipped for snapshots (goreleaser skips `signs` and `docker_signs` in snapshot mode; confirm it) and syft must be installed in the `release-check` job.
- Milestone criteria: goreleaser builds, base pinned by digest, `release-check`, release notes check, `--version` (already implemented in M1; confirm with the snapshot binary and with a `go install` build from a local path or module proxy if practical).

### PR 2: release workflow and docs

- [ ] Branch `m5/release-workflow`, stacked on `m5/release-build`.
- Contents: `.github/workflows/release.yaml` on `v*` tags with `contents: write`, `packages: write` and `id-token: write` only, checkout with full history, setup-go, QEMU and Buildx, ghcr.io login, cosign and syft installed at pinned versions, SHA-pinned `goreleaser-action` at goreleaser 2.20.3 with `GITHUB_TOKEN` and `HOMEBREW_AUTH_TOKEN`; then an explicit step that generates the image's SPDX SBOM and attaches it with `cosign attest --type spdxjson` to the published multi-arch digest; then a verification step that downloads `checksums.txt` and its bundle and runs `cosign verify-blob`, `cosign verify` and `cosign verify-attestation --type spdxjson` with the identities in spec 0002, failing the run if any fails. The README documents installation (binary with verification, image with verification, Homebrew, `go install`), and a pipeline example for at least GitHub Actions. `docs/agents/internals.md`.
- The workflow cannot run before a tag. `actionlint` (if available through the existing tooling, else a careful read against the goreleaser and cosign docs) and the rc rehearsal are its tests.
- Milestone criteria: tag publishing, image SBOM attestation, self-verification, CD shape, README.

### Labels and secret

- [ ] The user has added `HOMEBREW_AUTH_TOKEN` to the repository's Actions secrets.
- [ ] With the user's go, Claude creates `breaking-change` and `dependencies` with `gh label create`.

### Rehearsal: v0.1.0-rc.1

- [ ] After PRs 1 and 2 are merged, with the user's go, Claude tags `main` as `v0.1.0-rc.1` and pushes the tag. The release workflow must pass, including its own verification. Then Claude runs spec 0002's verification commands from a clean directory against the rc. Anything that fails gets a fix PR and `v0.1.0-rc.2`.

### Release: v0.1.0

- [ ] With the user's go, Claude tags `v0.1.0`. The workflow passes; spec 0002's commands verify the binary and the image; `brew install ivanklee86/tap/releash` and `go install github.com/ivanklee86/releash@v0.1.0` each give a working `releash --version`.

### Close out

- [ ] Every M5 exit criterion holds. Set M5 to `done`, spec 0002 to `implemented`, and update this plan's status, internals.md and the README status line.

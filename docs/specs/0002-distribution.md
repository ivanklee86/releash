- Feature Name: `distribution`
- Start Date: 2026-11-27
- Status: implemented
- Related ADRs: [0005](../adrs/0005-cosign-keyless-signing.md)

# Summary

releash is released from version tags as signed, static binaries for Linux, macOS and Windows, a multi-architecture container image on ghcr.io, and a Homebrew cask. Every release publishes SHA-256 checksums, SBOMs and Sigstore cosign keyless signatures, so a pipeline can verify that the binary it is about to trust with deploy decisions was built by this repository's release workflow. Versions follow SemVer, starting at v0.1.0; the first complete release is v0.1.2 (see [Versioning](#versioning)).

# Motivation

releash runs inside CD pipelines, often next to deploy credentials, and its output decides whether a deploy step runs. A pipeline should be able to install a specific version in one step, on whatever CI system and runner it uses, and check that the download is genuine and not just intact.

Goals:

- Install a pinned version of releash on any common CI runner: Linux, macOS or Windows, amd64 or arm64, or a container-native system.
- Verify a download cryptographically against this repository's release workflow, with no long-lived signing keys to manage.
- Make the public contract's stability explicit, so users know which upgrades can break their pipelines.

Non-goals:

- CI-specific integrations such as a GitHub Action (a non-goal in spec 0001).
- OS package managers other than Homebrew (apt, rpm, Scoop, winget). See [Future possibilities](#future-possibilities).
- macOS code signing and notarization.

# Guide-level explanation

Download a pinned release in a pipeline, verify it, and run it. Verifying needs cosign v3 or later, because releases are signed in cosign v3's Sigstore bundle format, which cosign v2 reads only with `--new-bundle-format`:

```sh
VERSION=0.1.2
BASE=https://github.com/ivanklee86/releash/releases/download/v${VERSION}

curl -sSfLO "${BASE}/releash_${VERSION}_linux_amd64.tar.gz"
curl -sSfLO "${BASE}/checksums.txt"
curl -sSfLO "${BASE}/checksums.txt.sigstore.json"

# Check that checksums.txt was signed by releash's release workflow for this tag...
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# ...and that the archive matches it.
sha256sum --check --ignore-missing checksums.txt

tar -xzf "releash_${VERSION}_linux_amd64.tar.gz" releash
./releash check deploy-payments
```

Or use the container image. Verify it first, then run the exact digest you verified, because a tag can move between the two commands:

```sh
VERSION=0.1.2
IMAGE=ghcr.io/ivanklee86/releash

DIGEST=$(docker buildx imagetools inspect "${IMAGE}:${VERSION}" --format '{{json .Manifest.Digest}}' | tr -d '"')

cosign verify "${IMAGE}@${DIGEST}" \
  --certificate-identity "https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

docker run --rm -e UNLEASH_URL -e UNLEASH_API_TOKEN \
  "${IMAGE}@${DIGEST}" check deploy-payments
```

Pipelines that pin the digest in their config only need to verify it once, when they update the pin.

On a workstation, install with Homebrew or Go:

```sh
brew install ivanklee86/tap/releash
go install github.com/ivanklee86/releash@v0.1.2
```

`releash --version` prints the installed version.

# Reference-level explanation

## Platforms

Binaries are built with `CGO_ENABLED=0`, so they are static and have no runtime dependencies.

| OS | Architectures | Archive |
|---|---|---|
| Linux | amd64, arm64 | `.tar.gz` |
| macOS (darwin) | amd64, arm64 | `.tar.gz` |
| Windows | amd64 | `.zip` |

Archives are named `releash_<version>_<os>_<arch>.<ext>`, where `<version>` has no leading `v`. Each contains the `releash` binary (`releash.exe` on Windows), `LICENSE` and `README.md`.

## Release artifacts

Each GitHub release contains:

- One archive per platform.
- `checksums.txt`: SHA-256 checksums of every archive and SBOM, in `sha256sum` format.
- `checksums.txt.sigstore.json`: a Sigstore bundle with the cosign keyless signature of `checksums.txt`, its signing certificate and its transparency log entry.
- One SPDX JSON SBOM per archive, named `<archive>.sbom.json`.

Signing `checksums.txt` covers every file it lists, so archives and SBOMs are not signed individually.

## Container image

- Repository: `ghcr.io/ivanklee86/releash`.
- Platforms: `linux/amd64` and `linux/arm64`, published as one multi-architecture manifest.
- Base: `gcr.io/distroless/static-debian13:nonroot`, pinned by digest in the `Dockerfile` and kept current by Renovate. It has CA certificates and runs as UID 65532. The entrypoint is `/usr/bin/releash`.
- Tags: `<version>` (e.g. `0.1.2`) for every release, and `latest` for non-prerelease versions. There are no floating `<major>` or `<major>.<minor>` tags. Pipelines should pin a version or a digest.
- Labels: OCI `source`, `version`, `revision` and `licenses`.
- The image is signed with cosign keyless signing, both the multi-arch index and each platform image in it. SPDX SBOMs are attached as signed attestations by an explicit `cosign attest --type spdxjson` step in the release workflow, because goreleaser's `sboms` covers only archives and `docker_signs` attaches no SBOM. Each platform image (`linux/amd64`, `linux/arm64`) carries an SBOM of its own contents, and the multi-arch index carries the `linux/amd64` SBOM, because an attestation has one subject. Users can check them with `cosign verify-attestation --type spdxjson`; to inspect the packages of an arm64 image, verify its platform digest.

## Homebrew

A cask named `releash` is published to `ivanklee86/homebrew-tap` for non-prerelease versions, using the existing tap token secret. The tap needs no other setup. The binaries are not notarized, so the cask removes the macOS quarantine attribute after install, as argonap's cask does.

## `go install`

`go install github.com/ivanklee86/releash@<version>` works because `main.go` is at the module root.

`releash --version` uses the first of these that is set: the version injected with `-ldflags` (release builds), the main module version from `runtime/debug.ReadBuildInfo` (`go install` builds report the module version, and `go build` in a git checkout reports a VCS pseudo-version such as `v0.0.0-20261127120000-abcdef123456+dirty`), and finally `dev` when build info has no version or reports `(devel)`.

## Versioning

- Releases are tagged `vMAJOR.MINOR.PATCH` following [SemVer 2.0.0](https://semver.org/spec/v2.0.0.html), starting at `v0.1.0`. `v0.1.0` was tagged but never published, because its release run rebuilt the rc it shared a commit with, and `v0.1.1` was published without its cask and image SBOM attestations, so `v0.1.2` is the first complete release. Prereleases use `-rc.N` (e.g. `v0.2.0-rc.1`). They are published as GitHub prereleases, do not move the `latest` image tag, and are not published to Homebrew.
- The public contract is: the meaning of each exit code, stdout and the exit code for inputs that are valid in the current release, command and option names and their meaning, defaults, environment variable names, and archive and image naming.
- Before `v1.0.0`, a breaking change to the public contract bumps the minor version and is listed under a "Breaking changes" heading in the release notes (see [Release notes](#release-notes)). From `v1.0.0`, a breaking change needs a new major version.
- Exit codes are additionally governed by [ADR 0004](../adrs/0004-exit-code-contract.md): existing codes never change meaning, in any version.
- New options, new exit codes for new kinds of error, and additional stderr output are not breaking. Rejecting an input that the current release accepts is breaking. stderr wording is not part of the contract.

## Release notes

Release notes are GitHub's automatically generated release notes, built from the pull requests merged since the previous tag, with a short "Highlights" section above them. goreleaser publishes the generated notes with `changelog.use: github-native`. The Highlights say what changed for users; they are drafted in the release pull request, reviewed there before the tag is pushed, and added to the GitHub release once it verifies.

`.github/release.yml` groups pull requests into categories by label:

| Category | Label |
|---|---|
| Breaking changes | `breaking-change` |
| Features | `enhancement` |
| Bug fixes | `bug` |
| Dependencies | `dependencies` |
| Other changes | any other label, or none |

A pull request that changes the public contract must carry `breaking-change`. Pull requests from Renovate get `dependencies`. Pull request titles are what appear in the notes, so they should describe the change for users.

## Release process

- Pushing a `v*` tag runs `.github/workflows/release.yaml`, which runs goreleaser at a pinned version.
- The workflow has `contents: write` for the GitHub release, `packages: write` for ghcr.io, and `id-token: write` for cosign keyless signing. The Homebrew tap token is a repository secret used only by this workflow.
- Signing identities are tied to the workflow file and the tag ref. Renaming `release.yaml` changes the identity users verify against and is a breaking change.
- The version is injected with `-ldflags "-X main.version=<version>"`.

## Test plan

- On every pull request, CI runs `goreleaser release --snapshot --clean`, which builds every archive, SBOM and both image platforms without publishing or signing.
- CI smoke-tests the linux/amd64 binary and both image platforms with `--version`, and checks that the reported version matches the snapshot version.
- CI builds the Windows and macOS binaries but does not run them. Their correctness relies on Go's cross-compilation and the platform-independent test suite.
- A snapshot release does not call GitHub's release-notes API, so it cannot validate `.github/release.yml`. CI checks the file statically instead: it must parse as YAML and define the categories and labels listed under [Release notes](#release-notes). CI does not call the release-notes API, because that needs `contents: write`, which pull requests from forks don't get, and because the generated notes leave out categories with no matching pull requests.
- After publishing, the release workflow downloads `checksums.txt` and its bundle, and runs `cosign verify-blob`, then `cosign verify` and `cosign verify-attestation --type spdxjson` on the multi-arch index and on each platform image, with the identities documented above. goreleaser has already published by then, so a failure does not hold the release back: it marks the workflow run as failed, so a release users cannot verify is noticed right away.

# Drawbacks

- Verification needs cosign on the runner, which is one more tool for users to install. Checksums alone are still published for users who skip it.
- macOS binaries are not notarized. Users downloading the archive directly must remove the quarantine attribute themselves.
- Keyless signing ties verification to the workflow path and tag format, so changing either breaks existing verification instructions.
- Windows and macOS binaries are built but not executed in CI.

# Rationale and alternatives

- **cosign keyless signing over GitHub artifact attestations.** Both use Sigstore without long-lived keys. cosign signatures and bundles are verifiable with standard Sigstore tooling outside GitHub, and image signatures are understood by admission controllers such as Kyverno and Sigstore's policy-controller. See [ADR 0005](../adrs/0005-cosign-keyless-signing.md).
- **Checksums only.** Simpler, and what argonap does, but it only detects corrupted downloads, not a tampered release. That is not enough for a tool in the deploy path.
- **An install script (`curl | sh`).** Convenient in CI, but it's one more artifact to secure and maintain, and verifying it needs the same tooling as verifying the binary. Deferred.
- **GitHub-generated release notes over goreleaser's commit-based changelog or hand-written notes.** Pull request titles and labels are already reviewed, so the notes cost nothing extra at release time, and labels give the "Breaking changes" section the versioning policy needs. Commit messages are noisier, and hand-written notes get skipped.
- **Floating image tags (`0`, `0.1`).** Convenient, but they move under pipelines silently, which conflicts with pinning a tool that gates deploys.

# Prior art

- argonap and tangle use the same goreleaser, distroless image and Homebrew tap setup. This spec adds signing, SBOMs and an explicit versioning policy.
- goreleaser's documented cosign keyless setup, used by many Go CLIs.

# Unresolved questions

None at this time.

# Future possibilities

- An install script that downloads, verifies and installs the right binary.
- Scoop or winget for Windows, and deb/rpm packages via goreleaser's nFPM integration.
- SLSA provenance, in addition to signatures and SBOMs.
- Windows and macOS smoke tests on their own runners.

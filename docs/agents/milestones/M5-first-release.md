---
status: done  # planned | active | done | dropped
---

# M5: First release

## Goal

Pipelines can install a released `releash` binary or container image at a pinned version and use it from any CI system.

## Scope

Specs (in `docs/specs`) delivered by this milestone. Check each off when it is implemented.

- [x] [0002-distribution](../../specs/0002-distribution.md)

## Exit criteria

- goreleaser, at a pinned version, builds the archives for every platform in spec 0002, SBOMs, `checksums.txt`, and the multi-architecture image.
- Pushing a `v*` tag publishes the GitHub release, the image to ghcr.io and the Homebrew cask, signed with cosign keyless signing.
- The image's SPDX SBOM is attached as a signed attestation by a `cosign attest` step.
- The release workflow verifies its own published signatures and the image SBOM attestation, and fails if any of them don't verify.
- CI checks on every pull request that `.github/release.yml` parses and defines the categories and labels in spec 0002.
- The release image base is pinned by digest.
- CD follows argonap: `.github/workflows/release.yaml` runs on `v*` tags with only the permissions it needs (`contents`, `packages`, and `id-token` for signing), sets up QEMU and Buildx for the arm64 image, and runs a SHA-pinned `goreleaser-action` at a pinned goreleaser version.
- `ci.yaml` gains argonap's `release-check` job: a snapshot release on every pull request, with the linux/amd64 binary and both image platforms smoke-tested with `--version`, so release breakage is caught before a tag exists.
- `releash --version` reports the released version, including for `go install` builds.
- Release notes are GitHub-generated, grouped by the labels in `.github/release.yml`, and the labels exist in the repository.
- `v0.1.2` is released and verifies with the commands in spec 0002. The milestone first named `v0.1.0`: that tag was never published, because its run rebuilt the rc on the same commit, and `v0.1.1` was published without its cask and image SBOM attestations. `v0.1.2` is the first release that published everything, and the M5 plan records both failures.
- The README documents installation, verification and a pipeline example for at least one CI system.

## Out of scope

- CI-specific integrations such as a GitHub Action (a non-goal in spec 0001).
- Package managers other than Homebrew, an install script, and SLSA provenance (future possibilities in spec 0002).

## Risks / open questions

- Releasing before real-world use may lock in exit codes or option names. ADR 0004 already treats exit codes as public, so option names are the main exposure.

---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# Sign releases with cosign keyless signing and publish SBOMs

## Context and Problem Statement

releash runs in CD pipelines, often with access to deploy credentials, and its output decides whether deploy steps run. A compromised binary could turn gates on or leak credentials. How should users be able to verify that a releash binary or image was built by this repository?

## Decision Drivers

* Verification must detect a tampered release, not only a corrupted download.
* No long-lived signing keys to store, rotate or leak.
* Verification should work outside GitHub, including in Kubernetes admission control for the container image.
* Release configuration should stay small and follow goreleaser's supported paths.

## Considered Options

* cosign keyless signing of the checksums file and the image, plus SPDX SBOMs.
* GitHub artifact attestations (SLSA build provenance via `actions/attest-build-provenance`).
* SHA-256 checksums only.

## Decision Outcome

Chosen option: "cosign keyless signing of the checksums file and the image, plus SPDX SBOMs", because it detects tampering, needs no keys, and is verifiable with standard Sigstore tooling and admission controllers anywhere.

### Consequences

* Good, because a signature proves an artifact came from `release.yaml` in this repository at a specific tag.
* Good, because SBOMs let users scan releash's dependencies, including the Unleash SDK, for known vulnerabilities.
* Bad, because users need cosign installed to verify.
* Bad, because the signing identity includes the workflow path, so renaming `release.yaml` breaks verification instructions and is treated as a breaking change.

### Confirmation

The release workflow verifies its own published signatures and the image SBOM attestation with `cosign verify-blob`, `cosign verify` and `cosign verify-attestation`, using the identities documented in spec 0002, and fails if any check fails.

## Pros and Cons of the Options

### cosign keyless signing plus SBOMs

* Good, because Sigstore bundles and image signatures are understood by Kyverno, Sigstore's policy-controller and other tools.
* Good, because goreleaser supports most of it directly: `signs` for the checksums, `docker_signs` for the image and `sboms` for the archives. The image SBOM attestation needs one extra `cosign attest` step in the release workflow.
* Neutral, because it does not by itself provide SLSA provenance. That can be added later.
* Bad, because it adds signing and SBOM configuration to the release.

### GitHub artifact attestations

* Good, because it gives SLSA build provenance with a single action step and is verified with `gh attestation verify`.
* Bad, because verification is centred on GitHub's tooling and API, which pipelines on other CI systems may not have.

### Checksums only

* Good, because it is the simplest option and matches argonap.
* Bad, because anyone who can replace a release asset can also replace `checksums.txt`, so tampering goes undetected.

## More Information

See [spec 0002](../specs/0002-distribution.md).

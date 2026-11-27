# releash

A small CLI to drive CI/CD changes via Unleash feature flags

releash is in early development. The first release, v0.1.0, is being prepared in M5 (see [`docs/agents/milestones`](docs/agents/milestones)); until it is published, install with `go install` at a commit SHA. The instructions below use v0.1.0.

## Install

releash runs in pipelines that decide whether to deploy, so pin an exact version and verify what you download. Every release is built by [`.github/workflows/release.yaml`](.github/workflows/release.yaml) and signed with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) keyless signing, so verifying needs cosign but no keys. The details are in [spec 0002](docs/specs/0002-distribution.md).

### Binary

Archives are published for Linux and macOS on amd64 and arm64 (`.tar.gz`) and for Windows on amd64 (`.zip`), named `releash_<version>_<os>_<arch>`, where `<version>` has no leading `v`. Check that `checksums.txt` was signed by the release workflow at that tag, then that the archive matches it:

```sh
VERSION=0.1.0
BASE=https://github.com/ivanklee86/releash/releases/download/v${VERSION}

curl -sSfLO "${BASE}/releash_${VERSION}_linux_amd64.tar.gz"
curl -sSfLO "${BASE}/checksums.txt"
curl -sSfLO "${BASE}/checksums.txt.sigstore.json"

cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt

tar -xzf "releash_${VERSION}_linux_amd64.tar.gz" releash
./releash --version
```

On macOS, use `shasum -a 256 --check --ignore-missing checksums.txt`. The macOS binaries are not notarized, so if the archive was downloaded with a browser, remove the quarantine attribute with `xattr -d com.apple.quarantine releash` before running it. Each archive also has an SPDX SBOM, `<archive>.sbom.json`, which `checksums.txt` covers.

### Container image

`ghcr.io/ivanklee86/releash` is a multi-architecture image for linux/amd64 and linux/arm64, tagged `<version>` and, for releases that are not prereleases, `latest`. There are no `0` or `0.1` tags. Resolve the tag to a digest, verify the digest, and then run that digest, because a tag can move between two commands:

```sh
VERSION=0.1.0
IMAGE=ghcr.io/ivanklee86/releash
IDENTITY="https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}"

DIGEST=$(docker buildx imagetools inspect "${IMAGE}:${VERSION}" --format '{{json .Manifest.Digest}}' | tr -d '"')

cosign verify "${IMAGE}@${DIGEST}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

docker run --rm "${IMAGE}@${DIGEST}" --version
```

The image's SPDX SBOM is attached as a signed attestation. Check it, and print the SBOM, with:

```sh
cosign verify-attestation --type spdxjson "${IMAGE}@${DIGEST}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  | jq -r '.payload' | base64 -d | jq '.predicate'
```

A pipeline that pins the digest in its config only needs to verify it when the pin changes.

### Homebrew

```sh
brew install ivanklee86/tap/releash
```

The cask is published for releases that are not prereleases.

### Go

```sh
go install github.com/ivanklee86/releash@v0.1.0
```

### Versions

`releash --version` prints `releash version <version>`. Release binaries and images report the version without a leading `v` (`releash version 0.1.0`), and `go install` builds report the Go module version (`releash version v0.1.0`). Versions follow SemVer from v0.1.0. Before v1.0.0, a breaking change to the public contract (command and option names, defaults, environment variables, stdout, exit codes, and archive and image names) bumps the minor version and is listed under "Breaking changes" in the release notes. Exit codes never change meaning in any version ([ADR 0004](docs/adrs/0004-exit-code-contract.md)).

## Usage

`releash check` evaluates a flag through the official Unleash Go SDK and prints `true` or `false`. It reports the check's usage to Unleash before it exits, and without `--remote-address` it uses the local address of its connection to Unleash as `remoteAddress`.

```sh
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

- Releash options: `--url` and `--token` fall back to `UNLEASH_URL` and `UNLEASH_API_TOKEN`, and an option wins over its environment variable. Prefer the environment variable for the token, because options show in process listings and CI logs. `--timeout` is a positive Go duration (default `5s`) that bounds the whole check, including every request to Unleash. `--loud` explains on stderr why the answer is `false`.
- Context options set the Unleash context: `--env` (default `production`), `--app-name` (default `releash`), `--user-id`, `--session-id`, `--remote-address` (an IPv4 or IPv6 address), `--current-time` (an RFC 3339 timestamp), and `--properties name=value`, repeated once per custom property. A property value may contain `=` or be empty.

If Unleash is unreachable or does not answer within `--timeout`, rejects the token, sends a response releash cannot read, or does not know the flag, releash prints `false` and exits 0, so the gated step stays off without failing the pipeline. Add `--loud` to see why on stderr:

```sh
$ releash check deploy-pament --loud
releash: flag "deploy-pament" not found; returning false
false
```

Without `--current-time`, date constraints compare against the time of the check. releash keeps the flags it fetches in memory only and writes no files, so nothing from an earlier check can turn a gate on.

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

## Pipeline examples

### GitHub Actions

This job installs a pinned, verified releash and runs the deploy step only when `deploy-payments` is on. A misconfigured check (a missing token, a bad option) exits non-zero and fails the job, while an Unleash outage gives `false` and skips the deploy (see the exit codes above).

```yaml
jobs:
  deploy:
    runs-on: ubuntu-26.04
    permissions:
      contents: read
    env:
      RELEASH_VERSION: 0.1.0
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Install cosign
        uses: sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532 # v4.1.3
      - name: Install releash
        run: |
          set -euo pipefail
          base="https://github.com/ivanklee86/releash/releases/download/v${RELEASH_VERSION}"
          archive="releash_${RELEASH_VERSION}_linux_amd64.tar.gz"
          dir="${RUNNER_TEMP}/releash"
          mkdir -p "${dir}"
          cd "${dir}"
          curl -sSfLO "${base}/${archive}"
          curl -sSfLO "${base}/checksums.txt"
          curl -sSfLO "${base}/checksums.txt.sigstore.json"
          cosign verify-blob checksums.txt \
            --bundle checksums.txt.sigstore.json \
            --certificate-identity "https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${RELEASH_VERSION}" \
            --certificate-oidc-issuer https://token.actions.githubusercontent.com
          sha256sum --check --ignore-missing checksums.txt
          tar -xzf "${archive}" releash
          echo "${dir}" >> "${GITHUB_PATH}"
      - name: Check the deploy-payments flag
        id: gate
        env:
          UNLEASH_URL: ${{ vars.UNLEASH_URL }}
          UNLEASH_API_TOKEN: ${{ secrets.UNLEASH_API_TOKEN }}
        run: |
          # A separate assignment, so a non-zero exit from releash fails the step.
          enabled=$(releash check deploy-payments --loud)
          echo "enabled=${enabled}" >> "${GITHUB_OUTPUT}"
      - name: Deploy
        if: steps.gate.outputs.enabled == 'true'
        run: ./deploy.sh
```

### Any CI system

In a shell step, compare the output with `true`. With `set -e`, a misconfigured check stops the script, and `false` skips the deploy:

```sh
set -eu
enabled=$(releash check deploy-payments --env production)
if [ "${enabled}" = "true" ]; then
  ./deploy.sh
fi
```

On a container-native CI system, run the image by the digest you verified, passing the Unleash settings through the environment:

```sh
docker run --rm -e UNLEASH_URL -e UNLEASH_API_TOKEN \
  "ghcr.io/ivanklee86/releash@${DIGEST}" check deploy-payments
```

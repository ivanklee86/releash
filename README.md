# releash

[![CI](https://github.com/ivanklee86/releash/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/ivanklee86/releash/actions/workflows/ci.yaml?query=branch%3Amain)
[![Coverage](https://raw.githubusercontent.com/ivanklee86/octocov-central/main/badges/ivanklee86/releash/coverage.svg)](https://octocov.dev/ivanklee86/releash)
[![Test Execution Time](https://raw.githubusercontent.com/ivanklee86/octocov-central/main/badges/ivanklee86/releash/time.svg)](https://octocov.dev/ivanklee86/releash)
[![Go Reference](https://pkg.go.dev/badge/github.com/ivanklee86/releash.svg)](https://pkg.go.dev/github.com/ivanklee86/releash)
[![Release](https://img.shields.io/github/v/release/ivanklee86/releash)](https://github.com/ivanklee86/releash/releases/latest)
[![License: MPL 2.0](https://img.shields.io/badge/license-MPL%202.0-brightgreen.svg)](LICENSE)

A small CLI that lets CI/CD pipelines read [Unleash](https://www.getunleash.io/) feature flags. 🏁🏁🏁

- `releash check <flag>` prints `true` or `false`, so a pipeline can gate a step on a flag.
- `releash variant <flag>` prints the flag's variant and payload as environment variables, so a pipeline can configure the step behind the gate.

## Install

Pin an exact version in pipelines. To check signatures before you run a download, see [Verify a release](#verify-a-release).

### Binary

```sh
VERSION=0.2.0
curl -sSfL "https://github.com/ivanklee86/releash/releases/download/v${VERSION}/releash_${VERSION}_linux_amd64.tar.gz" | tar -xz releash
./releash --version
```

Archives are named `releash_<version>_<os>_<arch>` and are published for `linux` and `darwin` on `amd64` and `arm64` (`.tar.gz`), and for `windows` on `amd64` (`.zip`).

### Homebrew

```sh
brew install ivanklee86/tap/releash
```

### Go

```sh
go install github.com/ivanklee86/releash@v0.2.0
```

### Container image

```sh
docker run --rm -e UNLEASH_URL -e UNLEASH_API_TOKEN ghcr.io/ivanklee86/releash:0.2.0 check deploy-payments
```

The image supports `linux/amd64` and `linux/arm64`. Tags are exact versions, and `latest` for releases that are not prereleases.

## Quick start

Point releash at Unleash with a backend (client) API token, then gate a step on a flag:

```sh
export UNLEASH_URL=https://unleash.example.com/api
export UNLEASH_API_TOKEN=default:production.abc123

if [ "$(releash check deploy-payments)" = "true" ]; then
  ./deploy.sh
fi
```

## Check a flag

```text
releash check <flag> [options]
```

`check` always prints exactly `true` or `false` on stdout and exits 0 for either answer, so it is safe under `set -e`.

It fails safe. If Unleash is unreachable, too slow, rejects the token, sends an unreadable response or does not know the flag, `check` prints `false` and exits 0. The gated step stays off and the pipeline keeps going. Add `--loud` to see the reason on stderr:

```console
$ releash check deploy-pament --loud
releash: flag "deploy-pament" not found; returning false
false
```

A mistake in the command or its configuration, such as a missing token or an unknown option, also prints `false` but exits non-zero. See [Exit codes](#exit-codes).

Each check reports its usage to Unleash before it exits. releash keeps fetched flags in memory only and writes no files, so a previous run can never turn a gate on.

## Load a variant

```text
releash variant <flag> [options]
```

`variant` needs releash 0.2.0 or later.

`variant` prints the flag's variant and payload as environment variables. Suppose the flag `checkout-redesign` has a variant `treatment` with this JSON payload:

```json
{"image": {"tag": "v2"}, "replicas": 3, "regions": ["eu", "us"]}
```

Then `releash variant checkout-redesign` prints:

```text
RELEASH_CHECKOUT_REDESIGN_ENABLED=true
RELEASH_CHECKOUT_REDESIGN_VARIANT=treatment
RELEASH_CHECKOUT_REDESIGN_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG=v2
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0=eu
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1=us
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS=3
```

### Variable names

- The default prefix is `RELEASH_<FLAG>_`, where `<FLAG>` is the flag name in upper case with every character outside `[A-Z0-9_]` replaced by `_`. Override it with `--prefix`.
- `PAYLOAD` holds the payload as Unleash stores it. JSON payloads are compacted. A variant without a payload prints no `PAYLOAD`.
- A JSON payload also gets one `PAYLOAD__<path>` variable per leaf value. Nested keys are joined with `__`, and array elements use their index.
- Keys are normalised like the flag name, so `imageTag` becomes `IMAGETAG`. Prefer snake_case or kebab-case keys.
- `null` becomes an empty value. Numbers keep their original text.

Read the variables with a default for when the experiment is off, for example `"${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"`.

Environment variables are visible to every later step, so never put secrets in a payload.

### Output formats

| `--format` | Output | Use with |
|---|---|---|
| `dotenv` (default) | `NAME=value` | `$GITHUB_ENV`, GitLab dotenv reports, `docker run --env-file` |
| `shell` | `export NAME='value'` | `eval` in a shell |

### The disabled variant

When there is no variant to report, `variant` prints the disabled variant:

```text
RELEASH_CHECKOUT_REDESIGN_ENABLED=false
RELEASH_CHECKOUT_REDESIGN_VARIANT=disabled
```

| Situation | Output | Exit |
|---|---|---|
| The flag is enabled but has no variants | `ENABLED=true`, `VARIANT=disabled` | 0 |
| Unleash cannot answer, or does not know the flag | Disabled variant | 0 |
| The payload cannot be printed (invalid JSON, two keys with the same variable name, a newline in a `dotenv` value) | Disabled variant | 0 |
| The flag is disabled for the context | Disabled variant, and an error on stderr | 6 |

Exit 6 means the pipeline loaded a variant without gating on `check` first. Run `check`, and call `variant` only when it prints `true`. Pass both commands the same context options, otherwise they can disagree.

### Keep the variant sticky

Unleash picks a variant by hashing a context field. With default stickiness that is `userId`, then `sessionId`, then `remoteAddress`. Pipelines usually set neither ID, so the variant follows `remoteAddress`, which releash sets to the local address of its connection to Unleash. That address can change between runs, and behind a shared proxy or NAT every repository gets the same variant. A gradual rollout on default stickiness skips `remoteAddress`, so without an ID it turns on and off at random.

Pass something stable that identifies what you are experimenting on, usually the repository, to both commands:

```sh
releash check checkout-redesign --user-id "$GITHUB_REPOSITORY"
releash variant checkout-redesign --user-id "$GITHUB_REPOSITORY"
```

If `userId` already means something else in your Unleash setup, use a custom property instead:

```sh
releash variant checkout-redesign --properties repo="$GITHUB_REPOSITORY"
```

For this to work, create `repo` as a context field with stickiness enabled in Unleash, and select it as the stickiness of the variants and the rollout strategy. Custom stickiness does not fall back to `userId`. A run that omits the property gets a random variant, and does not match that strategy, so unless another strategy matches, `check` prints `false` and `variant` exits 6.

| CI system | Repository or pipeline name |
|---|---|
| GitHub Actions | `$GITHUB_REPOSITORY` |
| GitLab CI/CD | `$CI_PROJECT_PATH` |
| Jenkins | `$JOB_NAME` |
| CircleCI | `$CIRCLE_PROJECT_USERNAME/$CIRCLE_PROJECT_REPONAME` |
| Buildkite | `$BUILDKITE_PIPELINE_SLUG` |
| Azure Pipelines | `$BUILD_REPOSITORY_NAME` |

## Options

Options go after the command name. `releash --loud check <flag>` is an error.

| Option | Default | Description |
|---|---|---|
| `--url <url>` | `$UNLEASH_URL` | Unleash API URL, including `/api` |
| `--token <token>` | `$UNLEASH_API_TOKEN` | Backend (client) API token. Prefer the environment variable, because options show up in process listings and CI logs. |
| `--timeout <duration>` | `5s` | Time limit for the whole command, as a Go duration |
| `--loud` | off | Explain fallbacks on stderr |
| `--format dotenv\|shell` | `dotenv` | Output format (`variant` only) |
| `--prefix <prefix>` | `RELEASH_<FLAG>_` | Variable name prefix, matching `^[A-Z][A-Z0-9_]*$` (`variant` only) |

These options set the [Unleash context](https://docs.getunleash.io/reference/unleash-context) the flag is evaluated against:

| Option | Default | Context field |
|---|---|---|
| `--env <name>` | `production` | `environment` |
| `--app-name <name>` | `releash` | `appName` |
| `--user-id <id>` | unset | `userId` |
| `--session-id <id>` | unset | `sessionId` |
| `--remote-address <ip>` | Local address of the connection to Unleash | `remoteAddress` |
| `--current-time <timestamp>` | Time of the check | `currentTime`, as an RFC 3339 timestamp |
| `--properties <name>=<value>` | none | A custom property. Repeat the option for more. |

## Exit codes

| Exit | Meaning |
|---|---|
| 0 | An answer, including the fail-safe `false` or disabled variant |
| 1 | Internal error in releash |
| 2 | Bad command line: unknown option, missing or extra arguments, an invalid `--timeout`, `--format` or `--prefix`, or one of the non-context options given twice |
| 3 | Missing or empty Unleash URL or API token |
| 4 | Invalid context value, property name or Unleash URL |
| 5 | A context option or property given twice |
| 6 | `variant` only: the flag is disabled for the context |

On a non-zero exit, `check` still prints `false` and `variant` prints the disabled variant when it can tell which format and prefix you asked for. When there are several mistakes, the lowest code wins.

## CI examples

### GitHub Actions

Gate a deploy on a flag. An Unleash outage skips the deploy, and a misconfigured check fails the job.

```yaml
jobs:
  deploy:
    runs-on: ubuntu-26.04
    permissions:
      contents: read
    env:
      RELEASH_VERSION: 0.2.0
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Install releash
        run: |
          set -euo pipefail
          mkdir -p "${RUNNER_TEMP}/releash"
          curl -sSfL "https://github.com/ivanklee86/releash/releases/download/v${RELEASH_VERSION}/releash_${RELEASH_VERSION}_linux_amd64.tar.gz" \
            | tar -xz -C "${RUNNER_TEMP}/releash" releash
          echo "${RUNNER_TEMP}/releash" >> "${GITHUB_PATH}"
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

To configure the deploy from a variant, gate and load in the same step:

```yaml
      - name: Load the checkout-redesign variant
        id: gate
        env:
          UNLEASH_URL: ${{ vars.UNLEASH_URL }}
          UNLEASH_API_TOKEN: ${{ secrets.UNLEASH_API_TOKEN }}
        run: |
          enabled=$(releash check checkout-redesign --user-id "${GITHUB_REPOSITORY}")
          echo "enabled=${enabled}" >> "${GITHUB_OUTPUT}"
          if [ "${enabled}" = "true" ]; then
            releash variant checkout-redesign --user-id "${GITHUB_REPOSITORY}" >> "${GITHUB_ENV}"
          fi
      - name: Deploy
        if: steps.gate.outputs.enabled == 'true'
        run: ./deploy.sh --tag "${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"
```

### Any shell

```sh
set -eu
repo="${CI_PROJECT_PATH}" # GitLab CI/CD
enabled=$(releash check checkout-redesign --user-id "${repo}")
if [ "${enabled}" = "true" ]; then
  vars=$(releash variant checkout-redesign --user-id "${repo}" --format shell)
  eval "${vars}"
  ./deploy.sh --tag "${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"
fi
```

Capture the output before `eval`. `eval "$(releash variant ...)"` returns the status of `eval` and hides a failure in releash.

## Verify a release

Releases are signed with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) keyless signing, so verification needs cosign but no keys. Use cosign v3 or later. With cosign v2, add `--new-bundle-format` to each command below.

### Binary

Check that `checksums.txt` was signed by the release workflow for that tag, then that the archive matches it:

```sh
VERSION=0.2.0
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
```

On macOS, use `shasum -a 256 --check --ignore-missing checksums.txt`. The macOS binaries are not notarized. If you downloaded the archive with a browser, run `xattr -d com.apple.quarantine releash` before the first use.

In GitHub Actions, install cosign with [`sigstore/cosign-installer`](https://github.com/sigstore/cosign-installer) and replace the example's install step with these commands, so the archive is saved to disk and checked before it is extracted.

### Container image

Resolve the tag to a digest, verify the digest, then run the digest. A tag can move, a digest cannot.

```sh
VERSION=0.2.0
IMAGE=ghcr.io/ivanklee86/releash
IDENTITY="https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}"

DIGEST=$(docker buildx imagetools inspect "${IMAGE}:${VERSION}" --format '{{json .Manifest.Digest}}' | tr -d '"')

cosign verify "${IMAGE}@${DIGEST}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

docker run --rm "${IMAGE}@${DIGEST}" --version
```

A pipeline that pins the digest in its configuration only needs to verify again when the pin changes.

### SBOMs

Each archive has an SPDX SBOM, `<archive>.sbom.json`, which `checksums.txt` covers. The image SBOM is a signed attestation:

```sh
cosign verify-attestation --type spdxjson "${IMAGE}@${DIGEST}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  | jq -r '.payload' | base64 -d | jq '.predicate'
```

The multi-architecture digest carries the `linux/amd64` SBOM. For `linux/arm64`, verify that platform's digest, which `docker buildx imagetools inspect --raw` lists.

## Versioning

releash follows [SemVer](https://semver.org/). Before v1.0.0, a breaking change to command names, options, defaults, environment variables, output or artifact names bumps the minor version and is listed under "Breaking changes" in the release notes. Exit codes never change meaning.

## License

[Mozilla Public License 2.0](LICENSE)

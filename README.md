# releash

A small CLI to drive CI/CD changes via Unleash feature flags

releash is in early development. The first complete release is v0.1.2, so use v0.1.2 or later. v0.1.0 has no release, archives or image, though `go install ...@v0.1.0` still resolves, to a pre-release commit; v0.1.1 has no Homebrew cask and no image SBOM attestations. The instructions below use v0.1.2. `releash variant` (see [Variants](#variants)) is not in a release yet and will ship in v0.2.0.

## Install

releash runs in pipelines that decide whether to deploy, so pin an exact version and verify what you download. Every release is built by [`.github/workflows/release.yaml`](.github/workflows/release.yaml) and signed with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) keyless signing, so verifying needs cosign but no keys. Use cosign v3 or later: releases are signed with cosign v3, which writes Sigstore bundles, and cosign v2 does not read that format by default, so its `verify`, `verify-attestation` and `verify-blob` fail on a valid release. If you must use cosign v2, add `--new-bundle-format` to each of those commands. The details are in [spec 0002](docs/specs/0002-distribution.md).

### Binary

Archives are published for Linux and macOS on amd64 and arm64 (`.tar.gz`) and for Windows on amd64 (`.zip`), named `releash_<version>_<os>_<arch>`, where `<version>` has no leading `v`. Check that `checksums.txt` was signed by the release workflow at that tag, then that the archive matches it:

```sh
VERSION=0.1.2
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
VERSION=0.1.2
IMAGE=ghcr.io/ivanklee86/releash
IDENTITY="https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v${VERSION}"

DIGEST=$(docker buildx imagetools inspect "${IMAGE}:${VERSION}" --format '{{json .Manifest.Digest}}' | tr -d '"')

cosign verify "${IMAGE}@${DIGEST}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

docker run --rm "${IMAGE}@${DIGEST}" --version
```

The image's SPDX SBOM is attached as a signed attestation. The multi-arch digest carries the `linux/amd64` SBOM, and each platform image carries an SBOM of its own, so for an arm64 runner, verify the `linux/arm64` digest (from `docker buildx imagetools inspect --raw`) instead. Check it, and print the SBOM, with:

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
go install github.com/ivanklee86/releash@v0.1.2
```

### Versions

`releash --version` prints `releash version <version>`. Release binaries and images report the version without a leading `v` (`releash version 0.1.2`), and `go install` builds report the Go module version (`releash version v0.1.2`). Versions follow SemVer from v0.1.0, and the first complete release is v0.1.2. Before v1.0.0, a breaking change to the public contract (command and option names, defaults, environment variables, stdout, exit codes, and archive and image names) bumps the minor version and is listed under "Breaking changes" in the release notes. Exit codes never change meaning in any version ([ADR 0004](docs/adrs/0004-exit-code-contract.md)).

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

## Variants

`releash variant` prints a flag's variant and its payload as environment variables, so later steps can read an experiment's settings, such as an image tag or a replica count, without knowing about Unleash or JSON. `check` stays the gate and `variant` configures what runs behind it. It takes every option of `check`, with the same defaults, environment variables and validation, plus `--format` and `--prefix`:

```text
releash variant <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                       [--format dotenv|shell] [--prefix <prefix>]
                       [--env <name>] [--app-name <name>]
                       [--user-id <id>] [--session-id <id>]
                       [--remote-address <ip>] [--current-time <timestamp>]
                       [--properties <name>=<value>]...
```

For a flag `checkout-redesign` whose variant `treatment` has the JSON payload `{"image": {"tag": "v2"}, "replicas": 3, "regions": ["eu", "us"]}`, it prints:

```text
RELEASH_CHECKOUT_REDESIGN_ENABLED=true
RELEASH_CHECKOUT_REDESIGN_VARIANT=treatment
RELEASH_CHECKOUT_REDESIGN_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG=v2
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0=eu
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1=us
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS=3
```

- The prefix is `RELEASH_`, then the flag name in upper case with every character outside `[A-Z0-9_]` replaced by `_`, then `_`. `--prefix` replaces it and must match `^[A-Z][A-Z0-9_]*$`. Flags whose names differ only in case or punctuation get the same default prefix, so give one of them its own `--prefix` if a pipeline loads both.
- `PAYLOAD` holds the payload: `string`, `number` and `csv` payloads unchanged, and `json` payloads as compact JSON. A variant without a payload prints no `PAYLOAD`.
- A `json` payload also gets one `PAYLOAD__<path>` variable per leaf, nesting spelled with `__` and array elements by their index. Numbers keep their text (`1.0` stays `1.0`), `null` is the empty string, and empty objects and arrays only appear in `PAYLOAD`. Keys are normalised like the flag name, so use snake_case or kebab-case keys: `imageTag` becomes `IMAGETAG`. Variables are printed as `ENABLED`, `VARIANT`, `PAYLOAD`, then the leaves sorted by name.
- `--format dotenv` (the default) prints `NAME=value` lines for `$GITHUB_ENV`, GitLab dotenv reports or `docker run --env-file`. `--format shell` prints `export NAME='value'` lines, quoted for `eval`.
- Later steps should read the variables with a default for when the experiment is off, e.g. `"${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"`.

The variables are public contract from v0.2.0: see [spec 0003](docs/specs/0003-variant-variables.md) and [ADR 0006](docs/adrs/0006-variant-variable-names.md) for every naming rule.

### When there is no variant

- An enabled flag without variants prints `ENABLED=true` and `VARIANT=disabled` and exits 0.
- If releash cannot get an answer (Unleash is unreachable or too slow, rejects the token, sends a response releash cannot read, does not know the flag, or holds a flag the SDK cannot evaluate), it prints the disabled variant (`ENABLED=false`, `VARIANT=disabled`, no payload) and exits 0. It does the same when the payload cannot be printed: invalid JSON, two keys that give the same variable name, or a newline in a value in `dotenv` format (`--format shell` carries newlines). `--loud` says why on stderr.
- A disabled flag means the pipeline loaded the variant without gating on `check`. releash prints the disabled variant, writes `releash: flag "<flag>" is disabled; gate on "releash check <flag>" before loading its variant` to stderr and exits 6, so the step fails.
- A mistake in the command or its configuration prints the disabled variant in the requested format and exits with `check`'s codes 1 to 5. An invalid `--format` or `--prefix`, or either given twice, exits 2. If releash cannot tell which format or prefix you asked for, stdout is empty.

| Exit | Meaning |
|---|---|
| 0 | A variant, or the fallback to the disabled variant |
| 1 to 5 | As for `check` |
| 6 | The flag was evaluated and is disabled for the context |

Pass `check` and `variant` the same context. If they differ, `check` can answer `true` for a context in which `variant` finds the flag disabled, and `variant` exits 6.

### Keep the variant sticky

Unleash assigns variants by the variant's stickiness. To pick the variant, default stickiness uses `userId`, then `sessionId`; the Go SDK then uses `remoteAddress`, which releash sets to the local address of its connection to Unleash when `--remote-address` is not given, and only then picks at random. A gradual rollout on default stickiness skips `remoteAddress`: without `userId` or `sessionId`, whether the flag is on at all is decided at random on every run. A pipeline usually sets neither ID, so without help a partial rollout turns on and off from run to run, and the variant follows the connection's address rather than the repository; containers and proxies can give every repository the same address, and so the same variant. releash does not detect the CI system, so pass something that identifies what you are experimenting on, usually the repository or pipeline.

The simplest way is to pass the repository as `--user-id`. It needs no change in Unleash, and each repository always gets the same variant. Pass the same `--user-id` to `check`, so that a gradual rollout on default stickiness puts the gate and the variant in the same bucket:

```sh
releash variant checkout-redesign --user-id "$GITHUB_REPOSITORY" >> "$GITHUB_ENV"
```

If `userId` already means something else in your Unleash setup, pass a custom property instead, to `check` too, and set the variant's stickiness to that property in Unleash. Create the property as a context field with stickiness enabled, so that Unleash offers it as a stickiness option. Custom stickiness does not fall back to `userId`. A run that omits the property gets a random feature variant, and a strategy whose stickiness is that property does not match at all, so unless another strategy matches the flag is off for that run: `check` prints `false` and `variant` exits 6:

```sh
releash variant checkout-redesign --properties repo="$GITHUB_REPOSITORY" >> "$GITHUB_ENV"
```

The repository or pipeline name in common CI systems:

| CI system | Repository or pipeline |
|---|---|
| GitHub Actions | `$GITHUB_REPOSITORY` |
| GitLab CI/CD | `$CI_PROJECT_PATH` |
| Jenkins | `$JOB_NAME` |
| CircleCI | `$CIRCLE_PROJECT_USERNAME/$CIRCLE_PROJECT_REPONAME` |
| Buildkite | `$BUILDKITE_PIPELINE_SLUG` |
| Azure Pipelines | `$BUILD_REPOSITORY_NAME` |

With `--loud`, releash warns when the flag assigns variants by default stickiness and neither `--user-id` nor `--session-id` is set.

Environment variables are visible to every later step and to the processes they start, so payloads must not hold secrets.

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
      RELEASH_VERSION: 0.1.2
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

To configure the deploy from a variant (releash v0.2.0 or later), gate with `check` and load the variant into `$GITHUB_ENV` in the same step, passing both the same context. The redirect keeps releash's exit code, so exit 6 or a misconfiguration fails the step:

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

### Any CI system

In a shell step, compare the output with `true`. With `set -e`, a misconfigured check stops the script, and `false` skips the deploy:

```sh
set -eu
enabled=$(releash check deploy-payments --env production)
if [ "${enabled}" = "true" ]; then
  ./deploy.sh
fi
```

To load a variant into the shell, use `--format shell` and capture the output before `eval`. `eval "$(releash variant ...)"` would exit with `eval`'s status and hide exit 6 and the configuration errors:

```sh
set -eu
repo="${CI_PROJECT_PATH}" # GitLab; see Keep the variant sticky for other CI systems
enabled=$(releash check checkout-redesign --user-id "${repo}")
if [ "${enabled}" = "true" ]; then
  vars=$(releash variant checkout-redesign --user-id "${repo}" --format shell)
  eval "${vars}"
  ./deploy.sh --tag "${RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG:-v1}"
fi
```

On a container-native CI system, run the image by the digest you verified, passing the Unleash settings through the environment:

```sh
docker run --rm -e UNLEASH_URL -e UNLEASH_API_TOKEN \
  "ghcr.io/ivanklee86/releash@${DIGEST}" check deploy-payments
```

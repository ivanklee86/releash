---
name: release
description: Prepare or publish a releash release - draft the notes, pin the README to the new version, tag it, watch the release workflow, verify signatures and attestations, and finish the GitHub release notes.
argument-hint: "[version, e.g. 0.3.0]"
model: fable
disable-model-invocation: true
allowed-tools: Bash(git *) Bash(gh *) Bash(task *) Bash(prek *) Bash(docker *) Bash(curl *) Bash(sha256sum *) Bash(tar *) Bash(jq *) Bash(go install *) Bash(mktemp *)
---

# Release releash

Release releash as spec 0002 (`docs/specs/0002-distribution.md`) describes. The requested version, if any, is: $ARGUMENTS

A release takes two runs of `/release`, because `main` takes changes only through pull requests and Claude never merges:

1. **Prepare** opens a release pull request that pins `README.md` to the new version and carries the draft notes.
2. **Publish**, run after the user has merged that pull request, tags it, watches the release workflow, verifies what it published, and finishes the release notes.

Work out which phase applies from the repository, so running `/release` again always continues at the right step:

- No open or merged pull request titled `Release v<version>` for the version: **Prepare**.
- That pull request is open: tell the user it is waiting for their review and merge, and stop.
- It is merged and the tag `v<version>` does not exist: **Publish**.
- The tag exists: check whether the GitHub release exists and its notes have a Highlights section, finish whatever step of Publish is missing, and report.

Stop and explain at the first check that fails. Never merge a pull request, never re-run or cancel the release workflow, and never delete or move a tag.

## Prepare

1. Check the starting point: `git status --porcelain` is empty, local `main` matches `origin/main` after `git fetch --tags`, and the latest CI run on `main`'s head succeeded (`gh run list --workflow ci.yaml --branch main --limit 1`).
2. Pick the version. The last release is the highest `v*` tag by `git tag --sort=-v:refname` that has a GitHub release. List the pull requests merged since it with their labels (`gh pr list --state merged --base main --search "merged:>=<date of the last tag>" --json number,title,labels,body`). Following spec 0002's "Versioning":
   - before `v1.0.0`, `breaking-change` or `enhancement` bumps the minor version, anything else the patch version
   - from `v1.0.0`, `breaking-change` bumps the major version, `enhancement` the minor, anything else the patch

   If `$ARGUMENTS` names a version, use it, but say so if it disagrees with the labels. Otherwise propose the version with the reason. Either way, ask the user to confirm before going on.
3. Review the pull requests. Flag any title that would read badly in the release notes, and any pull request that looks like it changes the public contract (output, options, exit codes, variable names) without the `breaking-change` label. Labels can still be fixed before tagging, because GitHub builds the notes when the release is created.
4. Draft the notes:
   - Preview GitHub's generated list with `gh api repos/{owner}/{repo}/releases/generate-notes -f tag_name=v<version> -f target_commitish=main -f previous_tag_name=v<last>`.
   - Write a `## Highlights` section to go above it, in the style of the `v0.2.0` release (`gh release view v0.2.0`): what changed for users in plain sentences, a short example where it helps, and what stayed the same. Link to the README at the tag (`https://github.com/ivanklee86/releash/blob/v<version>/README.md#...`). For a release with only dependency or internal changes, one or two sentences are enough.
5. On a new branch `release/v<version>`, update the version pins in `README.md`: the `VERSION=` lines, `go install github.com/ivanklee86/releash@v<version>`, the image tag `ghcr.io/ivanklee86/releash:<version>` and `RELEASH_VERSION`. Leave minimum-version statements, such as "`variant` needs releash 0.2.0 or later", alone. Show the diff.
6. Run `task` and `prek run --all-files`, commit, push, and open the pull request titled `Release v<version>`. Its description holds the draft Highlights between `<!-- highlights:start -->` and `<!-- highlights:end -->`, then the preview of the generated notes under a "Generated notes (preview)" heading, so the user reviews both before merging.
7. Tell the user to review and merge the pull request, editing the Highlights in its description if they like, and then run `/release` again. Stop.

## Publish

Running `/release` after the release pull request is merged is the user's go to publish.

1. Check: `git fetch --tags`, the merge commit of the `Release v<version>` pull request is on `origin/main` (later merges are fine; the tag goes on the release merge, not on `main`'s head), the tag does not exist locally or on `origin`, and the CI run on that commit succeeded. Wait for CI if it is still running.
2. Create the annotated tag and push it: `git tag -a v<version> -m "releash v<version>" <merge commit>` and `git push origin v<version>`.
3. Watch the release workflow. Find its run with `gh run list --workflow release.yaml --branch v<version>`, then `gh run watch <id> --exit-status`. If any step fails, stop: report which step failed with the end of its log (`gh run view <id> --log-failed`). Do not re-run it, because goreleaser would run against assets that already exist. The fix is a new version (see "Release pipeline" in `docs/architecture.md`).
4. Verify in a new directory from `mktemp -d`, with the commands from the README's "Verify a release". cosign is not installed in the devcontainer, so run it from its image, pinned to the `COSIGN_VERSION` in `.github/workflows/release.yaml` (for example `docker run --rm -v "$PWD:/w" -w /w ghcr.io/sigstore/cosign/cosign:<COSIGN_VERSION> ...`). The identity is `https://github.com/ivanklee86/releash/.github/workflows/release.yaml@refs/tags/v<version>` and the issuer `https://token.actions.githubusercontent.com`. Check each of these and record the result:
   - `cosign verify-blob` on `checksums.txt` with `checksums.txt.sigstore.json`, then `sha256sum --check --ignore-missing checksums.txt` for the `linux_amd64` archive and its SBOM.
   - The extracted binary prints `releash version <version>`.
   - Resolve the image index digest with `docker buildx imagetools inspect`, and the `linux/amd64` and `linux/arm64` digests from its `--raw` manifest. `cosign verify` and `cosign verify-attestation --type spdxjson` pass on all three.
   - `docker run --rm ghcr.io/ivanklee86/releash@<index digest> --version` prints `releash version <version>`.
   - `GOBIN=<dir> go install github.com/ivanklee86/releash@v<version>`, through the default proxy, gives a binary that prints `releash version v<version>`.
   - For a version without a prerelease suffix: the `latest` image tag resolves to the same index digest, and the cask in `ivanklee86/homebrew-tap` (`gh api repos/ivanklee86/homebrew-tap/contents/Casks/releash.rb`) names the version, with sha256 values that match `checksums.txt`.

   If a check fails, stop and report it. The release is already public, so do not try to repair it; the fix is a new version.
5. Finish the notes. Take the Highlights from the merged pull request's description, between the markers, as the user last left them. Put them above the notes goreleaser published (`gh release view v<version> --json body`) and apply them with `gh release edit v<version> --notes-file <file>`. Keep the generated part unchanged.
6. Report: the release URL, the tagged commit, the workflow run, and each verification with its result.

#!/usr/bin/env bash
# Stop hook: runs task and prek run --all-files when the working tree changed
# since the last passing run, and blocks the end of the turn on a failure, so
# Claude fixes it before handing off. Integration tests are not run here: they
# need the shared stack and take over a minute.
#
# When Claude is already continuing because of this hook (stop_hook_active),
# a second failure is reported but does not block again, so a failure Claude
# cannot fix, such as a missing tool, cannot loop.
#
# CLAUDE_STOP_CHECK_CMD replaces the checks, for the hook's own tests.
set -euo pipefail

active=$(jq -r '.stop_hook_active // false')
cd "${CLAUDE_PROJECT_DIR:-.}"

stamp="$(git rev-parse --git-dir)/claude-stop-check"
state() {
  { git rev-parse HEAD 2>/dev/null || true; git status --porcelain=v1 --untracked-files=all; git diff HEAD 2>/dev/null || true; } | sha256sum | cut -d' ' -f1
}

if [[ -f "${stamp}" && "$(cat "${stamp}")" == "$(state)" ]]; then
  exit 0
fi

checks=${CLAUDE_STOP_CHECK_CMD:-"task && prek run --all-files"}
if out=$(bash -c "${checks}" 2>&1); then
  # prek's fixers may have changed files, so record the state after the checks.
  state >"${stamp}"
  exit 0
fi

msg="The end-of-turn checks failed (${checks}). Fix them before handing off. Last lines of output:
$(tail -n 40 <<<"${out}")"
if [[ "${active}" == "true" ]]; then
  # Already retried once: let the turn end, but say so.
  jq -n --arg msg "${msg}" '{systemMessage: $msg}'
  exit 0
fi
echo "${msg}" >&2
exit 2

#!/usr/bin/env bash
# Tests for the Claude Code hooks in this directory. Each case pipes the JSON
# Claude Code would send into a hook and checks its exit code and decision.
# Run with: task claude:test-hooks
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
failures=0

fail() {
  echo "FAIL: $*"
  failures=$((failures + 1))
}

# guard.sh: denied commands, also inside compound commands, and look-alikes
# that must pass.
decision() {
  jq -n --arg tool "$1" --arg cmd "$2" '{tool_name: $tool, tool_input: {command: $cmd}}' |
    "${here}/guard.sh" | jq -rs '.[0].hookSpecificOutput.permissionDecision // "allow"'
}

for cmd in \
  "task services:down" \
  "task -x services:down" \
  "go build ./... && task services:down" \
  "FOO=1 task services:down" \
  "docker compose down --volumes" \
  "docker-compose down" \
  "gh pr merge 42 --squash" \
  "git fetch; gh pr merge" \
  "gh api -X PUT repos/ivanklee86/releash/pulls/42/merge" \
  "gh api graphql -f query='mutation { mergePullRequest(input: {}) { clientMutationId } }'"; do
  [[ "$(decision Bash "${cmd}")" == "deny" ]] || fail "guard.sh should deny: ${cmd}"
done

for cmd in \
  "task services" \
  "task" \
  "task go:test-integration" \
  "docker compose ps" \
  "docker compose up --detach --wait unleash" \
  "gh pr view 42" \
  "gh pr create --title x" \
  "gh api repos/ivanklee86/releash/pulls/42" \
  "git push origin main" \
  "git push origin v0.3.0" \
  "echo services:down"; do
  [[ "$(decision Bash "${cmd}")" == "allow" ]] || fail "guard.sh should allow: ${cmd}"
done

[[ "$(decision mcp__claude_ai_Github_MCP__merge_pull_request "")" == "deny" ]] ||
  fail "guard.sh should deny the GitHub MCP merge tool"
[[ "$(decision Read "")" == "allow" ]] || fail "guard.sh should ignore other tools"

# format-go.sh: formats .go files and leaves other files alone.
tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT

edit() {
  jq -n --arg f "$1" '{tool_name: "Edit", tool_input: {file_path: $f}}' | "${here}/format-go.sh"
}

printf 'package x\n\nfunc  f( ) {}\n' >"${tmp}/x.go"
edit "${tmp}/x.go" >/dev/null
[[ "$(cat "${tmp}/x.go")" == $'package x\n\nfunc f() {}' ]] || fail "format-go.sh should format x.go, got: $(cat "${tmp}/x.go")"

printf 'func  f( ) {}\n' >"${tmp}/notes.txt"
edit "${tmp}/notes.txt" >/dev/null
[[ "$(cat "${tmp}/notes.txt")" == "func  f( ) {}" ]] || fail "format-go.sh should leave notes.txt alone"

printf 'package x\nfunc {\n' >"${tmp}/broken.go"
out=$(edit "${tmp}/broken.go") || fail "format-go.sh should not fail on a broken file"
[[ "$(jq -r '.hookSpecificOutput.additionalContext // empty' <<<"${out}")" == *"golangci-lint fmt failed"* ]] ||
  fail "format-go.sh should report a broken file as context"

# stop-check.sh: runs the checks only when the tree changed, blocks once on a
# failure, and does not block a second time.
repo="${tmp}/repo"
git init -q "${repo}"
git -C "${repo}" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init

stop() {
  local active=$1 cmd=$2
  jq -n --argjson active "${active}" '{stop_hook_active: $active}' |
    CLAUDE_PROJECT_DIR="${repo}" CLAUDE_STOP_CHECK_CMD="${cmd}" "${here}/stop-check.sh" >/dev/null 2>&1
}

stop false true || fail "stop-check.sh should pass when the checks pass"
stop false false || fail "stop-check.sh should skip the checks when nothing changed since a pass"
echo change >"${repo}/file"
code=0
stop false false || code=$?
[[ "${code}" == 2 ]] || fail "stop-check.sh should block (exit 2) on a failure, got ${code}"
stop true false || fail "stop-check.sh should not block a second time"
stop false true || fail "stop-check.sh should pass once the checks pass again"

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} hook test(s) failed"
  exit 1
fi
echo "All hook tests passed"

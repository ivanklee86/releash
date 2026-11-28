#!/usr/bin/env bash
# PreToolUse hook for Bash and the GitHub MCP merge tool: refuses commands that
# delete the shared local Unleash stack or merge a pull request. The user's
# merge is the checkpoint before dependent work. Compound commands are split
# on ;, &&, || and | first, so chaining does not get around a rule.
#
# This guards against mistakes, not against a determined agent: a command built
# at run time, for example through eval, can get past it.
set -euo pipefail

input=$(cat)
tool=$(jq -r '.tool_name // empty' <<<"${input}")

deny() {
  jq -n --arg reason "$1" \
    '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $reason}}'
  exit 0
}

if [[ "${tool}" == mcp__*merge_pull_request ]]; then
  deny "Claude never merges pull requests in this repository; the user merges."
fi
[[ "${tool}" == "Bash" ]] || exit 0

command=$(jq -r '.tool_input.command // empty' <<<"${input}")

# One segment per line: split on ;, &&, || and |, and on newlines.
segments=$(sed -E 's/(\&\&|\|\||;|\|)/\n/g' <<<"${command}")

while IFS= read -r segment; do
  # Drop leading whitespace and leading VAR=value assignments.
  segment=$(sed -E 's/^[[:space:]]+//; s/^([A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+)+//' <<<"${segment}")

  if [[ "${segment}" =~ ^task([[:space:]]|$) && "${segment}" =~ (^|[[:space:]])services:down([[:space:]]|$) ]]; then
    deny "task services:down deletes the shared local Unleash stack and its data. Leave it running; ask the user if it must be reset."
  fi
  if [[ "${segment}" =~ ^(docker[[:space:]]+compose|docker-compose)([[:space:]]|$) && "${segment}" =~ [[:space:]]down([[:space:]]|$) ]]; then
    deny "docker compose down stops the shared local Unleash stack. Leave it running; ask the user if it must be reset."
  fi
  if [[ "${segment}" =~ ^gh[[:space:]]+pr[[:space:]]+merge([[:space:]]|$) ]]; then
    deny "Claude never merges pull requests in this repository; the user merges."
  fi
  if [[ "${segment}" =~ ^gh[[:space:]]+api([[:space:]]|$) && ( "${segment}" =~ pulls/[0-9]+/merge || "${segment}" =~ mergePullRequest ) ]]; then
    deny "Claude never merges pull requests in this repository; the user merges."
  fi
done <<<"${segments}"

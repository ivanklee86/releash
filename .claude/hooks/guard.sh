#!/usr/bin/env bash
# PreToolUse hook for Bash and the GitHub MCP merge tool: refuses commands that
# delete the shared local Unleash stack or merge a pull request. The user's
# merge is the checkpoint before dependent work. Compound commands, subshells,
# bash -c payloads and command substitutions are split into segments first, and
# wrappers such as env or sudo are dropped, so they do not get around a rule.
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

# One segment per line. Splitting on control operators, subshell and group
# brackets, command substitution and quotes means the payload of bash -c '...'
# or $(...) is checked as a command of its own. A quoted argument that happens
# to read like a denied command, such as a commit message, is refused too; that
# is the cheaper mistake.
segments=$(sed -E 's/(\&\&|\|\||;|\||&|\(|\)|\{|\}|`|\$|'"'"'|")/\n/g' <<<"${command}")

# The merge endpoint or mutation is usually a quoted argument, so this is
# checked on the whole command rather than per segment.
if [[ "${command}" =~ (^|[^[:alnum:]_])gh[[:space:]]+api([[:space:]]|$) && ( "${command}" =~ pulls/[0-9]+/merge || "${command}" =~ mergePullRequest ) ]]; then
  deny "Claude never merges pull requests in this repository; the user merges."
fi

prefix='^(env|command|exec|nohup|sudo|time|nice)([[:space:]]+-[^[:space:]]*)*[[:space:]]+'
assignment='^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+'

while IFS= read -r segment; do
  segment=$(sed -E 's/^[[:space:]]+//' <<<"${segment}")
  # Drop leading VAR=value assignments and wrappers such as env or sudo, in any
  # order. Quoted assignment values were split off above.
  while [[ "${segment}" =~ ${prefix} || "${segment}" =~ ${assignment} ]]; do
    segment=${segment:${#BASH_REMATCH[0]}}
  done

  if [[ "${segment}" =~ ^task([[:space:]]|$) && "${segment}" =~ (^|[[:space:]])services:down([[:space:]]|$) ]]; then
    deny "task services:down deletes the shared local Unleash stack and its data. Leave it running; ask the user if it must be reset."
  fi
  if [[ "${segment}" =~ ^(docker[[:space:]]+compose|docker-compose)([[:space:]]|$) && "${segment}" =~ [[:space:]]down([[:space:]]|$) ]]; then
    deny "docker compose down stops the shared local Unleash stack. Leave it running; ask the user if it must be reset."
  fi
  if [[ "${segment}" =~ ^(docker[[:space:]]+compose|docker-compose)([[:space:]]|$) && "${segment}" =~ [[:space:]]rm([[:space:]]|$) && "${segment}" =~ [[:space:]](-[a-zA-Z]*v[a-zA-Z]*|--volumes)([[:space:]]|$) ]]; then
    deny "docker compose rm with volumes deletes the shared local Unleash stack's data. Leave it running; ask the user if it must be reset."
  fi
  if [[ "${segment}" =~ ^docker[[:space:]]+volume[[:space:]]+(rm|remove|prune)([[:space:]]|$) ]]; then
    deny "Removing Docker volumes can delete the shared local Unleash stack's data (releash_db-data). Ask the user first."
  fi
  if [[ "${segment}" =~ ^gh[[:space:]]+pr[[:space:]]+merge([[:space:]]|$) ]]; then
    deny "Claude never merges pull requests in this repository; the user merges."
  fi
done <<<"${segments}"

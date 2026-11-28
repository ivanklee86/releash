#!/usr/bin/env bash
# PostToolUse hook for Edit, Write and MultiEdit: formats an edited .go file with
# golangci-lint fmt, so the formatters in .golangci.yml apply as soon as the file
# changes. It never blocks the edit. A file that does not format, such as one
# with a syntax error, is reported back to Claude as context instead.
set -euo pipefail

file=$(jq -r '.tool_input.file_path // empty')
[[ "${file}" == *.go && -f "${file}" ]] || exit 0

# Run from the project root so golangci-lint finds .golangci.yml.
cd "${CLAUDE_PROJECT_DIR:-.}"
report() {
  jq -n --arg msg "$1" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $msg}}'
  exit 0
}
# golangci-lint fmt exits 0 on a file it cannot parse, so gofmt checks that
# first.
if ! out=$(gofmt -e -l "${file}" 2>&1 >/dev/null); then
  report "${file} does not parse, so it was not formatted: ${out}"
fi
if ! out=$(golangci-lint fmt "${file}" 2>&1); then
  report "golangci-lint fmt failed on ${file}: ${out}"
fi

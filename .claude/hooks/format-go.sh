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
# golangci-lint fmt exits 0 on a file it cannot parse and only prints a
# warning, so any output counts as a failure.
if ! out=$(golangci-lint fmt "${file}" 2>&1) || [[ -n "${out}" ]]; then
  jq -n --arg msg "golangci-lint fmt failed on ${file}: ${out}" \
    '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $msg}}'
fi

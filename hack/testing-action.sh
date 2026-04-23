#!/usr/bin/env bash

set -euo pipefail

echo "Running testing-action.sh"
echo "Repository: ${GITHUB_REPOSITORY:-unknown}"
echo "Event: ${GITHUB_EVENT_NAME:-unknown}"
echo "Ref: ${GITHUB_REF:-unknown}"

echo "✅ testing-action.sh finished successfully"

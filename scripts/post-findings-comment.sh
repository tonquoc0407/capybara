#!/usr/bin/env bash
# Posts a capybara findings gate result as a PR comment. Silent when the
# report is empty, matching the project's "no output on success" convention.
# The job's own exit status still comes from `findings --fail-on`, so a
# comment failure never masks a real gate failure.
set -euo pipefail

db="${1:?usage: post-findings-comment.sh <db> <fail-on> <pr-number>}"
fail_on="${2:?usage: post-findings-comment.sh <db> <fail-on> <pr-number>}"
pr="${3:?usage: post-findings-comment.sh <db> <fail-on> <pr-number>}"

set +e
report="$(capybara -db "$db" findings --fail-on "$fail_on")"
status=$?
set -e

if [ -n "$report" ]; then
  body="$(printf '### capybara agent findings\n\n```\n%s\n```\n' "$report")"
  gh pr comment "$pr" --body "$body"
fi

exit "$status"

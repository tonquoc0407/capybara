#!/bin/sh
# Score the detectors against the labelled corpus and fail on any regression.
# Imports every trace into a throwaway database, then evals against labels.json.
set -eu

dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bin=$(mktemp -d)/capybara
db=$(mktemp -u)
trap 'rm -f "$db" "$db"-wal "$db"-shm; rm -rf "$(dirname "$bin")"' EXIT

go build -o "$bin" ./cmd/capybara
for f in "$dir"/corpus/traces/*.jsonl; do
	"$bin" -db "$db" import "$f"
done
"$bin" -db "$db" eval --fail-under "${FAIL_UNDER:-1.0}" "$dir/corpus/labels.json"

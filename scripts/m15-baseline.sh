#!/usr/bin/env bash
# M15 Phase 1 baseline: run each gate, capture stdout+stderr and the real exit code.
# Usage: bash scripts/m15-baseline.sh <logdir>
set -u
LOGDIR="${1:?logdir}"
mkdir -p "$LOGDIR"
LOG="$LOGDIR/baseline-$(date +%Y%m%dT%H%M%S).log"
: > "$LOG"

run() { # run <label> <command...>
  local label="$1"; shift
  local start end status
  start=$(date +%s)
  {
    echo "### $label"
    echo "\$ $*"
  } >> "$LOG"
  "$@" >> "$LOG" 2>&1
  status=$?
  end=$(date +%s)
  echo "### exit=$status time=$((end-start))s" >> "$LOG"
  echo >> "$LOG"
  printf '%-34s exit=%-3s %ss\n' "$label" "$status" "$((end-start))"
  return $status
}

{
  echo "M15 baseline — $(date -Iseconds)"
  echo "host: $(uname -s -r -m)"
  echo "go:   $(go version)"
  echo "node: $(node --version 2>/dev/null)"
  echo "npm:  $(npm --version 2>/dev/null)"
  echo "sha:  $(git rev-parse HEAD)"
  echo "branch: $(git rev-parse --abbrev-ref HEAD)"
  echo
} >> "$LOG"

fail=0
run "go build ./..."          go build ./...          || fail=1
run "go vet ./..."            go vet ./...            || fail=1
run "go test ./... -count=1"  go test ./... -count=1  || fail=1
run "frontend app.test.cjs"   node internal/web/static/app.test.cjs || fail=1
run "govulncheck ./..."       govulncheck ./...       || fail=1

go build -o .m15/bin/tapelog.exe ./cmd/tapelog || fail=1
run "dogfood pass.yaml"  ./.m15/bin/tapelog.exe test --plain --junit .m15/junit.xml test-fixtures/scenarios/pass.yaml || fail=1

# fail.yaml is designed to fail: a zero exit here is the defect.
run "dogfood fail.yaml (must fail)" ./.m15/bin/tapelog.exe test --plain test-fixtures/scenarios/fail.yaml
fail_yaml_status=$?
if [ "$fail_yaml_status" -eq 0 ]; then
  echo "WARNING: fail.yaml exited 0 — the dogfood negative case did not fail" | tee -a "$LOG"
  fail=1
fi

echo
echo "log: $LOG"
if [ $fail -eq 0 ]; then echo "BASELINE: PASS"; else echo "BASELINE: FAIL"; fi
exit $fail

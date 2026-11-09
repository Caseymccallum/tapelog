#!/usr/bin/env bash
# Rebuild the regression demo's fictional recordings through Tapelog's own boundary.
#
#   bash examples/regression-demo/generate.sh
#
# Needs Go only. The regenerated files get fresh timestamps, session ids and hash
# heads, so they will not be byte-identical to the committed ones — the verdicts
# must be. Everything the demo cases depend on is asserted here: a valid complete
# session carrying two client-declared conversations, and an interrupted copy
# whose chain is intact but which has no session/end.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# `go build -o` is a native program: a Git Bash /tmp path is not a Windows path,
# so hand it a native one. On POSIX this is the same string.
native_work="$work"
if command -v cygpath >/dev/null 2>&1; then
  native_work="$(cygpath -w "$work")"
  native_here="$(cygpath -w "$here")"
else
  native_here="$here"
fi

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) exe=".exe" ;;
  *) exe="" ;;
esac

echo "== building tapelog and the hermetic fixture"
(cd "$root" && go build -o "$native_work/tapelog$exe" ./cmd/tapelog)
(cd "$root" && go build -o "$native_work/fakecmd$exe" ./internal/compat/fakecmd)

clean="$here/session-two-conversations.jsonl"
interrupted="$here/session-interrupted.jsonl"
native_clean="$native_here/session-two-conversations.jsonl"
native_interrupted="$native_here/session-interrupted.jsonl"

echo "== recording one session with two client-declared conversations"
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"regression-demo","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/fictional/alpha.md"},"_meta":{"io.tapelog/conversationId":"demo-alpha"}}}' \
  '{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"write_file","arguments":{"path":"/fictional/beta.md"},"_meta":{"io.tapelog/conversationId":"demo-beta"}}}' \
  | "$work/tapelog$exe" record --harness regression-demo --log "$native_clean" \
      -- "$native_work/fakecmd$exe" -era=2025-11-25 > "$work/responses.jsonl" 2> "$work/record.err"

echo "== asserting the clean recording is one complete valid session"
"$work/tapelog$exe" verify --require-structure "$native_clean" > "$work/verify-clean.txt"
grep -q "Structure: VALID" "$work/verify-clean.txt"
grep -q '"io.tapelog' "$work/record.err" 2>/dev/null && echo "unexpected attribution echo" || true
grep -q "demo-alpha" "$clean"
grep -q "demo-beta" "$clean"
grep -q '"session/end"' "$clean"

echo "== deriving the interrupted copy (no session/end was ever written)"
python3 - "$native_clean" "$native_interrupted" <<'PY'
import sys, pathlib
src, dst = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
lines = src.read_text(encoding="utf-8").splitlines()
if not lines or '"session/end"' not in lines[-1]:
    sys.exit("the clean recording does not end with session/end; cannot derive the interrupted copy")
dst.write_text("\n".join(lines[:-1]) + "\n", encoding="utf-8")
if "session/end" in dst.read_text(encoding="utf-8"):
    sys.exit("the interrupted copy still contains session/end")
print(f"   {len(lines)} events -> {len(lines)-1} (last event dropped)")
PY

echo "== asserting the interrupted copy keeps an intact chain but lost its end"
"$work/tapelog$exe" verify "$native_interrupted" | grep -q "chain intact"
"$work/tapelog$exe" verify "$native_interrupted" | grep -q "INCOMPLETE — no_end"
if "$work/tapelog$exe" verify --require-structure "$native_interrupted" > /dev/null 2>&1; then
  echo "FAIL: the interrupted copy passed the structure gate; the demo's INCONCLUSIVE case would be meaningless" >&2
  exit 1
fi

echo "== running the demo suite"
set +e
"$work/tapelog$exe" lab suite --dir "$native_here" --suite demo-suite.json
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL: the demo suite exited $status; the documented outcome is 1 (one expected FAIL case)" >&2
  exit 1
fi

echo
echo "Regenerated. The demo suite reports 2 pass, 1 fail, 2 inconclusive — as documented in README.md."

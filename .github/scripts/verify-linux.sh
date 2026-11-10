#!/usr/bin/env bash
# Verifies the Linux archive the release workflow just built, by executing it.
# Runs on ubuntu-latest; also runnable locally against a built dist.
set -euo pipefail

dist="${1:-dist}"
port="${2:-8989}"
expected="$(git rev-parse HEAD)"
[ -n "$expected" ] || { echo "cannot resolve the checked-out commit" >&2; exit 1; }

archive="$(ls "$dist"/*linux_amd64.tar.gz | head -1)"
[ -n "$archive" ] || { echo "no linux archive in $dist" >&2; exit 1; }
(cd "$dist" && sha256sum -c checksums.txt)

tar -tzf "$archive" | grep -q 'docs/GETTING_STARTED.md' || { echo "docs/GETTING_STARTED.md missing" >&2; exit 1; }
tar -tzf "$archive" | grep -q 'docs/SECURITY-AND-LIMITATIONS.md' || { echo "docs/SECURITY-AND-LIMITATIONS.md missing" >&2; exit 1; }
tar -tzf "$archive" | grep -q 'examples/regression-demo/demo-suite.json' || { echo "demo suite missing" >&2; exit 1; }
tar -tzf "$archive" | grep -q 'MANIFEST.txt' || { echo "MANIFEST.txt missing" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
tar -xzf "$archive" -C "$work"
cd "$work"/*/

test -x tapelog || { echo "tapelog is not executable in the tar" >&2; exit 1; }
version="$(./tapelog version)"
echo "version: $version"
echo "$version" | grep -F "($expected)" > /dev/null || { echo "version does not name the released commit $expected" >&2; exit 1; }
grep -F "commit: $expected" MANIFEST.txt > /dev/null || { echo "MANIFEST.txt does not name $expected" >&2; exit 1; }

./tapelog demo --dir demo-run
./tapelog verify demo-run/recordings/*.jsonl | grep -F 'chain intact' > /dev/null \
  || { echo "verify did not report an intact chain" >&2; exit 1; }

set +e
./tapelog lab suite --dir examples/regression-demo --suite demo-suite.json > lab.log 2>&1
code=$?
set -e
if [ "$code" != "1" ]; then echo "bundled suite exited $code, expected 1 (a deliberate FAIL)" >&2; cat lab.log; exit 1; fi
grep -F '2 pass, 1 fail, 2 inconclusive' lab.log > /dev/null || { echo "suite summary missing" >&2; exit 1; }

./tapelog web --dir examples/regression-demo --listen "127.0.0.1:$port" > web.log 2>&1 &
webpid=$!
trap 'kill "$webpid" 2>/dev/null || true; rm -rf "$work"' EXIT
ready=false
for _ in $(seq 1 40); do
  if curl -fsS "http://127.0.0.1:$port/api/sessions" > /dev/null 2>&1; then ready=true; break; fi
  sleep 0.5
done
[ "$ready" = true ] || { echo "web server did not answer on 127.0.0.1:$port" >&2; cat web.log; exit 1; }
curl -fsS "http://127.0.0.1:$port/" | grep -F 'Your agent sessions' > /dev/null || { echo "workspace HTML not served" >&2; exit 1; }
curl -fsS "http://127.0.0.1:$port/app.js" | grep -F 'suite-case-list' > /dev/null || { echo "embedded workspace assets missing" >&2; exit 1; }
curl -fsS "http://127.0.0.1:$port/app.css" | grep -F '#behaviour-stats' > /dev/null || { echo "embedded stylesheet missing" >&2; exit 1; }

echo "linux archive verified"

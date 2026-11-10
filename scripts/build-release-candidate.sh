#!/usr/bin/env bash
# Build the M15 release candidate locally: stamped binaries, archives, checksums.
#
#   bash scripts/build-release-candidate.sh [output-dir]
#
# Mirrors what .goreleaser.yaml would produce (naming, archive formats, bundled
# files, checksums) so the candidate can be validated without the goreleaser binary.
# It does NOT produce SBOMs or signatures: those need the release workflow's OIDC
# identity, and this script never claims they exist.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$root/dist-m15}"
# Default to the version the code declares, so a release cannot be stamped with a
# stale string copied into a script. buildinfo is the single source of truth.
version="${TAPELOG_VERSION:-$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/buildinfo/buildinfo.go)}"
[ -n "$version" ] || { echo "could not read Version from internal/buildinfo/buildinfo.go" >&2; exit 1; }
# Full commit: the stamp is the only provenance a downloader can check, and a
# short SHA can collide where CI stamps the full one.
commit="$(cd "$root" && git rev-parse HEAD)"
dirty=""
if [ -n "$(cd "$root" && git status --porcelain)" ]; then
  dirty="-dirty"
  echo "WARNING: working tree is dirty; the archive will be marked $dirty" >&2
fi

rm -rf "$out"
mkdir -p "$out/bin"
# `go build -o` is a native program: an MSYS /c/... path is not a Windows path.
native_out="$out"
if command -v cygpath >/dev/null 2>&1; then native_out="$(cygpath -w "$out")"; fi
cd "$root"

echo "== manifest"
{
  echo "version: $version"
  echo "commit: $commit$dirty"
  echo "built: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go: $(go version)"
  signing="${TAPELOG_MANIFEST_SIGNING:-}"
  [ -n "$signing" ] || signing="NOT PERFORMED (cosign keyless needs the release workflow's OIDC identity)"
  sbom="${TAPELOG_MANIFEST_SBOM:-}"
  [ -n "$sbom" ] || sbom="NOT PRODUCED locally (the release workflow produces one per archive)"
  echo "signing: $signing"
  echo "sbom: $sbom"
} > "$out/MANIFEST.txt"
cat "$out/MANIFEST.txt"


echo "== building tapelog $version ($commit$dirty)"
# Platforms to build. Default is every configured target; a beta ships only the
# platforms that have been executed (see docs/GETTING_STARTED.md#supported-platforms).
#   TAPELOG_PLATFORMS="windows/amd64 linux/amd64" bash scripts/build-release-candidate.sh
targets=()
if [ -n "${TAPELOG_PLATFORMS:-}" ]; then
  for platform in $TAPELOG_PLATFORMS; do
    goos="${platform%%/*}"
    goarch="${platform##*/}"
    case "$goos" in
      windows) targets+=("$goos $goarch .exe zip") ;;
      *) targets+=("$goos $goarch '' tar.gz") ;;
    esac
  done
else
  # Only platforms that have been executed. Add a target here and in
  # .goreleaser.yaml together, after an acceptance pass on that platform.
  targets=(
    "windows amd64 .exe zip"
    "linux amd64 '' tar.gz"
  )
fi
for target in "${targets[@]}"; do
  read -r goos goarch exe ext <<<"$target"
  if [ "$exe" = "''" ]; then exe=""; fi
  bin="tapelog${exe}"
  name="tapelog_${version}_${goos}_${goarch}"
  echo "   $goos/$goarch -> $name.$ext"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/Caseymccallum/tapelog/internal/buildinfo.Version=${version} -X github.com/Caseymccallum/tapelog/internal/buildinfo.Commit=${commit}${dirty}" \
    -o "$native_out/bin/$bin" ./cmd/tapelog

  stage="$out/stage/$name"
  mkdir -p "$stage"
  chmod 0755 "$out/bin/$bin"
  cp "$out/bin/$bin" "$stage/"
  cp LICENSE README.md CHANGELOG.md THIRD_PARTY_NOTICES.md "$stage/"
  cp "$out/MANIFEST.txt" "$stage/"
  # The first-run guides ship with the binary: a download-only user has no checkout,
  # and the privacy guidance has to be readable before the first recording.
  # Every guide the shipped docs link to, so a downloader is not sent to a 404.
  mkdir -p "$stage/docs/adr"
  cp docs/*.md "$stage/docs/"
  cp docs/adr/*.md "$stage/docs/adr/" 2>/dev/null || true
  cp SECURITY.md ARCHITECTURE.md CONTRIBUTING.md GOVERNANCE.md CODE_OF_CONDUCT.md RESEARCH.md STACK.md "$stage/"
  # The Getting Started walkthrough ends with the fictional Regression Lab suite. A
  # download-only user has no source checkout, so the suite travels with the archive.
  mkdir -p "$stage/examples/regression-demo"
  cp examples/regression-demo/*.json examples/regression-demo/*.jsonl      examples/regression-demo/README.md examples/regression-demo/generate.sh "$stage/examples/regression-demo/"
  if [ "$ext" = "zip" ]; then
    # No `zip` binary on a stock Git Bash host; Python's zipfile is portable.
    native_stage="$out/stage"
    if command -v cygpath >/dev/null 2>&1; then native_stage="$(cygpath -w "$out/stage")"; fi
    python - "$native_out/$name.zip" "$native_stage" "$name" <<'PYZIP'
import os, sys, zipfile
target, root, base = sys.argv[1], sys.argv[2], sys.argv[3]
with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as z:
    for dirpath, _, files in os.walk(os.path.join(root, base)):
        for f in sorted(files):
            full = os.path.join(dirpath, f)
            z.write(full, os.path.relpath(full, root))
PYZIP
  else
    # MSYS tar records 0644 for everything on NTFS, which ships a non-executable
    # binary ("Permission denied" on first run). Python's tarfile sets the mode.
    python - "$native_out/$name.tar.gz" "$native_stage" "$name" <<'PYTAR'
import os, sys, tarfile
target, root, base = sys.argv[1], sys.argv[2], sys.argv[3]
with tarfile.open(target, "w:gz") as t:
    for dirpath, _, files in os.walk(os.path.join(root, base)):
        for f in sorted(files):
            full = os.path.join(dirpath, f)
            info = t.gettarinfo(full, os.path.relpath(full, root))
            info.mode = 0o755 if f == "tapelog" else 0o644
            with open(full, "rb") as fh:
                t.addfile(info, fh)
PYTAR
  fi
  rm -rf "$stage"
done

echo "== checksums"
(cd "$out" && sha256sum tapelog_*.zip tapelog_*.tar.gz > checksums.txt)
cat "$out/checksums.txt"

rm -rf "$out/stage"
echo
echo "candidate written to $out"

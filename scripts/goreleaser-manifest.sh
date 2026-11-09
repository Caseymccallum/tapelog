#!/usr/bin/env bash
# Write MANIFEST.txt for a goreleaser build.
#
# Runs from goreleaser's `before.hooks` step, so the file exists when the
# archives are assembled and travels inside each of them. The CI build is the
# one that is signed, so the manifest says so; the local candidate script
# writes its own, weaker manifest.
set -euo pipefail

version="${GORELEASER_CURRENT_TAG:-}"
version="${version#v}"
if [ -z "$version" ]; then
  version="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/buildinfo/buildinfo.go)"
fi
[ -n "$version" ] || { echo "cannot determine version for MANIFEST.txt" >&2; exit 1; }

commit="$(git rev-parse HEAD)"
[ -n "$commit" ] || { echo "cannot determine commit for MANIFEST.txt" >&2; exit 1; }

# goreleaser runs this hook for snapshot builds too. Only the release workflow
# signs and SBOMs, so the manifest must not inherit the stronger claim there.
if [ -n "${CI:-}" ] && [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  signing="PERFORMED (cosign keyless over checksums.txt by the release workflow)"
  sbom="PRODUCED per archive (syft/SPDX)"
else
  signing="NOT PERFORMED (local goreleaser build; the release workflow signs it)"
  sbom="NOT PRODUCED locally (the release workflow produces one per archive)"
fi

{
  echo "version: $version"
  echo "commit: $commit"
  echo "built: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go: $(go version)"
  echo "signing: $signing"
  echo "sbom: $sbom"
} > MANIFEST.txt

cat MANIFEST.txt

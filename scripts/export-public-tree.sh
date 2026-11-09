#!/usr/bin/env bash
# Export the public source tree as a tarball, from HEAD, without touching history.
#
# Why not just push the branch: the internal history carries local machine paths in
# older revisions of documents, internal milestone records and model-review
# transcripts. A public reader can `git log -p` any of that. This script produces the
# subset that is meant to be public, as a single directory, so a public repository can
# be initialised from it with one commit — while the full history stays in the private
# bundle.
#
#   bash scripts/export-public-tree.sh [out-dir]
#
# It refuses to run on a dirty tree: an export that does not correspond to a commit
# cannot be reproduced.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$root/dist-public}"

cd "$root"
if [ -n "$(git status --porcelain)" ]; then
  echo "refusing: working tree is dirty; commit first so the export matches a SHA" >&2
  exit 1
fi
sha="$(git rev-parse HEAD)"

# Paths that are public. Everything else is excluded by omission — a whitelist, so a
# new internal document is private until someone adds it here on purpose.
include_paths=(
  .gitignore .gitattributes
  LICENSE README.md CHANGELOG.md THIRD_PARTY_NOTICES.md
  CODE_OF_CONDUCT.md CONTRIBUTING.md SECURITY.md
  go.mod go.sum .goreleaser.yaml
  cmd internal adapters packs spec scripts tools/gen-vectors tools/gen-m9-navigation
  examples test-fixtures
  .github
  ARCHITECTURE.md RESEARCH.md STACK.md GOVERNANCE.md
  docs/adr docs/RESEARCH-EVOLUTION.md
  docs/BEHAVIOUR_CLI.md docs/CHECKPOINTS.md docs/CLIENT-SETUP.md
  docs/CONVERSATION-ATTRIBUTION.md docs/FIRST-SESSION.md docs/GETTING_STARTED.md
  docs/HERMES-ATTRIBUTION.md docs/M9-EVIDENCE-SEMANTICS.md docs/PLUGINS.md docs/POLICY.md
  docs/QUICKSTART.md docs/REGRESSION-LAB.md docs/RESEARCH-EVOLUTION.md docs/ROADMAP.md
  docs/SCHEMA.md docs/SECURITY-AND-LIMITATIONS.md docs/TESTING.md docs/THREAT_MODEL.md
  docs/WORKBENCH.md
)

rm -rf "$out"
mkdir -p "$out"

# git archive with explicit pathspecs: only tracked files, exact bytes, no .git.
existing=()
for path in "${include_paths[@]}"; do
  if git cat-file -e "HEAD:$path" 2>/dev/null; then
    existing+=("$path")
  fi
done

git archive --format=tar "$sha" "${existing[@]}" | tar -x -C "$out"

{
  echo "exported-from: $sha"
  echo "exported-at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "files: $(find "$out" -type f | wc -l)"
  echo "excluded-by-design: docs/releases/ docs/reviews/ docs/launch/ tools/hermes-observer/ .m15/ dist-m15/"
  echo "included paths:"
  printf '  %s\n' "${existing[@]}"
} > "$out/PUBLIC-TREE.txt"

echo "exported $sha -> $out ($(find "$out" -type f | wc -l) files)"
echo "manifest: $out/PUBLIC-TREE.txt"

#!/usr/bin/env bash
# Build the public-beta publication candidate: one snapshot commit on top of the
# existing public main, with the whitelisted export as its tree.
#
#   bash scripts/prepare-public-branch.sh [branch] [--no-fetch]
#
# What this does NOT do: push, merge, tag, rewrite or touch any existing ref. It
# writes one new local branch and prints exactly what a push of that branch would
# make reachable.
#
# Why this shape: the development history contains material that must not be
# published (authentic recordings with machine-specific paths, internal records).
# A snapshot whose parent is the already-public `main` exposes the public commits plus
# this one tree — nothing from the private line.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
branch="${1:-public-beta}"
do_fetch=1
for arg in "$@"; do
  [ "$arg" = "--no-fetch" ] && do_fetch=0
done

cd "$root"

if [ -n "$(git status --porcelain)" ]; then
  echo "refusing: working tree is dirty; commit so the export and the branch agree" >&2
  exit 1
fi

if [ "$do_fetch" -eq 1 ]; then
  echo "== fetching the public base"
  git fetch --quiet origin main
fi
base="$(git rev-parse origin/main)"
echo "== public base: $(git log -1 --format='%h %s' "$base")"

echo "== exporting the public tree from HEAD"
bash scripts/export-public-tree.sh >/dev/null
export_dir="$root/dist-public"
[ -f "$export_dir/PUBLIC-TREE.txt" ] || { echo "export missing" >&2; exit 1; }

# The export manifest names the private commit it came from. A public branch should not
# carry a pointer to an unpublished commit, so the published copy says only that.
if [ -f "$export_dir/PUBLIC-TREE.txt" ]; then
  sed -i 's/^exported-from: .*/exported-from: (source snapshot; see this commit)/' "$export_dir/PUBLIC-TREE.txt"
fi

echo "== building the snapshot commit (tree = export, parent = public main)"
# A path that does not exist yet: git refuses an empty file as an index.
tmp_dir="$(mktemp -d)"
tmp_index="$tmp_dir/index"
trap 'rm -rf "$tmp_dir"' EXIT
# Build the tree from the export directory in its own index. -f because the export
# contains fixture *.jsonl files that the repository's ignore rules would otherwise skip.
# Native paths: git here is a native binary, and an MSYS /c/... path in GIT_DIR or
# GIT_WORK_TREE fails with "not a git repository".
native() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }
(
  cd "$export_dir"
  GIT_DIR="$(native "$root/.git")" GIT_WORK_TREE="$(native "$export_dir")"     GIT_INDEX_FILE="$(native "$tmp_index")" git add -A -f .
)
tree="$(GIT_INDEX_FILE="$(native "$tmp_index")" git write-tree)"
# The message deliberately carries no private SHA: it becomes public history.
message="$(printf 'Tapelog 0.5.0-beta.1 — public beta source snapshot\n\nFirst public snapshot of the project, published as a single commit on top of the\nexisting public main. Apache-2.0; see LICENSE and THIRD_PARTY_NOTICES.md.\n\nStart with docs/GETTING_STARTED.md — the beta ships prebuilt archives, and\n`tapelog demo` runs a complete fictional session with no agent or account.\n')"
commit="$(git commit-tree "$tree" -p "$base" -m "$message")"
git branch -f "$branch" "$commit"

echo
echo "== branch $branch -> $(git rev-parse --short "$commit")"
echo "   commits it adds on top of public main: $(git rev-list --count "$base..$commit")"
echo "   files in the snapshot: $(git ls-tree -r --name-only "$commit" | wc -l)"
echo
echo "== what a push of this branch would make reachable, beyond what is already public"
git rev-list --objects "$commit" "^$base" | wc -l | sed 's/^/   objects: /'
echo
echo "== guarding the publication decision"
if git rev-list "$commit" | grep -qx "$(git rev-parse HEAD)"; then
  echo "   PROBLEM: the snapshot commit is the private HEAD — this must never happen" >&2
  exit 1
fi
if git merge-base --is-ancestor HEAD "$commit"; then
  echo "   PROBLEM: the private history is an ancestor of the publication commit" >&2
  exit 1
fi
echo "   private history is NOT an ancestor of $branch ✓"
echo "   parent chain: $(git log --oneline "$commit" | tail -3 | tr '\n' ' ')"
echo
echo "Nothing has been pushed. To publish, the owner runs:"
echo "   git push origin $branch:main     # after review, and only with explicit approval"

## What

<!-- What does this change do? Link issues with "Fixes #123". -->

## Why

<!-- Which problem does it solve? Feature requests: link the issue. -->

## Checklist

- [ ] `go test ./...` passes locally (ideally also `go test -race` on Linux)
- [ ] New behavior has tests; docs updated (POLICY.md / TESTING.md / CHANGELOG)
- [ ] Policy/pack changes: run `tapelog fuzz --policy <pack> <cassette>` too
- [ ] I ran `git status --ignored` and committed any new test fixtures
      (`.gitignore` ignores `*.jsonl` — fixtures need an explicit exception)

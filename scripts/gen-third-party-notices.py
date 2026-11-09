"""Generate THIRD_PARTY_NOTICES.md from the module cache.

Apache-2.0 and MIT both require the notice to travel with a binary distribution, and
one dependency (`mattn/go-localereader`) ships no LICENSE file inside its module zip,
so the file has to be reconstructed from the upstream repository. This script reads
every module's licence from the cache, classifies it, and captures the copyright line
so the archive carries something a lawyer can read.
"""

from __future__ import annotations

import json
import pathlib
import re
import subprocess

# Derived from the script location: a hard-coded checkout path in a shipped
# script is a disclosure, and it breaks on every other machine.
ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "THIRD_PARTY_NOTICES.md"

LICENSE_FILES = ("LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING", "COPYING.txt", "NOTICE")


def classify(text: str) -> str:
    low = text.lower()
    if "apache license" in low:
        return "Apache-2.0"
    if "permission is hereby granted, free of charge" in low:
        return "MIT"
    if "redistribution and use in source and binary forms" in low:
        return "BSD-3-Clause" if "neither the name" in low else "BSD-2-Clause"
    if "mozilla public license" in low:
        return "MPL-2.0"
    if "isc license" in low or "permission to use, copy, modify, and/or distribute" in low:
        return "ISC"
    return "UNKNOWN"


def read_licence(d: pathlib.Path) -> tuple[str, str]:
    for name in LICENSE_FILES:
        p = d / name
        if p.exists():
            try:
                text = p.read_text(encoding="utf-8", errors="replace")
            except Exception:
                continue
            m = re.search(r"Copyright[^\n]{0,200}", text)
            return classify(text), (m.group(0).strip() if m else "")
    return "", ""


def modules() -> list[dict]:
    raw = subprocess.run(["go", "list", "-m", "-json", "all"], cwd=ROOT, capture_output=True, text=True).stdout
    dec, i, out = json.JSONDecoder(), 0, []
    while i < len(raw):
        while i < len(raw) and raw[i] in " \r\n\t":
            i += 1
        if i >= len(raw):
            break
        obj, j = dec.raw_decode(raw, i)
        out.append(obj)
        i = j
    return out


# Reconstructed from https://github.com/mattn/go-localereader (module zip omits it).
MANUAL = {
    "github.com/mattn/go-localereader": ("MIT", "Copyright (c) 2022 Yasuhiro Matsumoto",
                                        "LICENSE absent from the module zip; read from the upstream repository at https://github.com/mattn/go-localereader"),
}


def main() -> int:
    rows = []
    for m in modules():
        if m.get("Main"):
            continue
        path, version = m["Path"], m.get("Version", "")
        label, copyright_line = read_licence(pathlib.Path(m.get("Dir", "")))
        note = ""
        if not label and path in MANUAL:
            label, copyright_line, note = MANUAL[path]
        rows.append({"path": path, "version": version, "licence": label or "UNKNOWN",
                     "copyright": copyright_line, "note": note})
    rows.sort(key=lambda r: r["path"])

    unknown = [r for r in rows if r["licence"] == "UNKNOWN"]
    counts: dict[str, int] = {}
    for r in rows:
        counts[r["licence"]] = counts.get(r["licence"], 0) + 1

    lines = [
        "# Third-party notices",
        "",
        "Tapelog is Apache-2.0. Its binary distributions include the following Go modules,",
        "each under its own licence. Generated from the module cache by",
        "`scripts/gen-third-party-notices.py`; re-run it after any dependency change.",
        "",
        "Summary: " + ", ".join(f"{k} {v}" for k, v in sorted(counts.items())) + ".",
        "",
    ]
    if unknown:
        lines += [f"**{len(unknown)} module(s) could not be classified** — see the end of this file.", ""]
    lines += ["| Module | Version | Licence |", "|---|---|---|"]
    for r in rows:
        lines.append(f"| `{r['path']}` | {r['version']} | {r['licence']} |")
    lines += ["", "## Copyright notices", ""]
    for r in rows:
        if r["copyright"]:
            lines.append(f"- `{r['path']}` — {r['copyright']}")
    manual = [r for r in rows if r["note"]]
    if manual:
        lines += ["", "## Reconstructed entries", ""]
        for r in manual:
            lines.append(f"- `{r['path']}` — {r['note']}")
    if unknown:
        lines += ["", "## Unclassified modules", ""]
        for r in unknown:
            lines.append(f"- `{r['path']}` ({r['version']}) — no licence file found in the module cache")
    lines += [
        "",
        "## Obligations",
        "",
        "Apache-2.0 and MIT both require this notice to accompany binary distributions.",
        "Archives built by `scripts/build-release-candidate.sh` include this file and the",
        "project `LICENSE`. Nothing in this file is legal advice.",
        "",
    ]
    OUT.write_text("\n".join(lines), encoding="utf-8")
    print(f"wrote {OUT} with {len(rows)} modules; unknown: {len(unknown)}")
    for r in unknown:
        print("  UNKNOWN:", r["path"], r["version"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

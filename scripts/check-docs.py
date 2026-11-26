#!/usr/bin/env python3
"""Check the repository's Markdown documents without network or build tools."""

import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parent.parent


def check(root):
    errors = []
    files = [root / "README.md", root / "AGENTS.md", *sorted((root / "docs").rglob("*.md"))]
    links = 0
    for file in files:
        label = file.relative_to(root)
        text = file.read_text(encoding="utf-8")
        if not text.startswith("# "):
            errors.append(f"{label}: missing top-level heading")
        fence = None
        body = []
        for number, line in enumerate(text.splitlines(), 1):
            marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
            if marker:
                run, rest = marker.groups()
                if fence is None:
                    fence = (run[0], len(run))
                elif run[0] == fence[0] and len(run) >= fence[1] and not rest.strip():
                    fence = None
                continue
            if fence is None:
                body.append((number, re.sub(r"(`+).*?\1", "", line)))
        if fence:
            errors.append(f"{label}: unclosed code fence")
        # The documentation uses inline Markdown links, including angle-bracket destinations.
        for number, line in body:
            for match in re.finditer(r"!?\[[^\]]*\]\(\s*(?:<([^>]+)>|([^\s)]+))(?:\s+\"[^\"]*\")?\s*\)", line):
                target = match.group(1) or match.group(2)
                url = urlsplit(target)
                if url.scheme or url.netloc:
                    continue
                path = unquote(url.path)
                destination = (root / path.lstrip("/")) if path.startswith("/") else (file.parent / path)
                links += 1
                if not destination.exists():
                    errors.append(f"{label}:{number}: missing link target: {target}")
    return files, links, errors


def main():
    files, links, errors = check(ROOT)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"Documentation OK: {len(files)} files, {links} local links; headings and code fences valid")
    return 0


if __name__ == "__main__":
    sys.exit(main())

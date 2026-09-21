#!/usr/bin/env python3
"""Compare two Homebrew formulas, ignoring the url and sha256 lines.

Exit 0 when the two are identical once every `url "..."` and `sha256 "..."`
line is blanked out, 1 when they differ, 2 on a usage problem.

Used by `test-sync-tap.sh`. It exists as a file rather than an inline one-liner
because the comparison needs a grouped alternation, and BSD `sed` rejects
`sed -E 's|(url|sha256) ".*"|\\1 "X"|'` with "parentheses not balanced" — which
made the assertion compare two empty streams and pass without testing anything.
Python is already a dependency of the release tooling, so this costs nothing.
"""

import pathlib
import re
import sys

PATTERN = re.compile(r'(url|sha256) ".*"')


def normalise(path: str) -> str:
    return PATTERN.sub(r'\1 "X"', pathlib.Path(path).read_text())


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(f"usage: {argv[0]} <formula-a> <formula-b>", file=sys.stderr)
        return 2
    try:
        return 0 if normalise(argv[1]) == normalise(argv[2]) else 1
    except OSError as err:
        print(f"{argv[0]}: {err}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))

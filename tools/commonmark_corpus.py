#!/usr/bin/env python3
"""Regenerate or check the pinned CommonMark corpus; Python 3.9+, stdlib only."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
CORPUS = ROOT / "internal/parser/testdata/commonmark/0.31.2"
INPUTS = {
    "upstream/spec.txt",
    "upstream/test/spec_tests.py",
    "upstream/test/cmark.py",
    "upstream/test/normalize.py",
    "upstream/LICENSE",
}
FIELDS = {"markdown", "html", "example", "section", "start_line", "end_line"}


def verify_bytes(path, expected):
    content = path.read_bytes()
    if len(content) != expected["byte_count"]:
        raise ValueError(f"{path.name}: byte count differs from provenance")
    if hashlib.sha256(content).hexdigest() != expected["sha256"]:
        raise ValueError(f"{path.name}: SHA-256 differs from provenance")
    return content


def generate():
    provenance = json.loads((CORPUS / "provenance.json").read_text(encoding="utf-8"))
    if set(provenance["inputs"]) != INPUTS:
        raise ValueError("provenance must identify the complete pinned input set")
    # Verify every imported helper before starting the upstream Python program.
    for name in sorted(INPUTS):
        verify_bytes(CORPUS / name, provenance["inputs"][name])
    result = subprocess.run(
        [
            # Upstream uses site's exit() helper; keep system site initialization.
            sys.executable, "-E", "-s", "-B",
            str(CORPUS / "upstream/test/spec_tests.py"),
            "--spec", str(CORPUS / "upstream/spec.txt"), "--dump-tests",
        ],
        cwd=CORPUS / "upstream",
        capture_output=True,
        check=True,
    )
    if result.stderr:
        # Newer interpreters warn about legacy escapes in the unmodified helper.
        # Diagnostics do not invalidate a successful, byte-verified extraction.
        sys.stderr.buffer.write(result.stderr)
    output = result.stdout
    expected = provenance["generated"]
    if (len(output) != expected["byte_count"]
            or hashlib.sha256(output).hexdigest() != expected["sha256"]):
        raise ValueError("upstream dump bytes differ from the pinned generated digest")
    cases = json.loads(output)
    if len(cases) != 652 or [case["example"] for case in cases] != list(range(1, 653)):
        raise ValueError("expected exactly 652 consecutive upstream examples")
    if len({case["section"] for case in cases}) != 26:
        raise ValueError("expected exactly 26 upstream sections")
    if any(set(case) != FIELDS for case in cases):
        raise ValueError("expected all six upstream fields on every example")
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check", action="store_true",
        help="fail on input or fixture drift without writing files",
    )
    args = parser.parse_args()
    try:
        output = generate()
        fixture = CORPUS / "examples.json"
        if args.check:
            if fixture.read_bytes() != output:
                raise ValueError(
                    "examples.json differs from upstream output; "
                    "run python3 tools/commonmark_corpus.py to regenerate"
                )
        else:
            fixture.write_bytes(output)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print(f"CommonMark corpus: {error}", file=sys.stderr)
        if isinstance(error, subprocess.CalledProcessError):
            print(error.stderr.decode("utf-8", errors="replace"), file=sys.stderr)
        return 1
    action = "checked" if args.check else "generated"
    print(f"CommonMark 0.31.2: {action} 652 examples across 26 sections")
    return 0


if __name__ == "__main__":
    sys.exit(main())

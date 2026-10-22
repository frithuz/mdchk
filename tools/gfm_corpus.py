#!/usr/bin/env python3
"""Regenerate or check original published GFM examples; Python 3.9+, stdlib only."""

import argparse
import hashlib
import html
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
CORPUS = ROOT / "internal/parser/testdata/gfm/0.29-gfm"
SNAPSHOT = "b153d814fdfc8624bb6da7449162c1cd707a637f7d1c1b636eb44b9cf63fa220"
SOURCE_SIZE = 508310
VERSION = 1
INPUTS = {"upstream/spec.html", "upstream/cmark-gfm-COPYING"}
EXTENSIONS = {
    **dict.fromkeys(range(198, 206), "tables"),
    **dict.fromkeys(range(279, 281), "task-list-items"),
    **dict.fromkeys(range(491, 494), "strikethrough"),
    **dict.fromkeys(range(622, 636), "extended-autolinks"),
    657: "disallowed-raw-html",
}


class Extractor(HTMLParser):
    """Strict example subtrees; decoded example data never re-enters the parser."""

    def __init__(self):
        super().__init__(convert_charrefs=False)
        self.cases = []
        self.stack = []
        self.case = None
        self.field = None
        self.parts = []
        self.heading = None
        self.heading_parts = []
        self.heading_skip = 0
        self.section = None

    def fail(self, message):
        raise ValueError(f"source HTML line {self.getpos()[0]}: {message}")

    def handle_starttag(self, tag, attrs):
        attributes = dict(attrs)
        if len(attributes) != len(attrs):
            # The pinned page has a duplicate style on its license image, outside
            # our authority-bearing subtrees. Reject ambiguity inside examples.
            if self.case is not None:
                self.fail("duplicate example attribute")
        classes = attributes.get("class", "").split()
        if tag == "div" and "example" in classes:
            if self.case is not None or self.heading is not None:
                self.fail("nested example container")
            identity = attributes.get("id", "")
            if not re.fullmatch(r"example-[1-9][0-9]*", identity):
                self.fail("missing or invalid example ID")
            if not self.section:
                self.fail("example has no section heading")
            self.case = {
                "example": int(identity[8:]), "section": self.section,
                "start_line": self.getpos()[0],
            }
            self.stack = [tag]
            return
        if self.case is not None:
            if self.field:
                if (tag != "span" or attrs != [("class", "space")]
                        or self.stack[-1] != "code"):
                    self.fail("unsupported nested code presentation markup")
            elif tag == "code":
                kind = attributes.get("class")
                if kind not in ("language-markdown", "language-html"):
                    self.fail("unsupported example code field")
                if self.stack[-1] != "pre":
                    self.fail("code field must be inside pre")
                self.field = "markdown" if kind == "language-markdown" else "html"
                if self.field in self.case:
                    self.fail(f"duplicate {self.field} code field")
                self.parts = []
            elif tag not in ("div", "pre", "a"):
                self.fail("unsupported example container markup")
            self.stack.append(tag)
        elif re.fullmatch(r"h[1-6]", tag) and attributes.get("id"):
            if self.heading is not None:
                self.fail("nested section heading")
            self.heading = tag
            self.heading_parts = []
        elif self.heading is not None and tag == "span" and "number" in classes:
            self.heading_skip += 1

    def handle_endtag(self, tag):
        if self.case is not None:
            if not self.stack or self.stack[-1] != tag:
                self.fail(f"unbalanced example closing tag {tag}")
            self.stack.pop()
            if tag == "code":
                self.case[self.field] = "".join(self.parts).replace("→", "\t")
                self.field = None
                self.parts = []
            if not self.stack:
                if not {"markdown", "html"} <= self.case.keys():
                    self.fail("missing Markdown or HTML code field")
                self.case["end_line"] = self.getpos()[0]
                self.cases.append(self.case)
                self.case = None
        elif self.heading is not None:
            if tag == "span" and self.heading_skip:
                self.heading_skip -= 1
            elif tag == self.heading:
                self.section = "".join(self.heading_parts).strip()
                self.heading = None

    def handle_data(self, data):
        if self.field:
            self.parts.append(data)
        elif self.heading is not None and not self.heading_skip:
            self.heading_parts.append(data)

    def handle_entityref(self, name):
        self.handle_data(html.unescape("&" + name + ";"))

    def handle_charref(self, name):
        self.handle_data(html.unescape("&#" + name + ";"))

    def handle_startendtag(self, tag, attrs):
        if self.case is not None:
            self.fail("unsupported self-closing example markup")

    def handle_comment(self, data):
        if self.case is not None:
            self.fail("unsupported example comment")

    def unknown_decl(self, data):
        if self.case is not None:
            self.fail("unsupported example declaration")

    def handle_decl(self, decl):
        if self.case is not None:
            self.fail("unsupported example declaration")


def extract(source, expected_count=677):
    """Pure structural extraction, separately testable behind the production pin."""
    parser = Extractor()
    parser.feed(source.decode("utf-8"))
    parser.close()
    if parser.case is not None or parser.heading is not None:
        raise ValueError("truncated example container or section heading")
    cases = parser.cases
    if [case["example"] for case in cases] != list(range(1, expected_count + 1)):
        raise ValueError(f"expected {expected_count} consecutive unique example IDs")
    for case in cases:
        case["snapshot_sha256"] = SNAPSHOT
        case["input_sha256"] = hashlib.sha256(case["markdown"].encode("utf-8")).hexdigest()
        case["scope"] = EXTENSIONS.get(case["example"], "base")
    if expected_count == 677:
        counts = {scope: sum(c["scope"] == scope for c in cases)
                  for scope in ("base", *set(EXTENSIONS.values()))}
        if counts != {"base": 649, "tables": 8, "task-list-items": 2,
                      "strikethrough": 3, "extended-autolinks": 14,
                      "disallowed-raw-html": 1}:
            raise ValueError("published extension classification differs")
    return cases


def serialize(cases):
    return (json.dumps(cases, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def verify_bytes(path, expected):
    content = path.read_bytes()
    if len(content) != expected["byte_count"]:
        raise ValueError(f"{path.name}: byte count differs from provenance")
    if hashlib.sha256(content).hexdigest() != expected["sha256"]:
        raise ValueError(f"{path.name}: SHA-256 differs from provenance")
    return content


def generate():
    # The approved page pin is fixed independently of editable provenance.
    source = verify_bytes(CORPUS / "upstream/spec.html",
                          {"byte_count": SOURCE_SIZE, "sha256": SNAPSHOT})
    provenance = json.loads((CORPUS / "provenance.json").read_bytes())
    if set(provenance["inputs"]) != INPUTS:
        raise ValueError("provenance must identify the complete pinned input set")
    for name in sorted(INPUTS):
        verify_bytes(CORPUS / name, provenance["inputs"][name])
    if provenance["generator"]["version"] != VERSION:
        raise ValueError("extractor version differs from provenance")
    verify_bytes(Path(__file__), provenance["generator"])
    output = serialize(extract(source))
    # A second independent invocation guards accidental state/nondeterminism.
    if output != serialize(extract(source)):
        raise ValueError("extraction output is nondeterministic")
    expected = provenance["generated"]
    if (len(output) != expected["byte_count"]
            or hashlib.sha256(output).hexdigest() != expected["sha256"]):
        raise ValueError("generated bytes differ from the pinned generated digest")
    return output


def main():
    arguments = argparse.ArgumentParser(description=__doc__)
    arguments.add_argument("--check", action="store_true",
                           help="fail on source or fixture drift without writing files")
    args = arguments.parse_args()
    try:
        output = generate()
        fixture = CORPUS / "examples.json"
        if args.check:
            if fixture.read_bytes() != output:
                raise ValueError("examples.json differs; run python3 -B tools/gfm_corpus.py to regenerate")
        else:
            fixture.write_bytes(output)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"GFM corpus: {error}", file=sys.stderr)
        return 1
    action = "checked" if args.check else "generated"
    print(f"GFM 0.29-gfm: {action} 677 original examples (649 base, 28 extension)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

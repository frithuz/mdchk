#!/usr/bin/env python3
"""Check current command-guide examples and scoped local documentation links.

Only the published disposable example builds a binary. All other probes execute
that same ordinary binary, in disposable projects. External URLs are not fetched.
"""

import argparse
import hashlib
import json
import os
import re
import shlex
import shutil
import subprocess
import tempfile
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
INVENTORY = ROOT / "tools/testdata/cli-documentation.json"
PAGES = [
    "README.md", "docs/index.md", "docs/configuration.md", "docs/directives.md",
    "docs/file-selection.md", "docs/output-formats.md", "docs/exit-codes.md",
    "docs/markdown-formats.md", "docs/troubleshooting.md", "docs/contributing.md",
    "docs/rules/index.md", "docs/rules/whitespace/index.md",
    "docs/rules/whitespace/md047.md", "docs/engineering/configuration-examples.md",
    "docs/engineering/limits.md",
]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def fences(text):
    result, opened, body = [], None, []
    for line in text.splitlines(keepends=True):
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})([^\n]*)\n?$", line)
        if opened is None and marker:
            run, language = marker.groups()
            opened = (run[0], len(run), language.strip())
            body = []
        elif opened is not None:
            if marker and marker[1][0] == opened[0] and len(marker[1]) >= opened[1] and not marker[2].strip():
                result.append((opened[2], "".join(body)))
                opened = None
            else:
                body.append(line)
    require(opened is None, "unclosed documentation fence")
    return result


def inventory(root=ROOT, inventory_path=INVENTORY):
    entries = json.loads(inventory_path.read_text())
    actual = {(str(p.relative_to(root)), i): (language, source)
              for p in sorted((root / "docs/commands").rglob("*.md"))
              for i, (language, source) in enumerate(fences(p.read_text()))}
    expected = {(e["document"], e["fence"]): (e["language"], e["source"])
                for e in entries}
    require(len(expected) == len(entries), "duplicate command fence inventory entry")
    require(actual == expected,
            "command fences changed: review and cover each added/changed example in "
            "tools/testdata/cli-documentation.json")
    for e in entries:
        kind = e["classification"]
        require(kind in {"syntax", "future-json", "disposable-build", "argv", "future-argv"},
                f"unclassified fence: {e}")
        if kind in {"argv", "future-argv"}:
            require(len(e["source"].splitlines()) == len(e["exit_codes"]),
                    f"missing process expectation: {e}")
        elif kind == "future-json":
            json.loads(e["source"])
    require(sum(e["classification"] == "disposable-build" for e in entries) == 1,
            "expected one published build script")
    return entries


def prose(text):
    # Fence-aware, including tilde/long fences in linked reference pages.
    result, opened = [], None
    for line in text.splitlines():
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if marker:
            run, tail = marker.groups()
            if opened is None:
                opened = (run[0], len(run))
                continue
            if run[0] == opened[0] and len(run) >= opened[1] and not tail.strip():
                opened = None
                continue
        if opened is None:
            result.append(line)
    return "\n".join(result)


def anchors(text):
    found, counts = set(), {}
    for line in prose(text).splitlines():
        match = re.match(r"^ {0,3}#{1,6}\s+(.+?)(?:\s+#+)?\s*$", line)
        if not match:
            continue
        title = re.sub(r"\[([^]]+)\]\([^)]*\)", r"\1", match[1])
        title = re.sub(r"<[^>]*>", "", title).replace("`", "").lower()
        base = "".join(c for c in title if c.isalnum() or c in "_-" or c.isspace())
        base = re.sub(r"\s", "-", base)
        count = counts.get(base, 0)
        counts[base] = count + 1
        found.add(base + (f"-{count}" if count else ""))
    found.update(re.findall(r'<a\s+(?:name|id)=["\']([^"\']+)', text))
    return found


def check_links(root=ROOT, pages=None):
    if pages is None:
        pages = [root / p for p in PAGES] + sorted((root / "docs/commands").rglob("*.md"))
    count = 0
    for page in pages:
        for match in re.finditer(r"!?\[[^]\n]*\]\(([^)\n]+)\)", prose(page.read_text())):
            target = match[1].split(' "', 1)[0].strip("<>")
            parts = urlsplit(target)
            if parts.scheme or parts.netloc or target.startswith("/"):
                continue
            destination = (page.parent / unquote(parts.path)).resolve() if parts.path else page
            require(destination.is_relative_to(root.resolve()), f"link escapes checkout: {page}: {target}")
            require(destination.exists(), f"missing local link: {page}: {target}")
            if parts.fragment and destination.suffix == ".md":
                require(unquote(parts.fragment) in anchors(destination.read_text()),
                        f"missing heading anchor: {page}: {target}")
            count += 1
    print(f"PASS scoped local prose links/anchors: {count} links, {len(pages)} pages; external URLs not checked")


def state(project):
    return {str(p.relative_to(project)): (p.read_bytes() if p.is_file() else None,
                                             p.stat().st_dev, p.stat().st_ino,
                                             p.stat().st_mode, p.stat().st_mtime_ns)
            for p in project.rglob("*")}


def process_env(project):
    # A controlled Git-source fixture, independent of the operator's system or
    # global ignore policy. No user configuration is read or changed.
    env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
    env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=str(project / "absent-gitconfig"))
    return env


class Examples:
    def __init__(self, binary):
        self.binary = binary
        self.count = 0

    def run(self, project, argv, code, *, writes=False, json_result=False):
        before = state(project)
        got = subprocess.run([str(self.binary), *argv], cwd=project,
                             capture_output=True, timeout=40, check=False, env=process_env(project))
        require(got.returncode in (code if isinstance(code, tuple) else (code,)),
                f"{argv}: exit {got.returncode}, expected {code}: {got.stdout!r} {got.stderr!r}")
        require(not got.stderr, f"{argv}: unexpected stderr: {got.stderr!r}")
        if not writes:
            require(state(project) == before, f"{argv}: read-only command changed fixture")
        if isinstance(code, tuple) and got.returncode == 13:
            require(b"unsupported" in got.stdout.lower(), "unrelated IO error is not unsupported-adapter proof")
        self.count += 1
        print(f"PASS exit={got.returncode}: mdchk {shlex.join(argv)}")
        return json.loads(got.stdout) if json_result else got.stdout.decode()


def fixture(root, *, long=False):
    root.mkdir(parents=True, exist_ok=True)
    # These are the actual published assets, with an independent literal oracle.
    config = (ROOT / "docs/examples/editor-ci/.mdchk.yaml").read_bytes()
    source = (ROOT / "docs/examples/editor-ci/guide.md").read_bytes()
    require(source == "雪\t😀x".encode(), "published Unicode/EOF fixture changed")
    (root / ".mdchk.yaml").write_bytes(config)
    (root / "docs").mkdir()
    (root / "README.md").write_bytes(b"# Example\n")
    (root / "--draft.md").write_bytes(b"Draft\n")
    (root / "docs/guide.md").write_bytes(b"Line\n" * 40 if long else source)
    # Bound discovery to this disposable root, avoiding host ancestor policies.
    (root / ".git").mkdir()
    return source


def check_examples(entries):
    build = next(e for e in entries if e["classification"] == "disposable-build")
    # Execute the complete published script unchanged, then expose its temp path.
    script = build["source"] + '\nmdchk_example_status=$?\nprintf "\\nMDCHK_EXAMPLE_ROOT=%s\\n" "$mdchk_demo"\nexit "$mdchk_example_status"\n'
    built = subprocess.run(["sh", "-c", script], cwd=ROOT, capture_output=True,
                           timeout=120, check=False)
    require(built.returncode == 0 and not built.stderr, f"disposable example failed: {built}")
    lines = built.stdout.decode().splitlines()
    require("lint status: 15" in lines, "disposable example did not observe real incomplete status")
    reports = [json.loads(line) for line in lines if line.startswith('{"schema_version"')]
    require(len(reports) == 1 and reports[0]["status"] == "incomplete" and reports[0]["coverage"],
            "disposable example did not retain unavailable default coverage")
    require(lines[-1].startswith("MDCHK_EXAMPLE_ROOT="), "missing disposable binary path")
    build_root = Path(lines[-1].split("=", 1)[1]).resolve()
    ex = Examples(build_root / "mdchk")
    try:
        with tempfile.TemporaryDirectory(prefix="mdchk-docs-") as temp:
            base = Path(temp).resolve()
            ordinal = 0
            for entry in entries:
                if entry["classification"] not in {"argv", "future-argv"}:
                    continue
                for command, code in zip(entry["source"].splitlines(), entry["exit_codes"]):
                    project = base / str(ordinal)
                    ordinal += 1
                    fixture(project, long=True)
                    argv = shlex.split(command)
                    require(argv.pop(0) == "mdchk", f"unsupported documented executable: {command}")
                    argv = [a.replace("/path/to/project", str(project)) for a in argv]
                    # Clean native apply is still refused by unsupported adapters.
                    apply = argv[0] == "fix" and "--dry-run" not in argv
                    if apply:
                        code = (0, 13)  # Exact clean no-write, or verified unsupported adapter refusal.
                    print(f"Example {entry['document']} fence {entry['fence']}: {entry['classification']}")
                    result = ex.run(project, argv, code, json_result="json" in argv)
                    if isinstance(result, dict):
                        require(result["status"] == "clean", f"clean documented example: {result}")
                    elif entry["classification"] == "future-argv":
                        require("usage" in result.lower() and "mcp" in result.lower(), "MCP rejection missing")
                    else:
                        require(bool(result), f"empty ordinary text example: {command}")
            semantic_examples(ex, base)
        print(f"PASS all {len(entries)} command fences classified; {ex.count} compiled invocations plus published build script")
    finally:
        shutil.rmtree(build_root)


def semantic_examples(ex, base):
    project = base / "semantics"
    source = fixture(project)
    target = project / "docs/guide.md"
    digest = hashlib.sha256(source).hexdigest()
    for argv in (["--help"], ["--version"], ["lint", "--help"], ["fix", "--help"],
                 ["inspect", "--help"], ["config", "--help"], ["config", "validate", "--help"]):
        require(bool(ex.run(project, argv, 0)), "empty metadata")
    for format_name in ("text", "json", "vscode", "github"):
        output = ex.run(project, ["lint", "docs/guide.md", "--format", format_name], 10,
                        json_result=format_name == "json")
        if format_name == "json":
            require(output["status"] == "violations" and not output["coverage"], "MD047-only coverage")
            diagnostic, = output["diagnostics"]
            require(diagnostic["rule"] == "MD047" and diagnostic["range"]["start"]["line"] == 1
                    and diagnostic["range"]["start"]["column"] == 5
                    and diagnostic["snapshot"]["sha256"] == digest, "literal MD047 identity/position")
        elif format_name == "vscode":
            require(output == "docs/guide.md:1:5: error: MD047: File must end with a line-ending sequence.\n", "VS Code fixture")
        elif format_name == "github":
            require(output == "::error file=docs/guide.md,line=1,title=MD047::File must end with a line-ending sequence.\n", "GitHub fixture")
        preview = ex.run(project, ["fix", "docs/guide.md", "--dry-run", "--format", format_name],
                         0, json_result=format_name == "json")
        if isinstance(preview, dict):
            change, = preview["changes"]
            require(change["disposition"] == "proposed" and not preview["diagnostics"], "preview outcome")
            require(preview["files"][0]["result_snapshot"]["view"] == "candidate"
                    and preview["files"][0]["result_snapshot"]["sha256"] == hashlib.sha256(source + b"\n").hexdigest(), "preview snapshot")
        elif format_name in ("vscode", "github"):
            require(preview == "", "clean preview editor output")
        ex.run(project, ["config", "validate", ".", "--format", format_name], 0,
               json_result=format_name == "json")
    for format_name in ("text", "json"):
        got = ex.run(project, ["inspect", "docs/guide.md", "--format", format_name], 0,
                     json_result=format_name == "json")
        if isinstance(got, dict):
            require(got["status"] == "inspected" and got["diagnostics"][0]["rule"] == "MD047", "inspection observation")
    for format_name in ("vscode", "github"):
        ex.run(project, ["inspect", "docs/guide.md", "--format", format_name], 11)
    ex.run(project, ["mcp", "--help"], 11)
    ex.run(project, ["mcp", "--version"], 11)
    before_apply = state(project)
    applied = subprocess.run([str(ex.binary), "fix", "docs/guide.md", "--format", "json"],
                             cwd=project, capture_output=True, timeout=40, env=process_env(project))
    require(not applied.stderr and applied.returncode in (0, 13), "native apply contract")
    result = json.loads(applied.stdout)
    ex.count += 1
    if applied.returncode == 0:
        require(target.read_bytes() == source + b"\n", "native exact saved bytes")
        require(result["changes"][0]["disposition"] == "written"
                and result["files"][0]["result_snapshot"]["view"] == "saved", "native saved outcome")
        require(result["files"][0]["result_snapshot"]["sha256"] == hashlib.sha256(source + b"\n").hexdigest(), "saved hash")
        again = ex.run(project, ["fix", "docs/guide.md", "--format", "json"], 0, json_result=True)
        require(not again["changes"], "native idempotence")
        ex.run(project, ["lint", "docs/guide.md"], 0)
    else:
        require(state(project) == before_apply and result["errors"]
                and "unsupported" in applied.stdout.decode().lower(), "unsupported apply changed fixture or unrelated IO")
    print(f"PASS native apply status={applied.returncode}; supported exact save/idempotence or unsupported refusal")
    config = project / ".mdchk.yaml"
    config.unlink()
    default = ex.run(project, ["lint", "docs/guide.md", "--format", "json"], 15, json_result=True)
    require(default["coverage"] and not default["errors"] and default["checked_files"], "default GFM did not execute")
    config.write_text("version: 1\n")
    omitted = ex.run(project, ["lint", "docs/guide.md", "--format", "json"], 15, json_result=True)
    require(omitted["coverage"] and not omitted["errors"] and omitted["checked_files"], "omitted GFM did not execute")
    config.write_text("version: 1\nflavor: commonmark\n")
    incomplete = ex.run(project, ["lint", "docs/guide.md", "--format", "json"], 15, json_result=True)
    require(incomplete["coverage"] and not incomplete["complete"], "missing default coverage")
    observed = ex.run(project, ["inspect", "docs/guide.md", "--format", "json"], 0, json_result=True)
    require(observed["status"] == "inspected" and not observed["complete"], "incomplete observation is not validation")
    for settings in ("rules: {single-trailing-newline: {enabled: true}}", "rules: {md047: {enabled: true}}",
                     "rules: {MD001: {enabled: true}}", "frontmatter: {enabled: true}", "flavor: pandoc"):
        config.write_text("version: 1\n" + ("" if settings.startswith("flavor:") else "flavor: commonmark\n") + settings + "\n")
        error = ex.run(project, ["config", "validate", ".", "--format", "json"], 12, json_result=True)
        require(error["errors"][0].get("range") and error["errors"][0].get("path"), "located configuration error")
        ex.run(project, ["lint", "docs/guide.md", "--format", "json"], 12, json_result=True)
    for format_name in ("text", "json", "vscode", "github"):
        located = ex.run(project, ["config", "validate", ".", "--format", format_name], 12,
                         json_result=format_name == "json")
        if isinstance(located, str):
            require("config-definition" in located and ".mdchk.yaml" in located, "located definition reporting")
    observation = ex.run(project, ["inspect", "docs/guide.md", "--format", "json"], 0, json_result=True)
    require(observation["status"] == "inspected" and observation["errors"], "unavailable flavor is inspection observation")
    discovery_examples(ex, base)


def discovery_examples(ex, base):
    root = base / "discovery"
    fixture(root)
    child = root / "docs"
    (child / ".mdchk.yaml").write_text("version: 1\nflavor: commonmark\nrules: {MD047: {enabled: false}}\n")
    bad = child / "bad"
    bad.mkdir()
    (bad / ".mdchk.yaml").write_text("version: [\n")
    lint = ex.run(root, ["lint", "docs/guide.md", "--format", "json"], 10, json_result=True)
    require(lint["diagnostics"][0]["rule"] == "MD047", "lint must use CWD definition")
    inspect = ex.run(root, ["inspect", "docs/guide.md", "--format", "json"], 0, json_result=True)
    require(not inspect["diagnostics"] and not inspect["complete"], "inspect must use target definition with missing defaults")
    scan = ex.run(root, ["config", "validate", ".", "--format", "json"], 12, json_result=True)
    require(scan["config_validation"]["invalid_definitions"] == 1
            and any(e.get("path", "").endswith("docs/bad/.mdchk.yaml") for e in scan["errors"]),
            "downward validation did not report unselected nested definition")


def self_test():
    require(prose('text\n```json\n[not a link](absent.md)\n```\n[link](ok.md)') == "text\n[link](ok.md)", "fence parser")
    require(anchors("# Heading `one`\n# Heading one\n```\n# Hidden\n```\n") == {"heading-one", "heading-one-1"}, "anchor conventions")
    with tempfile.TemporaryDirectory(prefix="mdchk-doc-negative-") as temp:
        root = Path(temp)
        (root / "docs/commands").mkdir(parents=True)
        page = root / "docs/commands/lint.md"
        entry = {"document": "docs/commands/lint.md", "fence": 0, "language": "sh",
                 "source": "echo build\n", "classification": "disposable-build"}
        manifest = root / "inventory.json"
        manifest.write_text(json.dumps([entry]))
        page.write_text("```sh\necho build\n```\n")
        inventory(root, manifest)
        for mutation in ("```sh\necho changed\n```\n", "```sh\necho build\n```\n```sh\nmdchk lint\n```\n", "~~~sh\nmdchk lint\n~~~\n"):
            page.write_text(mutation)
            try:
                inventory(root, manifest)
            except ValueError:
                pass
            else:
                raise ValueError("changed/uncovered command negative control passed")
        page.write_text("# Here\n\n[missing](absent.md)\n")
        for broken in ("# Here\n[missing](absent.md)\n", "# Here\n[missing](#absent)\n"):
            page.write_text(broken)
            try:
                check_links(root, [page])
            except ValueError:
                pass
            else:
                raise ValueError("broken local link/anchor negative control passed")
    print("PASS documentation verifier negative controls: changed command, uncovered backtick/tilde fence, broken link/anchor, fenced data and duplicate anchors")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--links-only", action="store_true")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return
    entries = inventory()
    check_links()
    if not args.links_only:
        check_examples(entries)


if __name__ == "__main__":
    main()

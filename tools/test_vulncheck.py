#!/usr/bin/env python3
"""Check the real vulnerability gate using disposable modules and local v1 databases."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile


def write(root, name, content):
    path = root / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def main():
    binary = str(Path(sys.argv[1]).resolve())
    checkout = Path(__file__).resolve().parent.parent
    go_directive = next(line for line in (checkout / "go.mod").read_text().splitlines()
                        if line.startswith("go "))
    with tempfile.TemporaryDirectory(prefix="mdchk-vulncheck-test-") as directory:
        root = Path(directory)
        write(root, "go.mod", f"module example.com/probe\n\n{go_directive}\n")
        write(root, "probe.go", '''package probe
import "strings"
func Clone(s string) string { return strings.Clone(s) }
''')
        write(root, "probe_test.go", '''package probe
import ("strings"; "testing")
func TestCompare(t *testing.T) {
    if strings.Compare("a", "b") != -1 { t.Fatal("comparison failed") }
}
''')
        stamp = "2026-01-01T00:00:00Z"
        for name, symbols in (("clean", []), ("called", ["Clone"]),
                              ("test-only", ["Compare"]), ("uncalled", ["ToUpper"])):
            db = root / name
            write(db, "index/db.json", json.dumps({"modified": stamp}))
            modules = []
            if symbols:
                identifier = "GO-2099-0001"
                modules = [{"path": "stdlib", "vulns": [
                    {"id": identifier, "modified": stamp, "fixed": "9.0.0"}]}]
                entry = {
                    "schema_version": "1.3.1", "id": identifier,
                    "modified": stamp, "published": stamp,
                    "summary": "Synthetic gate fixture, not a real vulnerability",
                    "details": "Synthetic applicable symbol for deterministic gate verification.",
                    "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
                                  "ranges": [{"type": "SEMVER", "events": [
                                      {"introduced": "0"}, {"fixed": "9.0.0"}]}],
                                  "ecosystem_specific": {"imports": [
                                      {"path": "strings", "symbols": symbols}]}}],
                    "database_specific": {"url": "https://example.invalid/synthetic-fixture"},
                }
                write(db, f"ID/{identifier}.json", json.dumps(entry))
            write(db, "index/modules.json", json.dumps(modules))

        def scan(database, *flags):
            result = subprocess.run(
                [binary, "-format=text", "-scan=symbol", "-test",
                 "-show=traces,version", "-db", (root / database).as_uri(),
                 *flags, "./..."], cwd=root, text=True, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, check=False, timeout=120,
            )
            return result.returncode, result.stdout

        for database in ("clean", "uncalled"):
            code, output = scan(database)
            if code != 0 or "No vulnerabilities found" not in output:
                raise AssertionError(f"{database} should pass (exit {code}):\n{output}")
            print(f"{database}: clean symbol scan passes.")
        for database, symbol in (("called", "Clone"), ("test-only", "Compare")):
            code, output = scan(database)
            if code != 3 or not all(value in output for value in (
                    "GO-2099-0001", f"strings.{symbol}", "Fixed in:", "go9.0")):
                raise AssertionError(f"{database} must fail with actionable finding (exit {code}):\n{output}")
            print(f"{database}: applicable strings.{symbol} fails with ID, trace and fixed version.")
        write(root / "broken", "index/modules.json", "invalid JSON")
        code, output = scan("broken")
        if code == 0 or "invalid character" not in output:
            raise AssertionError(f"database corruption must fail (exit {code}):\n{output}")
        print("Corrupt database fails with a diagnostic.")
        write(root, "probe.go", "package probe\nfunc Invalid(\n")
        code, output = scan("clean")
        if code == 0 or "probe.go" not in output:
            raise AssertionError(f"package loading must fail (exit {code}):\n{output}")
        print("Package loading failure fails with a source diagnostic.")


if __name__ == "__main__":
    main()

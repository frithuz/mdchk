"""Integration checks for faithful ingestion and non-rewriting drift detection."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
RELATIVE_CORPUS = Path("internal/parser/testdata/commonmark/0.31.2")


class CorpusTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="mdchk-corpus-test-")
        self.addCleanup(self.temporary.cleanup)
        temporary = Path(self.temporary.name)
        self.checkout = temporary / "checkout"
        self.corpus = self.checkout / RELATIVE_CORPUS
        shutil.copytree(ROOT / RELATIVE_CORPUS, self.corpus)
        self.tool = self.checkout / "tools/commonmark_corpus.py"
        self.tool.parent.mkdir()
        shutil.copy2(ROOT / "tools/commonmark_corpus.py", self.tool)
        self.unrelated = temporary / "unrelated"
        self.unrelated.mkdir()
        self.empty_path = temporary / "empty-path"
        self.empty_path.mkdir()
        self.environment = dict(os.environ, PATH=str(self.empty_path))
        self.fixture = self.corpus / "examples.json"

    def run_tool(self, *arguments):
        return subprocess.run(
            [sys.executable, "-B", str(self.tool), *arguments],
            cwd=self.unrelated, env=self.environment, capture_output=True,
        )

    def snapshot(self):
        return {
            str(path.relative_to(self.checkout)): (path.read_bytes(), path.stat().st_mtime_ns)
            for path in self.checkout.rglob("*") if path.is_file()
        }

    def assert_read_only_failure(self, expected_message):
        before = self.snapshot()
        result = self.run_tool("--check")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(expected_message, result.stderr.decode())
        self.assertEqual(self.snapshot(), before)

    def test_complete_faithful_upstream_dump(self):
        # Compare all stored bytes, and therefore every field, to the real extractor.
        result = subprocess.run(
            [sys.executable, "-B", str(self.corpus / "upstream/test/spec_tests.py"),
             "--spec", str(self.corpus / "upstream/spec.txt"), "--dump-tests"],
            cwd=self.unrelated, env=self.environment, capture_output=True, check=True,
        )
        self.assertEqual(self.fixture.read_bytes(), result.stdout)
        cases = json.loads(result.stdout)
        self.assertEqual(len(cases), 652)
        self.assertEqual([case["example"] for case in cases], list(range(1, 653)))
        self.assertEqual(len({case["section"] for case in cases}), 26)
        for case in cases:
            self.assertEqual(set(case), {
                "markdown", "html", "example", "section", "start_line", "end_line",
            })
            self.assertTrue(case["markdown"].endswith("\n"))
            self.assertTrue(case["html"] == "" or case["html"].endswith("\n"))
        self.assertEqual(cases[0], {
            "markdown": "\tfoo\tbaz\t\tbim\n",
            "html": "<pre><code>foo\tbaz\t\tbim\n</code></pre>\n",
            "example": 1, "section": "Tabs", "start_line": 355, "end_line": 360,
        })
        self.assertEqual(cases[206]["markdown"], "[foo]: /url\n")
        self.assertEqual(cases[206]["html"], "")

    def test_pristine_check_does_not_write(self):
        before = self.snapshot()
        result = self.run_tool("--check")
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertEqual(self.snapshot(), before)

    def test_regeneration_is_byte_stable_without_existing_fixture(self):
        original = self.fixture.read_bytes()
        self.fixture.unlink()
        for _ in range(2):
            result = self.run_tool()
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(self.fixture.read_bytes(), original)

    def test_changed_expected_output_fails_without_repair(self):
        content = self.fixture.read_bytes()
        self.assertIn(b"<pre><code>foo", content)
        self.fixture.write_bytes(content.replace(b"<pre><code>foo", b"<pre><code>Foo", 1))
        self.assert_read_only_failure("examples.json differs")

    def test_removed_case_fails_without_repair(self):
        cases = json.loads(self.fixture.read_bytes())
        del cases[206]
        self.fixture.write_text(json.dumps(cases), encoding="utf-8")
        self.assert_read_only_failure("examples.json differs")

    def test_every_retained_input_is_verified_before_extraction(self):
        provenance = json.loads((self.corpus / "provenance.json").read_bytes())
        for name in provenance["inputs"]:
            with self.subTest(input=name):
                path = self.corpus / name
                original = path.read_bytes()
                # Same-length corruption exercises the digest rather than size check.
                path.write_bytes(bytes([original[0] ^ 1]) + original[1:])
                self.assert_read_only_failure("SHA-256 differs from provenance")
                path.write_bytes(original)

    def test_missing_unconditional_import_is_reported_without_writes(self):
        (self.corpus / "upstream/test/normalize.py").unlink()
        self.assert_read_only_failure("normalize.py")

    def test_license_inventory_matches_retained_notices(self):
        # The retained LICENSE lists two BSD redistribution conditions, plus
        # separate CC-BY-SA specification and MIT normalization notices.
        provenance = json.loads((self.corpus / "provenance.json").read_bytes())
        self.assertEqual(
            {name: entry["license"] for name, entry in provenance["inputs"].items()},
            {
                "upstream/spec.txt": "CC-BY-SA-4.0",
                "upstream/test/spec_tests.py": "BSD-2-Clause",
                "upstream/test/cmark.py": "BSD-2-Clause",
                "upstream/test/normalize.py": "BSD-2-Clause AND MIT",
                "upstream/LICENSE": "CC-BY-SA-4.0 AND BSD-2-Clause AND MIT",
            },
        )

    def test_provenance_and_generated_digest(self):
        provenance = json.loads((self.corpus / "provenance.json").read_bytes())
        self.assertEqual(len(provenance["inputs"]), 5)
        for name, entry in provenance["inputs"].items():
            self.assertEqual(entry["commit"], "9103e341a973013013bb1a80e13567007c5cef6f")
            self.assertIn(entry["commit"], entry["origin_url"])
            for field in ("repository", "retrieved_at", "license", "role", "git_mode"):
                self.assertTrue(entry[field], (name, field))
            content = (self.corpus / name).read_bytes()
            self.assertEqual(len(content), entry["byte_count"])
            self.assertEqual(hashlib.sha256(content).hexdigest(), entry["sha256"])
        self.assertEqual(
            provenance["inputs"]["upstream/spec.txt"]["sha256"],
            "257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80",
        )
        self.assertEqual(hashlib.sha256(self.fixture.read_bytes()).hexdigest(),
                         provenance["generated"]["sha256"])


if __name__ == "__main__":
    unittest.main()

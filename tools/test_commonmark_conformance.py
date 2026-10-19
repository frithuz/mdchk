"""Independent runner/ratchet and real executable-boundary regression checks."""

import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import commonmark_conformance as runner


def small_cases():
    return [{"example": n, "section": section, "markdown": "x\n", "html": "<p>x</p>\n",
             "start_line": n * 10, "end_line": n * 10 + 3}
            for n, section in [(1, "Paragraphs"), (2, "Headings")]]


def small_ledger():
    return {"schema_version": 1, "supported_html_examples": [1],
            "pending": [{"reason": "syntax-unimplemented", "owners": [6, 18], "examples": [2]}]}


class Decisions(unittest.TestCase):
    def test_inventory_cannot_drop_duplicate_or_reclassify_ids(self):
        for change in ["missing", "duplicate", "unknown", "boolean"]:
            with self.subTest(change=change):
                ledger = small_ledger()
                ledger["pending"][0]["examples"] = {
                    "missing": [], "duplicate": [1, 2], "unknown": [3], "boolean": [True, 2]
                }[change]
                with self.assertRaises(ValueError):
                    runner.inventory(small_cases(), ledger)
        with self.assertRaises(ValueError):
            runner.inventory([small_cases()[0]] * 2, small_ledger())

    def test_ratchet_detects_real_lost_pass_and_new_unrecorded_pass(self):
        cases, ledger = small_cases(), small_ledger()
        for result in [
            {"status": "rendered", "html": "wrong"},
            {"status": "pending", "reason": "syntax-unimplemented"},
            {"status": "error", "reason": "invalid-utf8"},
            {"status": "interrupted", "reason": "cancelled"},
        ]:
            with self.subTest(result=result), patch.object(runner, "invoke", return_value=result):
                report = runner.run_cases(cases, cases[:1], ledger, None, None)
                self.assertFalse(report["gate_passed"])
                self.assertEqual(len(report["regressions"]), 1)
                self.assertNotEqual(report["outcomes"][0]["status"], "pass")
        with patch.object(runner, "invoke", return_value={"status": "rendered", "html": "<p>x</p>\n"}):
            report = runner.run_cases(cases, cases, ledger, None, None)
        self.assertFalse(report["gate_passed"])
        self.assertIn("record new pass", report["regressions"][0])
        self.assertEqual(report["counts"]["pass"], 2)

    def test_filtered_run_preserves_full_ledger_and_scope(self):
        cases, ledger = small_cases(), small_ledger()
        saved = copy.deepcopy(ledger)
        with patch.object(runner, "invoke", return_value={"status": "rendered", "html": "<p>x</p>\n"}):
            report = runner.run_cases(cases, cases[:1], ledger, None, None)
        self.assertTrue(report["gate_passed"])
        self.assertEqual((report["selected_count"], report["filtered_out_count"]), (1, 1))
        self.assertEqual(ledger, saved)
        ledger["pending"][0]["examples"] = []
        with self.assertRaises(ValueError):
            runner.run_cases(cases, cases[:1], ledger, None, None)

    def test_filters_and_empty_selection(self):
        cases = small_cases()
        self.assertEqual(runner.select(cases, [2], "heading"), cases[1:])
        for ids, pattern in [([999], None), (None, "["), (None, "absent"), ([1], "heading")]:
            with self.assertRaises(ValueError):
                runner.select(cases, ids, pattern)

    def test_comparison_exception_retains_subsequent_case(self):
        def broken_normalizer(_html):
            raise RuntimeError("independent normalizer failure")

        with patch.object(runner, "invoke", side_effect=[
                {"status": "rendered", "html": "<p>x</p>\n"},
                {"status": "pending", "reason": "syntax-unimplemented"}]):
            report = runner.run_cases(small_cases(), small_cases(), small_ledger(), None, broken_normalizer)
        self.assertFalse(report["gate_passed"])
        self.assertEqual(report["counts"], {"pass": 0, "fail": 0, "error": 1, "pending": 1})
        self.assertEqual([x["example"] for x in report["outcomes"]], [1, 2])
        self.assertEqual(report["outcomes"][0]["reason"], "comparison")
        self.assertIn("RuntimeError: independent normalizer failure", report["outcomes"][0]["message"])

    def test_process_interruption_is_not_swallowed(self):
        def interrupted_normalizer(_html):
            raise KeyboardInterrupt()

        with patch.object(runner, "invoke", return_value={"status": "rendered", "html": "x"}):
            with self.assertRaises(KeyboardInterrupt):
                runner.run_cases(small_cases(), small_cases(), small_ledger(), None, interrupted_normalizer)


class ExecutableBoundary(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="mdchk-adapter-test-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.adapter = Path(cls.temp.name) / "specadapter"
        cls.cases = runner.load_cases()  # Also verifies pinned helper identity.
        cls.normalizer = staticmethod(runner.load_normalizer())
        subprocess.run(["go", "build", "-o", str(cls.adapter),
                        "./internal/conformance/cmd/specadapter"],
                       cwd=runner.corpus.ROOT, check=True, capture_output=True)

    def test_real_adapter_empty_pending_and_invalid_encoding(self):
        for content, code, status, html in [
                (b"", 0, "rendered", b""), (b" \t\r\n", 0, "rendered", b""),
                (b"***\n", 0, "rendered", b"<hr />\n"),
                (b"hello\n", 2, "pending", b""),
                (b"Foo\n***\nbar\n", 2, "pending", b""),
                (b"[foo]: /url\n", 2, "pending", b""),
                (b"\xff", 1, "error", b"")]:
            with self.subTest(content=content):
                plain = subprocess.run([str(self.adapter)], input=content, capture_output=True)
                self.assertEqual(plain.returncode, code)
                self.assertEqual(plain.stdout, html)
                if code:
                    self.assertEqual(json.loads(plain.stderr)["status"], status)
                else:
                    self.assertEqual(plain.stderr, b"")
                machine = subprocess.run([str(self.adapter), "--json"], input=content, capture_output=True, check=True)
                outcome = json.loads(machine.stdout)
                self.assertEqual(outcome["status"], status)
                self.assertEqual(outcome["html"], html.decode())
                self.assertEqual(machine.stderr, b"")

    def test_all_652_actual_outputs_follow_ledger_in_both_modes(self):
        ledger = json.loads((runner.corpus.CORPUS / "foundation.json").read_text())
        for normalize in [self.normalizer, None]:
            with self.subTest(normalized=normalize is not None):
                report = runner.run_cases(self.cases, self.cases, ledger, self.adapter, normalize)
                self.assertTrue(report["gate_passed"])
                self.assertFalse(report["full_conformance"])
                self.assertEqual(report["counts"], {"pass": 84, "fail": 0, "error": 0, "pending": 568})
                self.assertEqual([r["example"] for r in report["outcomes"]], list(range(1, 653)))
                supported = {1, 2, 3, 6, 236, 239, 240, 8, 11, 18, 19, 36, 43, 47, 48, 50, 51, 52, 53, 54,
                             69, 79, 85, 98, 100, 107, 110, 111, 112, 116, 117,
                             118, 119, 120, 122, 123, 124, 125, 126, 127, 129,
                             130, 131, 132, 133, 134, 135, 136, 137, 139, 142,
                             143, 144, 146, 147, 231, 289,
                             21, 31, 150, 151, 153, 154, 156, 157, 158, 159, 160,
                             161, 162, 163, 164, 165, 166, 171, 173, 178, 181,
                             183, 184, 186, 189, 190, 191}
                self.assertEqual(set(ledger["supported_html_examples"]), supported)
                owners = {i: group["owners"] for group in ledger["pending"] for i in group["examples"]}
                for outcome in report["outcomes"]:
                    if outcome["example"] in supported:
                        self.assertEqual(outcome["status"], "pass")
                        self.assertNotIn(outcome["example"], owners)
                    else:
                        self.assertEqual(outcome["status"], "pending")
                        self.assertEqual(outcome["reason"], "syntax-unimplemented")
                        self.assertEqual(outcome["owners"], owners[outcome["example"]])
                html_owners = {
                    (17, 18): [149, 169, 170, 172, 174, 179, 180, 182, 185],
                    (17, 18, 24, 25): [152, 155, 167, 176, 177, 188],
                    (17, 18, 21, 23, 24, 25): [148],
                    (17, 18, 23, 24, 25): [168],
                    (14, 15, 16, 17, 18): [175],
                    (17, 18, 21, 23): [187],
                }
                for required, examples in html_owners.items():
                    for example in examples:
                        self.assertEqual(owners[example], list(required))
                self.assertEqual(owners[49], [17, 18, 21])
                self.assertEqual(owners[58], [17, 18])
                self.assertEqual(owners[59], [17, 18])
                setext_owners = {
                    (17, 18): [83, 84, 86, 89, 90, 92, 96, 97, 101, 103],
                    (17, 18, 24, 25): [80],
                    (17, 18, 21, 24, 25): [81, 82],
                    (17, 18, 21): [87, 88, 93, 95, 104, 105],
                    (17, 18, 20, 23): [91],
                    (14, 15, 16, 17, 18): [94, 99],
                    (17, 18, 19): [102],
                    (17, 18, 19, 21): [106],
                }
                for required, examples in setext_owners.items():
                    for example in examples:
                        self.assertEqual(owners[example], list(required))
                for example in [57, 60, 61]:
                    self.assertEqual(owners[example], [14, 15, 16, 17, 18])
                self.assertEqual(owners[219], [17, 18])
                self.assertEqual(owners[220], [17, 18, 21])
                self.assertEqual(owners[225], [17, 18])
                self.assertEqual(owners[226], [17, 18, 21])
                self.assertEqual(owners[227], [17, 18])
                for example in [62, 63, 64, 67, 68, 71, 72, 73, 74, 75, 77, 78]:
                    self.assertEqual(owners[example], [17, 18])
                for example in [65, 76]:
                    self.assertEqual(owners[example], [17, 18, 19])
                self.assertEqual(owners[66], [17, 18, 19, 24, 25])
                self.assertEqual(owners[70], [17, 18, 21])
                for example in [108, 109]:
                    self.assertEqual(owners[example], [14, 15, 16, 17, 18])
                self.assertEqual(owners[113], [17, 18, 21])
                for example in [114, 115]:
                    self.assertEqual(owners[example], [17, 18])
                self.assertEqual(owners[121], [17, 18, 20])
                self.assertEqual(owners[128], [17, 18])
                for example in [138, 145]:
                    self.assertEqual(owners[example], [17, 18, 20, 21])
                for example in [140, 141]:
                    self.assertEqual(owners[example], [17, 18])
                quote_owners = {
                    (17, 18, 21): [228, 229, 230, 232, 233, 238, 243, 247, 250, 251],
                    (17, 18): [234, 237, 241, 242, 244, 245, 246, 248, 249, 252],
                    (14, 15, 16, 17, 18): [235],
                    (17, 18, 27): [214, 218],
                }
                for required, examples in quote_owners.items():
                    for example in examples:
                        self.assertEqual(owners[example], list(required))
                self.assertEqual(report["outcomes"][206]["status"], "pending")
        complete = runner.run_cases(self.cases, self.cases[206:207], ledger, self.adapter, None, True)
        self.assertFalse(complete["gate_passed"])

    def test_normalization_discriminator_and_diff_context(self):
        expected, actual = "<p>a  b</p>\n", "<p>a b</p>\n"
        self.assertEqual(runner.compare(expected, actual, self.normalizer), "")
        diff = runner.compare(expected, actual, None)
        self.assertIn("expected HTML", diff)
        self.assertIn("actual bytes:", diff)
        cases = small_cases()
        with patch.object(runner, "invoke", return_value={"status": "rendered", "html": "wrong"}):
            report = runner.run_cases(cases, cases[:1], small_ledger(), self.adapter, None)
        self.assertEqual((report["outcomes"][0]["example"], report["outcomes"][0]["start_line"]), (1, 10))
        self.assertIn("diff", report["outcomes"][0])

    def test_pinned_upstream_helper_reports_unsupported_as_error(self):
        case = self.cases[206]
        self.assertEqual((case["example"], case["html"]), (207, ""))
        for mode in [[], ["--no-normalize"]]:
            result = subprocess.run([sys.executable, "-E", "-s", "-B",
                                     str(runner.corpus.CORPUS / "upstream/test/spec_tests.py"),
                                     "--spec", str(runner.corpus.CORPUS / "upstream/spec.txt"),
                                     "--program", str(self.adapter), "--number", "207", *mode], capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn(b"0 passed, 0 failed, 1 errored, 651 skipped", result.stdout)
            self.assertIn(b'"status":"pending"', result.stdout)

    def test_pinned_upstream_helper_complete_empty_transport_and_execution_failure(self):
        # A separate synthetic spec drives the real retained helper's pass path.
        # It is not a new retained upstream example or a conformance pass.
        spec = Path(self.temp.name) / "empty-spec.txt"
        fence = "`" * 32
        spec.write_text(f"# Empty transport fixture\n{fence} example\n.\n{fence}\n")
        bad = Path(self.temp.name) / "failing-adapter"
        bad.write_text(f"#!{sys.executable}\nraise SystemExit(7)\n")
        bad.chmod(0o700)
        for program, count, code in [(self.adapter, b"1 passed, 0 failed, 0 errored", 0),
                                     (bad, b"0 passed, 0 failed, 1 errored", 1)]:
            for mode in [[], ["--no-normalize"]]:
                result = subprocess.run([sys.executable, "-E", "-s", "-B",
                                         str(runner.corpus.CORPUS / "upstream/test/spec_tests.py"),
                                         "--spec", str(spec), "--program", str(program), *mode], capture_output=True)
                self.assertEqual(result.returncode, code)
                self.assertIn(count, result.stdout)

    def test_execution_and_protocol_failures_are_not_pending(self):
        bad = Path(self.temp.name) / "bad-adapter"
        for program in ["raise SystemExit(7)", "print('not JSON')", "print('{\"schema_version\":1,\"status\":\"pending\"}')"]:
            bad.write_text(f"#!{sys.executable}\n{program}\n")
            bad.chmod(0o700)
            outcome = runner.invoke(bad, "x")
            self.assertEqual(outcome["status"], "error")
            self.assertEqual(outcome["reason"], "adapter-execution")
        self.assertEqual(runner.invoke(Path(self.temp.name) / "missing", "x")["status"], "error")

    def test_cli_per_case_exceptions_preserve_complete_selection_accounting(self):
        adapter = Path(self.temp.name) / "faulty-protocol-adapter"
        # Error in the middle demonstrates retention of earlier results and
        # continued evaluation of subsequent selected original IDs.
        for outcome, reason in [
            ({"schema_version": 1, "status": []}, "adapter-execution"),
            ({"schema_version": 1, "status": {}}, "adapter-execution"),
            ({"schema_version": 1, "status": "rendered", "html": '<span data-x="href">ok</span>'}, "comparison"),
        ]:
            with self.subTest(outcome=outcome):
                adapter.write_text(f"#!{sys.executable}\nimport json,sys\n"
                                   f"outcome = {outcome!r} if sys.stdin.read() == {self.cases[206]['markdown']!r} else "
                                   "{'schema_version':1,'status':'pending','reason':'syntax-unimplemented'}\n"
                                   "print(json.dumps(outcome))\n")
                adapter.chmod(0o700)
                result = subprocess.run([sys.executable, "-B", str(runner.corpus.ROOT / "tools/commonmark_conformance.py"),
                                         "--adapter", str(adapter), "--example", "1", "--example", "207", "--example", "208", "--json"], capture_output=True)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertNotIn(b"Traceback", result.stderr)
                report = json.loads(result.stdout)
                self.assertFalse(report["gate_passed"])
                self.assertFalse(report["full_conformance"])
                self.assertEqual((report["selected_count"], report["filtered_out_count"]), (3, 649))
                self.assertEqual(report["counts"], {"pass": 0, "fail": 0, "error": 1, "pending": 2})
                self.assertEqual(sum(report["counts"].values()), report["selected_count"])
                self.assertEqual([x["example"] for x in report["outcomes"]], [1, 207, 208])
                self.assertEqual([x["status"] for x in report["outcomes"]], ["pending", "error", "pending"])
                self.assertEqual(report["outcomes"][1]["reason"], reason)
                self.assertTrue(report["outcomes"][1]["message"])

    def test_cli_scope_exit_status_and_no_ledger_write(self):
        before = (runner.corpus.CORPUS / "foundation.json").read_bytes()
        command = [sys.executable, "-B", str(runner.corpus.ROOT / "tools/commonmark_conformance.py"),
                   "--adapter", str(self.adapter), "--example", "207", "--json"]
        result = subprocess.run(command, cwd=self.temp.name, capture_output=True, check=True)
        report = json.loads(result.stdout)
        self.assertEqual((report["selected_count"], report["filtered_out_count"]), (1, 651))
        self.assertEqual(report["counts"]["pending"], 1)
        final = subprocess.run([*command, "--require-complete"], capture_output=True)
        self.assertEqual(final.returncode, 1)
        for extra in [["--section", "["], ["--section", "absent"]]:
            result = subprocess.run([*command, *extra], capture_output=True)
            self.assertEqual(result.returncode, 2)
            self.assertEqual(result.stdout, b"")
        self.assertEqual((runner.corpus.CORPUS / "foundation.json").read_bytes(), before)


if __name__ == "__main__":
    unittest.main()

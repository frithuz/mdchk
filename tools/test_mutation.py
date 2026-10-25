#!/usr/bin/env python3
"""Exercise mutation infrastructure failure paths without a real campaign."""

import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("mutation", Path(__file__).with_name("mutation.py"))
MUTATION = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MUTATION)


class MutationHarness(unittest.TestCase):
    def test_environment_ignores_ambient_mutation_selection(self):
        with patch.dict(os.environ, {"GREMLINS_UNLEASH_DRY_RUN": "true",
                                   "GREMLINS_MUTANTS_CONDITIONALS_NEGATION_ENABLED": "false",
                                   "GOFLAGS": "-tags=wrong", "MDCHK_RESULT_VALIDATOR_PYTHON": "wrong"}):
            env = MUTATION.environment()
        self.assertFalse(any(k.startswith("GREMLINS_") for k in env))
        self.assertNotIn("MDCHK_RESULT_VALIDATOR_PYTHON", env)
        self.assertEqual(env["GOFLAGS"], "-p=2 -mod=readonly")

    def test_snapshot_uses_candidate_bytes_and_ignores_ignored_artifacts(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "repo"
            root.mkdir()
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            (root / ".gitignore").write_text("ignored/\n")
            (root / "tracked").write_text("committed")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            (root / "tracked").write_text("candidate")
            (root / "new").write_text("new candidate")
            (root / "ignored").mkdir()
            (root / "ignored/large").write_text("not copied")
            target = Path(folder) / "snapshot"
            before = MUTATION.snapshot(root, target)
            self.assertEqual((target / "tracked").read_text(), "candidate")
            self.assertEqual((target / "new").read_text(), "new candidate")
            self.assertFalse((target / ".git").exists())
            self.assertFalse((target / "ignored").exists())
            self.assertEqual(MUTATION.inventory(root), before)
            (root / "link").symlink_to("tracked")
            with self.assertRaisesRegex(RuntimeError, "symlink"):
                MUTATION.inventory(root)

    def test_nonzero_tool_or_baseline_failure_propagates(self):
        with tempfile.TemporaryDirectory() as folder:
            log = Path(folder) / "failure.log"
            with self.assertRaisesRegex(RuntimeError, "command failed \\(7\\)"):
                MUTATION.run([sys.executable, "-c", "print('baseline failed'); raise SystemExit(7)"],
                             folder, os.environ, log, 10)
            self.assertIn("baseline failed", log.read_text())

    def test_deadline_kills_owned_descendant_and_retains_log(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            child = root / "child.py"
            child.write_text("import time\nfrom pathlib import Path\ntime.sleep(0.8)\nPath('escaped').write_text('bad')\n")
            parent = root / "parent.py"
            parent.write_text("import subprocess,sys,time\nfrom pathlib import Path\n"
                              "p=subprocess.Popen([sys.executable,'child.py'])\n"
                              "Path('child.pid').write_text(str(p.pid))\n"
                              "print('started',flush=True)\ntime.sleep(60)\n")
            with self.assertRaisesRegex(RuntimeError, "deadline exceeded"):
                MUTATION.run([sys.executable, str(parent)], root, os.environ, root / "log", 0.3)
            self.assertIn("started", (root / "log").read_text())
            time.sleep(0.9)
            self.assertFalse((root / "escaped").exists(), "owned child survived deadline")

    def test_sigterm_entrypoint_kills_install_descendants(self):
        # Run the actual entrypoint and signal its real main handler while a
        # substitute Go installer has an owned descendant. No Gremlins override.
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            executable = root / "go"
            child_code = ("import os,time; from pathlib import Path; time.sleep(0.8); "
                          "(Path(os.environ['MUTATION_TEST_PROCESS_DIR'])/'escaped').write_text('bad')")
            executable.write_text(f"#!{sys.executable}\n"
                "import os,subprocess,sys,time\nfrom pathlib import Path\n"
                "base=Path(os.environ['MUTATION_TEST_PROCESS_DIR'])\n"
                f"child=subprocess.Popen([sys.executable,'-c',{child_code!r}])\n"
                "(base/'ready').write_text(str(child.pid))\nprint('ready',flush=True)\ntime.sleep(60)\n")
            executable.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                       MUTATION_TEST_PROCESS_DIR=str(root))
            output = root / "reports"
            with (root / "outer.log").open("wb") as log:
                proc = subprocess.Popen([sys.executable, "-B", str(Path(MUTATION.__file__)),
                                         "--output", str(output)], env=env, stdout=log,
                                        stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + 10
                    while not (root / "ready").exists() and proc.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.02)
                    self.assertTrue((root / "ready").exists(), (root / "outer.log").read_text())
                    proc.send_signal(signal.SIGTERM)
                    self.assertNotEqual(proc.wait(timeout=10), 0)
                finally:
                    if proc.poll() is None:
                        proc.kill()
                    proc.wait()
            time.sleep(0.9)
            self.assertFalse((root / "escaped").exists(), "install child survived SIGTERM")
            result = json.loads((output / "run.json").read_text())
            self.assertFalse(result["complete"])
            self.assertIn("interrupted by signal", result["error"])
            self.assertIn("ready", (output / "install.log").read_text())

    def test_fresh_output_directory_refuses_stale_evidence(self):
        with tempfile.TemporaryDirectory() as folder:
            output = Path(folder)
            stale = output / "rules.json"
            stale.write_text("preserved previous evidence")
            proc = subprocess.run([sys.executable, "-B", str(Path(MUTATION.__file__)),
                                   "--output", str(output)], capture_output=True, text=True, check=False)
            self.assertNotEqual(proc.returncode, 0)
            self.assertEqual(stale.read_text(), "preserved previous evidence")
            self.assertFalse((output / "run.json").exists())

    def test_report_refuses_incomplete_malformed_empty_and_wrong_scope(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "internal/rules").mkdir(parents=True)
            (root / "internal/rules/md001.go").write_text("a > b\n")
            path = root / "report.json"
            with self.assertRaises(FileNotFoundError):
                MUTATION.validate_report(path, "rules", root)
            path.write_text("not json")
            with self.assertRaises(ValueError):
                MUTATION.validate_report(path, "rules", root)
            valid = {"go_module": "github.com/frithuz/mdchk", "files": [{"file_name": "md001.go", "mutations": [
                {"type": "CONDITIONALS_BOUNDARY", "status": "KILLED", "line": 1, "column": 3}]}]}
            def rejected(data):
                path.write_text(json.dumps(data))
                with self.assertRaises((ValueError, KeyError)):
                    MUTATION.validate_report(path, "rules", root)
            rejected(dict(valid, files=[]))
            for status in ("RUNNABLE", "unknown", "NOT COVERED"):
                data = json.loads(json.dumps(valid))
                data["files"][0]["mutations"][0]["status"] = status
                rejected(data)
            data = json.loads(json.dumps(valid))
            data["files"][0]["file_name"] = "parser.go"
            rejected(data)
            data = json.loads(json.dumps(valid))
            data["files"][0]["mutations"] *= 2
            rejected(data)
            path.write_text(json.dumps(valid))
            _, rows = MUTATION.validate_report(path, "rules", root)
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["expression"], "a > b")
            self.assertEqual(rows[0]["status"], "KILLED")
            valid["files"][0]["mutations"][0]["status"] = "LIVED"
            path.write_text(json.dumps(valid))
            _, lived = MUTATION.validate_report(path, "rules", root)
            self.assertEqual(lived[0]["identity"], rows[0]["identity"],
                             "review identity must remain stable when test outcomes change")


if __name__ == "__main__":
    unittest.main(verbosity=2)

#!/usr/bin/env python3
"""Run the pinned critical-behavior mutation baseline in an owned snapshot."""

import argparse
from collections import Counter
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import tempfile
import time

PIN = "github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"
AREAS = {"rules": ("md001.go",), "config": ("validate.go", "policy.go"),
         "fix": ("plan.go", "validate.go")}
STATUSES = {"KILLED", "LIVED", "NOT COVERED", "NOT VIABLE", "TIMED OUT", "SKIPPED"}
# Each replay is a token mutation that the campaign must independently report killed.
REPLAYS = [
    ("rules", "md001.go", "heading.Level > previous+1", ">", ">=",
     "CONDITIONALS_BOUNDARY", "TestMD001AnalysisFixtures", ("unexpected outcome:",)),
    ("config", "policy.go", "Allowed: len(restrictions) == 0", "==", "!=",
     "CONDITIONALS_NEGATION", "TestDirectivePermissionMatrix", ("wrong effective decision:",)),
    ("fix", "plan.go", "text[at-1] == '\\r'", "==", "!=",
     "CONDITIONALS_NEGATION", "TestBoundariesOwnershipPreconditionsAndRules",
     ("--- FAIL: TestBoundariesOwnershipPreconditionsAndRules/crlf", "Accepted:true")),
    ("fix", "validate.go", "proof != Preserved", "!=", "==",
     "CONDITIONALS_NEGATION", "TestCandidateTrustedMeaningAndMissingProof",
     ("--- FAIL: TestCandidateTrustedMeaningAndMissingProof/uncertain_proof",
      r'candidate="\nhello" complete=true reasons=[]')),
]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def environment():
    env = {k: v for k, v in os.environ.items()
           if not k.startswith("GREMLINS_") and k not in
           {"MDCHK_RESULT_VALIDATOR_PYTHON", "GOFLAGS", "GOWORK", "GOTOOLCHAIN", "GOMAXPROCS"}}
    env.update(GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-p=2 -mod=readonly", GOMAXPROCS="2")
    return env


def run(command, cwd, env, log, timeout):
    """Kill the whole owned process group on every completion path, including signals."""
    started = time.monotonic()
    proc = None
    try:
        with Path(log).open("wb") as output:
            proc = subprocess.Popen(command, cwd=cwd, env=env, stdout=output,
                                    stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = proc.wait(timeout=timeout)
            except subprocess.TimeoutExpired as err:
                raise RuntimeError(f"deadline exceeded ({timeout}s): {command[0]}") from err
        if code:
            raise RuntimeError(f"command failed ({code}); see {log}")
    finally:
        if proc is not None:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait()
    return round(time.monotonic() - started, 3)


def inventory(root):
    names = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others",
                                     "--exclude-standard"], cwd=root).split(b"\0")
    result = {}
    for raw in sorted(set(names) - {b""}):
        name = os.fsdecode(raw)
        path = root / name
        if path.is_symlink():
            raise RuntimeError(f"snapshot refuses symlink: {name}")
        if path.is_file():  # Tracked deletions are candidate deletions too.
            result[name] = {"sha256": digest(path.read_bytes()), "mode": path.stat().st_mode & 0o777}
    return result


def snapshot(root, target):
    before = inventory(root)
    target.mkdir()
    for name, identity in before.items():
        destination = target / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / name, destination)
        destination.chmod(identity["mode"])
        if digest(destination.read_bytes()) != identity["sha256"]:
            raise RuntimeError(f"source changed during snapshot: {name}")
    if inventory(root) != before:
        raise RuntimeError("source changed during snapshot")
    return before


def validate_report(path, area, source):
    report = json.loads(path.read_text())
    if report.get("go_module") != "github.com/frithuz/mdchk":
        raise ValueError("wrong report module")
    rows, seen = [], set()
    for file in report["files"]:
        name = file["file_name"]
        if name not in AREAS[area] or name in seen:
            raise ValueError(f"unexpected/duplicate report file: {name}")
        seen.add(name)
        data = (source / "internal" / area / name).read_bytes()
        lines = data.splitlines()
        for mutant in file["mutations"]:
            status = mutant["status"]
            if status not in STATUSES:
                raise ValueError(f"unfinished/unknown mutant: {status}")
            line, column = mutant["line"], mutant["column"]
            if not (isinstance(line, int) and 1 <= line <= len(lines)
                    and isinstance(column, int) and 1 <= column <= len(lines[line - 1])):
                raise ValueError("invalid source position")
            if mutant["type"] not in {"CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"}:
                raise ValueError("unexpected mutation operator")
            row = dict(mutant, file=f"internal/{area}/{name}", source_sha256=digest(data),
                       expression=lines[line - 1].decode("utf-8"))
            token = re.match(r"==|!=|<=|>=|<|>", lines[line - 1][column - 1:].decode("utf-8"))
            if token is None:
                raise ValueError("report position does not identify a comparison token")
            old = token.group()
            replacements = ({">": ">=", ">=": ">", "<": "<=", "<=": "<"}
                            if mutant["type"] == "CONDITIONALS_BOUNDARY" else
                            {"==": "!=", "!=": "==", ">": "<=", ">=": "<", "<": ">=", "<=": ">"})
            row["before"], row["after"] = old, replacements[old]
            stable = {k: v for k, v in row.items() if k != "status"}
            row["identity"] = digest(json.dumps(stable, sort_keys=True).encode())
            rows.append(row)
    if seen != set(AREAS[area]) or not rows or not any(r["status"] in {"KILLED", "LIVED"} for r in rows):
        raise ValueError(f"empty/unexecuted/missing target area: {area}")
    keys = [(r["file"], r["line"], r["column"], r["type"]) for r in rows]
    if len(keys) != len(set(keys)):
        raise ValueError("duplicate mutants")
    return report, rows


def replay(source, env, output, rows):
    evidence = []
    for area, name, expression, old, new, operator, test, markers in REPLAYS:
        path = source / "internal" / area / name
        original = path.read_text()
        if original.count(expression) != 1:
            raise RuntimeError(f"representative expression moved: {expression}")
        offset = original.index(expression) + expression.index(old)
        line = original[:offset].count("\n") + 1
        column = len(original[:offset].rsplit("\n", 1)[-1].encode()) + 1
        match = [r for r in rows if r["file"] == f"internal/{area}/{name}" and
                 (r["line"], r["column"], r["type"], r["status"]) == (line, column, operator, "KILLED")]
        if len(match) != 1:
            raise RuntimeError(f"representative absent or not killed: {area}:{line}:{column}")
        label = f"replay-{area}-{name[:-3]}"
        command = ["go", "test", "-p", "2", f"./internal/{area}", "-run", f"^{test}$", "-count=1", "-v"]
        run(command, source, env, output / f"{label}-baseline.log", 120)
        try:
            path.write_text(original[:offset] + new + original[offset + len(old):])
            try:
                run(command, source, env, output / f"{label}-mutant.log", 120)
            except RuntimeError as err:
                text = (output / f"{label}-mutant.log").read_text()
                if "command failed (1)" not in str(err) or f"--- FAIL: {test}" not in text or not all(marker in text for marker in markers) or any(
                        forbidden in text for forbidden in ("[build failed]", "panic:", "no tests to run")):
                    raise RuntimeError(f"representative did not fail intended assertion: {label}") from err
            else:
                raise RuntimeError(f"representative survived: {label}")
        finally:
            path.write_text(original)
        evidence.append(dict(identity=match[0]["identity"], command=command, before=old, after=new,
                             baseline="PASS", mutant="semantic assertion FAIL", assertion_markers=markers, log_prefix=label))
    return evidence


def campaign(root, output):
    env = environment()
    result = {"tool": PIN, "source_commit": subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(), "host": platform.platform(),
        "cpu_count": os.cpu_count(), "limits": {"workers": 1, "GOMAXPROCS": 2, "go_parallelism": 2,
                                                "area_deadline_seconds": 900},
        "cache": "existing Go build/module cache; test replay uses -count=1", "areas": {}}
    started = time.monotonic()
    try:
        with tempfile.TemporaryDirectory(prefix="mdchk-mutation-") as directory:
            scratch = Path(directory)
            source = scratch / "source"
            manifest = snapshot(root, source)
            result["source_manifest"] = manifest
            result["source_digest"] = digest(json.dumps(manifest, sort_keys=True).encode())
            result["config_sha256"] = manifest[".gremlins.yaml"]["sha256"]
            tool_env = dict(env, GOBIN=str(scratch / "bin"), GOFLAGS="-p=2")
            run(["go", "install", PIN], scratch, tool_env, output / "install.log", 300)
            binary = str(scratch / "bin/gremlins")
            run(["go", "version", "-m", binary], scratch, env, output / "tool-build.log", 30)
            build = (output / "tool-build.log").read_text()
            if "mod\tgithub.com/go-gremlins/gremlins\tv0.6.0\t" not in build:
                raise RuntimeError("installed tool module pin was not established")
            run(["go", "version"], scratch, env, output / "go-version.log", 30)
            rows = []
            for area, files in AREAS.items():
                report_path = output / f"{area}.json"
                if report_path.exists():
                    raise RuntimeError("report destination must be fresh")
                command = [binary, "--config", str(source / ".gremlins.yaml"), "unleash",
                           str(source / "internal" / area), "--output", str(report_path)]
                for path in sorted((source / "internal" / area).glob("*.go")):
                    if path.name not in files and not path.name.endswith("_test.go"):
                        command.extend(["--exclude-files", r"(^|/)" + re.escape(path.name) + "$"])
                print(f"Running {area}: {', '.join(files)} (900s deadline)", flush=True)
                elapsed = run(command, source, env, output / f"{area}.log", 900)
                report, area_rows = validate_report(report_path, area, source)
                rows.extend(area_rows)
                console = (output / f"{area}.log").read_text()
                coverage = re.search(r"Gathering coverage\.\.\. done in (\S+)", console)
                result["areas"][area] = dict(command=command, wall_seconds=elapsed,
                    engine_seconds=report["elapsed_time"], coverage_console=coverage.group(1) if coverage else None,
                    counts=dict(Counter(r["status"] for r in area_rows)), total=len(area_rows),
                    report_sha256=digest(report_path.read_bytes()), executed_tests=f"./internal/{area}",
                    mutation_files=list(files))
                print(f"Completed {area}: {result['areas'][area]['counts']}, {elapsed}s", flush=True)
            (output / "mutants.json").write_text(json.dumps(rows, indent=2) + "\n")
            result["replays"] = replay(source, env, output, rows)
            if inventory(root) != manifest:
                raise RuntimeError("live source changed during campaign")
            # Gremlins and representative replays must leave the snapshot unchanged too.
            for name, identity in manifest.items():
                if digest((source / name).read_bytes()) != identity["sha256"]:
                    raise RuntimeError(f"snapshot changed after campaign: {name}")
            result["complete"] = True
    except BaseException as err:
        result["complete"] = False
        result["error"] = str(err)
        raise
    finally:
        result["wall_seconds"] = round(time.monotonic() - started, 3)
        (output / "run.json").write_text(json.dumps(result, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, help="new report directory outside the checkout")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve() if args.output else Path(tempfile.mkdtemp(prefix="mdchk-mutation-reports-"))
    if output == root or root in output.parents:
        parser.error("reports must be outside the checkout")
    if args.output:
        output.mkdir(parents=True, exist_ok=False)
    print(f"Mutation evidence: {output}", flush=True)
    def interrupted(signum, _frame):
        raise RuntimeError(f"interrupted by signal {signum}")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    campaign(root, output)


if __name__ == "__main__":
    main()

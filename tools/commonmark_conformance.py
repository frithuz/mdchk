#!/usr/bin/env python3
"""Run staged CommonMark regression checks (Python 3.9+, Go, no network)."""

import argparse
from difflib import unified_diff
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

import commonmark_corpus as corpus


def load_cases():
    generated = corpus.generate()  # Verifies all retained helper hashes first.
    if (corpus.CORPUS / "examples.json").read_bytes() != generated:
        raise ValueError("examples.json differs from pinned upstream dump")
    return json.loads(generated)


def load_normalizer():
    spec = importlib.util.spec_from_file_location(
        "mdchk_upstream_normalize", corpus.CORPUS / "upstream/test/normalize.py"
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.normalize_html


def inventory(cases, ledger):
    """Validate the full ledger before filtering; IDs cannot silently disappear."""
    ids = [c["example"] for c in cases]
    if len(ids) != len(set(ids)):
        raise ValueError("duplicate corpus ID")
    if ledger.get("schema_version") != 1:
        raise ValueError("unsupported ledger schema")
    supported = ledger["supported_html_examples"]
    pending = {}
    seen = list(supported)
    for group in ledger["pending"]:
        if (not isinstance(group["reason"], str) or not group["reason"]
                or not group["owners"]
                or any(type(x) is not int or x < 1 for x in group["owners"])):
            raise ValueError("pending group needs a reason and issue owners")
        for number in group["examples"]:
            seen.append(number)
            pending[number] = {"reason": group["reason"], "owners": group["owners"]}
    if any(type(x) is not int for x in seen):
        raise ValueError("ledger IDs must be integers")
    if len(seen) != len(set(seen)) or set(seen) != set(ids):
        raise ValueError("ledger must account for every original ID exactly once")
    return set(supported), pending


def select(cases, examples, section):
    if examples and not set(examples) <= {c["example"] for c in cases}:
        raise ValueError("unknown example ID")
    try:
        pattern = re.compile(section, re.IGNORECASE) if section is not None else None
    except re.error as error:
        raise ValueError(f"invalid section regex: {error}") from error
    result = [c for c in cases if (not examples or c["example"] in examples)
              and (pattern is None or pattern.search(c["section"]))]
    if not result:
        raise ValueError("empty selection; no conformance result")
    return result


def invoke(adapter, markdown):
    try:
        process = subprocess.run(
            [str(adapter), "--json"], input=markdown.encode("utf-8"),
            capture_output=True, timeout=30,
        )
        if process.returncode:
            raise ValueError(f"adapter exit {process.returncode}: "
                             + process.stderr.decode("utf-8", errors="replace"))
        result = json.loads(process.stdout.decode("utf-8"))
        if (not isinstance(result, dict) or result.get("schema_version") != 1
                or not isinstance(result.get("status"), str)
                or result.get("status") not in {"rendered", "pending", "error", "interrupted"}):
            raise ValueError("invalid adapter outcome envelope")
        status = result["status"]
        if status == "rendered":
            if not isinstance(result.get("html"), str):
                raise ValueError("invalid adapter HTML")
        elif (not isinstance(result.get("reason"), str) or not result["reason"]
              or result.get("html")):
            raise ValueError("nonrendered outcome needs a reason and no HTML")
        if status == "pending" and result["reason"] != "syntax-unimplemented":
            raise ValueError("pending must identify unsupported syntax, not failure/interruption")
        return result
    except Exception as error:
        # This per-example boundary contains ordinary execution/protocol
        # exceptions, not BaseException (KeyboardInterrupt/SystemExit).
        return {"status": "error", "reason": "adapter-execution",
                "message": f"{type(error).__name__}: {error}"}


def compare(expected, actual, normalize):
    if normalize is not None:
        # Exactly the retained spec_tests.py comparison convention.
        expected, actual = normalize(expected) + "\n", normalize(actual) + "\n"
    if expected == actual:
        return ""
    diff = "".join(unified_diff(expected.splitlines(True), actual.splitlines(True),
                                "expected HTML", "actual HTML"))
    # Missing final newlines can otherwise make a diff misleading or blank.
    return diff + f"\nexpected bytes: {expected.encode('utf-8')!r}\nactual bytes: {actual.encode('utf-8')!r}\n"


def run_cases(cases, selected, ledger, adapter, normalize, require_complete=False):
    supported, pending = inventory(cases, ledger)
    outcomes, regressions = [], []
    for case in selected:
        number = case["example"]
        result = invoke(adapter, case["markdown"])
        record = {"example": number, "section": case["section"],
                  "start_line": case["start_line"], "end_line": case["end_line"]}
        if result["status"] == "rendered":
            try:
                diff = compare(case["html"], result.get("html", ""), normalize)
                record["status"] = "fail" if diff else "pass"
                if diff:
                    record["diff"] = diff
            except Exception as error:
                # Retained upstream code can raise ordinary exceptions beyond
                # ValueError. Preserve this ID and continue suite accounting;
                # never guess equality or modify/fall back from the normalizer.
                record.update(status="error", reason="comparison",
                              message=f"{type(error).__name__}: {error}")
        elif result["status"] == "pending":
            record.update(status="pending", reason=result["reason"],
                          owners=pending.get(number, {}).get("owners", []))
        else:
            # Keep interruption identity; its count is an error, never syntax-pending.
            record.update(status="error", reason=result["reason"],
                          adapter_status=result["status"], message=result.get("message", ""))
        if number in supported and record["status"] != "pass":
            regressions.append(f"example {number}: supported pass became {record['status']}")
        if number not in supported and record["status"] == "pass":
            regressions.append(f"example {number}: record new pass in supported_html_examples")
        outcomes.append(record)
    counts = {key: sum(x["status"] == key for x in outcomes)
              for key in ("pass", "fail", "error", "pending")}
    failed = bool(counts["fail"] or counts["error"] or regressions
                  or require_complete and counts["pending"])
    return {"schema_version": 1, "mode": "normalized" if normalize else "strict",
            "gate": "complete" if require_complete else "staged-regression",
            "full_corpus_count": len(cases), "selected_count": len(selected),
            "filtered_out_count": len(cases) - len(selected),
            "counts": counts, "regressions": regressions, "outcomes": outcomes,
            "full_conformance": len(selected) == len(cases) and counts["pass"] == len(cases) and not failed,
            "gate_passed": not failed}


def report_text(report):
    for case in report["outcomes"]:
        if case["status"] != "pass":
            print(f"Example {case['example']} ({case['section']}, spec lines "
                  f"{case['start_line']}-{case['end_line']}): {case['status']} "
                  f"{case.get('reason', '')} owners={case.get('owners', [])}")
            if "diff" in case:
                print(case["diff"], end="")
            if case.get("message"):
                print(case["message"])
    for error in report["regressions"]:
        print("Ratchet:", error)
    counts = report["counts"]
    print(f"{report['mode']} {report['gate']}: selected {report['selected_count']}/"
          f"{report['full_corpus_count']}, filtered out {report['filtered_out_count']}; "
          + ", ".join(f"{counts[x]} {x}" for x in counts)
          + f"; gate {'passed' if report['gate_passed'] else 'FAILED'}; "
          + f"full conformance={str(report['full_conformance']).lower()}")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--adapter", type=Path, help="prebuilt specadapter (default: build once in a temporary directory)")
    parser.add_argument("--example", type=int, action="append", help="original example ID; repeat to select several")
    parser.add_argument("--section", help="case-insensitive section regex; combined with --example")
    parser.add_argument("--strict", action="store_true", help="compare exact output rather than upstream-normalized HTML")
    parser.add_argument("--require-complete", action="store_true", help="also fail if any selected case is pending")
    parser.add_argument("--json", action="store_true", help="emit explicit per-ID test outcomes and scope as JSON")
    args = parser.parse_args(argv)
    try:
        cases = load_cases()
        ledger = json.loads((corpus.CORPUS / "foundation.json").read_text(encoding="utf-8"))
        inventory(cases, ledger)
        selected = select(cases, args.example, args.section)
        normalize = None if args.strict else load_normalizer()
        with tempfile.TemporaryDirectory(prefix="mdchk-conformance-") as directory:
            adapter = args.adapter.resolve() if args.adapter else Path(directory) / "specadapter"
            if not args.adapter:
                subprocess.run(["go", "build", "-o", str(adapter),
                                "./internal/conformance/cmd/specadapter"],
                               cwd=corpus.ROOT, check=True, stdout=sys.stderr)
            report = run_cases(cases, selected, ledger, adapter, normalize, args.require_complete)
        if args.json:
            print(json.dumps(report, ensure_ascii=False, indent=2))
        else:
            report_text(report)
        return 0 if report["gate_passed"] else 1
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print(f"CommonMark conformance: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())

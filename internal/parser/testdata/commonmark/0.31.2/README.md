# CommonMark 0.31.2 corpus

This directory retains all 652 examples across 26 sections from the original
CommonMark 0.31.2 specification. It is test infrastructure; ingestion does not
establish parser, correction, GFM, or Pandoc conformance.

`upstream/` contains unmodified files from
[commonmark/commonmark-spec at 9103e341a973013013bb1a80e13567007c5cef6f](https://github.com/commonmark/commonmark-spec/tree/9103e341a973013013bb1a80e13567007c5cef6f).
The retained spec has SHA-256
`257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80`.
`provenance.json` records each input's origin, revision, retrieval date, size,
checksum, original Git mode, license, and role. Generator versions and generated
artifact hashes are recorded separately.

`examples.json` is the exact UTF-8 output of upstream
`test/spec_tests.py --spec <retained-spec> --dump-tests`. Do not edit it by hand.
Each record preserves `markdown`, `html`, `example`, `section`, `start_line`, and
`end_line`. IDs 1–652 are upstream identities; line fields address the upstream
specification, not a future parsed Markdown document. The extractor converts
upstream tab arrows to actual tabs in both Markdown and HTML. It preserves final
newlines in these values; example 207 has legitimate empty expected HTML. The
JSON serialization itself has no terminal newline, matching upstream output.
No HTML normalization or expected-output adaptation is applied.

Use Python 3.9 or newer, with only its standard library. From the repository root:

```sh
python3 -B tools/commonmark_corpus.py
python3 -B tools/commonmark_corpus.py --check
python3 -B -m unittest discover -s tools -p 'test_commonmark_corpus.py' -v
```

Regeneration writes only `examples.json`. Check mode verifies every retained
input, independently regenerates output in memory, and compares exact fixture
bytes without writing files. It exits 0 for a match and 1 for an input,
extraction, or fixture error, with errors on stderr. After reviewing intentional
fixture edits, regenerate to replace them with the original upstream output.
For damaged retained inputs, restore the pinned original bytes; regeneration
does not download replacements or silently update provenance.
Newer Python versions may emit warnings about the unmodified upstream helper;
these remain on stderr and do not change the byte-verified generated data.

Paths are resolved relative to the tool, so from another directory use
`python3 -B /absolute/path/to/mdchk/tools/commonmark_corpus.py --check` (omit
`--check` to regenerate). Neither operation needs network access, a developer's
local spec path, a cmark executable/native library, or third-party Python
packages. The extractor imports `cmark.py` and `normalize.py` unconditionally;
both are retained, but dump mode exits before renderer construction or HTML
normalization. Tool status messages are separate from the generated JSON file.

The integration tests use disposable copies. They compare all fields to the real
upstream dump, check repeat generation from an unrelated directory, and require
non-rewriting failures for changed HTML, a removed case, corrupt retained inputs,
and a missing helper. CI runs these tests and the read-only drift check.

## Foundation coverage accounting

`foundation.json` is mdchk's explicit, separately maintained pending inventory.
It lists every upstream ID exactly once as pending, with no supported HTML cases.
Owners #6/#18 provide the remaining block/inline pipelines and syntax deliveries.
The test-only adapter and executable HTML comparison runner are available; #5 is
no longer an undelivered owner. The inventory does not alter upstream inputs,
expectations or provenance. Regeneration still writes
only examples.json.

`go test ./internal/parser` checks complete/unique accounting against the retained
corpus and verifies every input has explicit foundation-stage uncertainty. These
are coverage-contract assertions, not HTML-conformance passes. See the
[model contract](../../../model.md) for supported outcomes and later ownership.

`scanner.json` is a separate mdchk capability-target registry. It names exactly
Tabs IDs 1–11 as scanner/indentation validation targets. `TestScannerCorpusTargets`
checks their unique upstream identities, Tabs section and exact original input
spellings, then reconstructs physical lines and exercises scanner helpers without
changing the source. Independent coordinate, character, NUL and partial-tab fixtures
verify helper behavior. These checks add no supported HTML cases; all 652 IDs remain
pending under their block/inline owners. Do not rewrite upstream expectations
or count helper assertions as HTML comparison passes.

## Executable comparisons and passing baseline

Run `python3 -B tools/commonmark_conformance.py` from the repository root, or pass
`--strict` for exact HTML rather than retained-upstream-normalized comparison.
See [contributor instructions](../../../../../docs/contributing.md#run-commonmark-regression-checks)
for build-once adapter commands, filters, JSON, exit codes and baseline maintenance.

The default staged regression gate allows explicitly pending cases: currently
0 pass, 0 fail, 0 error, 652 pending, with `syntax-unimplemented` and owners #6/#18.
It is not a conformance-completion claim. `--require-complete` fails on pending;
final completion needs all 652 passes in an unfiltered run. The retained upstream
helper has no pending status and counts the adapter's nonzero pending exit as
error. Keep that behavior distinct from mdchk's explicit pending report.

The runner validates all original IDs before selection, rejects supported-pass
regressions including pass-to-pending, and requires newly passing IDs to be recorded
in `supported_html_examples`. Move those IDs out of pending groups in reviewed
syntax deliveries; retain prior passes and independent AST/source-span assertions.
No command automatically rewrites this ledger or generated upstream expectations.
CI checks the gate, and independent synthetic baselines verify that it rejects
regressions even while the real parser has zero supported upstream cases.

## Attribution and licenses

The specification and derived examples are Copyright (C) 2014–16 John MacFarlane,
under CC-BY-SA 4.0. The upstream Python test software is Copyright (c) 2014
John MacFarlane, under its BSD-style terms. Normalization code includes material
derived from the markdowntest project, Copyright 2013 Karl Dubost, under MIT.
The unmodified [upstream LICENSE](upstream/LICENSE) retains these original
copyright notices, terms, and disclaimers; do not assign one blanket software
license to all imported artifacts. Source files are unmodified; the derived
JSON is produced solely by the upstream dump operation described above.

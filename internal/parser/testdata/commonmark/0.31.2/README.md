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

`foundation.json` is mdchk's separately reviewed pass/pending inventory. Every
original upstream ID occurs exactly once: **239 supported HTML cases and 413
pending**, preserving all 87 previous passes and adding 152 from inline text.
The parser recognizes the block structure described in the
[model contract](../../../model.md), collects document-wide definitions, and
constructs source-preserving Text children in paragraphs and headings after the
block/reference handoff. Code and raw HTML payloads stay opaque.

The unchanged 650–652 cases now pass actual Parse/Evaluate. Example 327 remains
pending for #20; unmatched backticks in 347/348 are literal. Independent inline
AST/source assertions also cover Tabs 4/5/9/10, literal backslashes 13, block-local
backticks 42, unmatched emphasis markers and invalid/bare autolinks. Existing
paragraph/ATX/Setext/thematic/code/HTML/quote/list tests retain their independent
block envelopes, marker ranges, original content and hierarchy, adding Text checks
where applicable. Exact byte ranges are separate assertions from HTML comparison.

`TestInlineSegmentedSourceAndDetachedAccessors` verifies omitted prefixes, lazy
continuation, nested quote/item text, both heading styles, original endings/EOF,
Unicode/NUL provenance and immutable Text accessors. Matched syntax whose provider
is undelivered remains pending, even beside supported text. Dispatch/state hooks
alone never establish syntax support; conservative checks also defer some invalid
candidates until their actual handlers can prove fallback. Multiline mapping
preserves exact source endings while semantic breaks remain #21.

The ledger no longer lists #18 as an undelivered provider. Remaining scopes are
#19 decoding, #20 code spans, #21 breaks, #22 autolinks, #23 inline HTML, #24/#25
emphasis, #26/#27 links and #28 images, as applicable to each case. Auxiliary
coverage owners can be conservative when a later code/HTML handler may prove
content opaque; they are not reverse native prerequisites or conformance passes.
Thematic 55, for example, remains pending for conservative delimiter
classification (#24/#25), while 57–61 now pass. Refer to the exact per-ID ledger rather than inferring support from a
section name. Source-only structural progress is not a complete HTML pass.

Every unchanged 192–218 definition, 219–227 paragraph, 62–79 ATX, 80–106 Setext,
43–61 thematic, 107–118 indented, 119–147 fenced, 148–191 HTML, 228–252 quote,
253–300 list-item and 301–326 list case retains independent actual-Parse/source
coverage. The tests preserve invalid fallback, precedence, stripping/trimming,
partial tabs, NUL, CRLF/CR/LF/EOF, blank ownership, initial ordinals, list tightness,
non-rendered definition boundaries and protected payload behavior. Constructed
renderer fixtures remain separate from actual parser recognition.

Deterministic cancellation/deadline checks cover block/reference and inline work.
Interrupted inline construction does not mutate already frozen block handles or
publish a partly constructed tree; strict invalid-UTF-8 failure keeps precedence.
Bounded fuzz targets retain source, reference, list and inline provenance oracles.
These checks do not establish full resource/work/depth-budget enforcement (#31),
fix safety or production CLI/MCP parity.

The runner verifies exact upstream identities and provenance, compares actual
HTML, rejects lost passes and unrecorded new passes, and never rewrites this
ledger or generated expectations. Regeneration still writes only examples.json.
Final completion requires all 652 cases without skips or exemptions.

## Reference-definition recognition

Every unchanged 192–218 input has independent actual Parse record/source checks.
The tests assert title fallback, paragraph noninterruption, Setext remnants,
consecutive/container definitions and original components/marker envelopes;
additional fixtures cover invalid/999/1000-character labels, full Unicode folding,
raw escape/entity identity, duplicate first selection, empty/absent components,
LF/CRLF/CR/EOF, prefixes/tabs, immutability, literal code/HTML and interruption.
Definition-only 207 has one actual definition, no paragraph, complete phases and
empty HTML. List 317 retains all three items, its non-rendered definition and loose
outer list. #17 ownership is removed; remaining 192–218 cases identify actual
#19/#21/#23/#27 providers in the ledger. No forward resolution is claimed.

## Executable comparisons and passing baseline

Run `python3 -B tools/commonmark_conformance.py` from the repository root, or pass
`--strict` for exact HTML rather than retained-upstream-normalized comparison.
See [contributor instructions](../../../../../docs/contributing.md#run-commonmark-regression-checks)
for build-once adapter commands, filters, JSON, exit codes and baseline maintenance.

The default staged regression gate allows explicitly pending cases: currently
239 pass, 0 fail, 0 error, 413 pending, with `syntax-unimplemented` and the
remaining owners above. All 87 earlier passes remain; text scanning adds 152 passes. Synthetic
empty/ASCII-blank documents also complete and render empty HTML. It is not a
conformance-completion claim. `--require-complete` fails on pending; final
completion needs all 652 passes in an unfiltered run. The retained upstream
helper has no pending status and counts the adapter's nonzero pending exit as
error. Keep that behavior distinct from mdchk's explicit pending report.

The runner validates all original IDs before selection, rejects supported-pass
regressions including pass-to-pending, and requires newly passing IDs to be recorded
in `supported_html_examples`. Move those IDs out of pending groups in reviewed
syntax deliveries; retain prior passes and independent AST/source-span assertions.
No command automatically rewrites this ledger or generated upstream expectations.
CI checks the gate, and independent synthetic baselines verify that it rejects
regressions independently of the real parser's supported-case count.

## Attribution and licenses

The specification and derived examples are Copyright (C) 2014–16 John MacFarlane,
under CC-BY-SA 4.0. The upstream Python test software is Copyright (c) 2014
John MacFarlane, under its BSD-style terms. Normalization code includes material
derived from the markdowntest project, Copyright 2013 Karl Dubost, under MIT.
The unmodified [upstream LICENSE](upstream/LICENSE) retains these original
copyright notices, terms, and disclaimers; do not assign one blanket software
license to all imported artifacts. Source files are unmodified; the derived
JSON is produced solely by the upstream dump operation described above.

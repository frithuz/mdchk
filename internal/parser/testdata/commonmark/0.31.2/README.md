# CommonMark 0.31.2 corpus

This directory retains all 652 examples across 26 sections from the original
CommonMark 0.31.2 specification. It is test infrastructure; ingestion does not
establish parser, correction, or GFM conformance.

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
original upstream ID occurs exactly once: **652 supported HTML cases and 0
pending**, preserving all 629 previous passes and adding the 23 image cases.
The parser recognizes the block structure described in the
[model contract](../../../model.md), collects document-wide definitions, and
constructs source-preserving Text/CodeSpan/break/InlineHTML/Link/Image/Emphasis/Strong children in paragraphs and headings after the
block/reference handoff. Code and raw HTML payloads stay opaque.

Decoding supplies known semantic Text, reference destination/title and fence-info
values with separate Escape/Entity provenance. New complete passes are 12, 24, 26,
27, 29, 30, 34, 38–40, 65, 76, 102, 563 and 592. Unchanged examples 12–41 have
independent decoder/AST/source checks, including malformed-literal and one-pass
structure scenarios. Complete authoritative named-entity coverage is independently
checked against the pinned [WHATWG dataset](../../html5/README.md).
Code/raw HTML blocks stay opaque. Code spans also consume literal escapes/entities;
raw code closers ignore backslashes. Entity-produced punctuation/tabs/newlines
remain Text and do not create syntax. Physical endings outside code now produce
source-preserving SoftBreak/HardBreak nodes when eligible.

The unchanged 650–652 and 327 cases pass actual Parse/Evaluate; unmatched backticks
in 347/348 remain literal. Independent inline
AST/source assertions also cover Tabs 4/5/9/10, literal backslashes 13, block-local
backticks 42, unmatched emphasis markers and invalid/bare autolinks. Existing
paragraph/ATX/Setext/thematic/code/HTML/quote/list tests retain their independent
block envelopes, marker ranges, original content and hierarchy, adding Text checks
where applicable. Exact byte ranges are separate assertions from HTML comparison.

`TestInlineSegmentedSourceAndDetachedAccessors` verifies omitted prefixes, lazy
continuation, nested quote/item text, both heading styles, original endings/EOF,
Unicode/NUL provenance and immutable Text accessors. All core inline handlers are
delivered; malformed candidates retain ordinary core interpretation. Multiline mapping
preserves exact source endings and separate semantic break nodes.

Code spans add passes 17, 35, 121, 327–333, 335–343, 345, 349, 525, 537 and
640–641. `TestCodeSpanUpstreamAcceptanceAndNewPasses` asserts independent node,
literal, delimiter and raw-payload ranges alongside original HTML. New fixtures
cover ASCII-only edge trimming, all-space/tab/NBSP content, all physical endings,
nested/lazy containers, headings, NUL/Unicode, mixed Text decoding, ownership and
isolation. The normalizer retains trimmed source bytes and explicit transformation
provenance; semantic offsets never become source coordinates. Code endings are
normalized internally; 334 now completes with its external soft break, while 344
now completes through raw inline HTML and 346 now completes with its opaque autolink.

`TestBreakUpstreamAcceptance` checks all unchanged Hard/Soft line break inputs
633–649, with independent kind/origin/marker/ending/Text assertions. Newly supported
633–637 and 648–649 join already-supported terminal cases 644–647; code exclusions
640–641 remain supported. Emphasis 638–639 now renders, and inline HTML
642–643 now complete without internal break nodes. Full-corpus evaluation adds
52 passes across paragraph/heading/list/quote continuations, escapes/entities and
code-span 334. Exact per-ID support is in the ledger. Independent fixtures cover
original LF/CRLF/CR and Unicode/NUL coordinates, whitespace and generated-character
boundaries, nested/lazy containers, opaque contexts, immutable accessors and
interruption without publishing unfinished children. A soft break's one discarded
space is retained as Normalized empty Text; hard markers retain their entire raw
spelling separately from ending Content. Shared fixture/provenance oracles keep
old block assertions and accept ordered Text/CodeSpan/break/InlineHTML/Link/Image/Emphasis/Strong children.

Angle autolinks add 24 actual passes: 20, 346, 480–481, 526, 538, 594–610 and
literal HTML fallback 618. All 19 unchanged Autolinks cases 594–612 now pass;
bare 611–612 remain ordinary Text. `TestAutolinkUpstreamAcceptanceAndNewPasses`
independently checks node kinds, raw label/destination semantics, Synthetic email
prefix provenance, exact angle markers/ranges and opaque label children alongside
actual Parse/Evaluate HTML. Additional grammar/source fixtures cover boundary
lengths, controls/space, Unicode/NUL, escaped/generated angles, code precedence,
invalid fallback and raw inline HTML, containers/headings, all endings,
immutable ownership and interruption. Existing inline/list fuzz consumers preserve
their previous assertions and independently check autolink grammar/provenance.
No upstream input, helper, normalizer or expected output changes.

Inline raw HTML adds 28 passes: 187, 344, 475–477, 524, 536, 613–617, 619–632
and 642–643. All unchanged Raw HTML 613–632 now pass; 618 was already literal
fallback. Actual Parse/Evaluate comparisons are paired with independent original
HTML-node byte envelopes, a separate declarative raw grammar oracle, ordered
source contributions and exact confirmed-comment closing markers. Additional
fixtures cover short/multiline comments, quoted opacity, rejected-input decoding,
all seven block boundaries, headings/containers and source gaps, Unicode/NUL,
LF/CRLF/CR/EOF, immutable ownership and deterministic interruption. Shared
inline/list fuzz consumers validate InlineHTML without weakening prior checks.
Pinned upstream input and expected output bytes are unchanged.

Single-level emphasis added 94 passes while preserving 383 previous passes.
Nested construction now adds the 36 specified cases while preserving all 477
established results. TestEmphasisApprovedPartitionAndUpstream retains the fixed
350–481 partition: 90 established plus 36 nested passes, and six inline-link
cases now completed by #26. Parent raw Content retains nested syntax bytes;
recursive source assertions check independently counted consumed markers, original
envelopes, child order/coverage and leftovers alongside actual Parse/Evaluate HTML.
The original 18 single-level cross-section additions remain supported; this nested
increment unlocks no additional cross-section case.

Separate tests cover repeated/scoped stack-bottom processing and suffix cleanup,
nested/lazy source gaps, headings, NUL/Unicode, physical endings/EOF, partial tabs,
opaque interiors, detached nested handles, adversarial long runs and deterministic
interruption. Inline/list fuzz consumers retain their source oracles and nested
seeds. No pinned upstream source or expected HTML bytes change.

The ledger no longer lists #17–#28 as undelivered providers. Images complete
517/520/531 and 572–591. Cases 419/433/516/567/530/532/568/593 complete.
Coverage owners are not reverse native prerequisites or conformance passes.
Thematic 55 completes through literal delimiter classification; 56–61 also pass.
Refer to the per-ID ledger rather than inferring support from a section name.
Source-only structural progress is not a complete HTML pass.

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

Inline-link delivery adds all 47 sole-#26 cases plus empty-inline precedence
567. Full-corpus inspection also promotes 197/199/201/209/211/212/213/511/523/
546/547/548/551/552 as literal fallback with a completed empty definition inventory.
`TestInlineLinkApprovedPartitionAndUpstream` uses unchanged inputs/HTML plus
independent component/marker/envelope fixtures for all 62 promotions. Origin-aware
source oracles, mapped containers/headings, Unicode/NUL, empty values, physical
endings, opacity, nested-link/emphasis boundaries, ownership and deterministic
interruption cover the actual parser. Reference uses and image construction are covered by the deliveries below. Existing supported IDs and immutable upstream expectations are preserved.

## Reference-definition recognition

Every unchanged 192–218 input has independent actual Parse record/source checks.
The tests assert title fallback, paragraph noninterruption, Setext remnants,
consecutive/container definitions and original components/marker envelopes;
additional fixtures cover invalid/999/1000-character labels, full Unicode folding,
raw escape/entity identity, duplicate first selection, empty/absent components,
LF/CRLF/CR/EOF, prefixes/tabs, immutability, literal code/HTML and interruption.
Definition-only 207 has one actual definition, no paragraph, complete phases and
empty HTML. List 317 retains all three items, its non-rendered definition and loose
outer list. #17 ownership is removed; all 192–218 cases now render through actual Parse
and the completed document-wide reference resolver. Forward uses and duplicate
first-selection have independent identity/source assertions.

## Reference-use resolution

The 53 fixed sole-#27 examples and escaped-marker 593 now complete with unchanged
HTML and independent AST/use/identifier/marker/source assertions and selected
first-definition indices. Full/collapsed/shortcut forms preserve separate visible
text and raw keys, exact use envelopes and authoritative definition provenance.
Projected destination/title semantics are explicitly spanless; the document-local
normalized-key lookup recovers the original definition. Negative binding, folding,
scalar limits, mapping, detached ownership and interruption tests supplement the
corpus. Inline/list fuzz consumers retain independent provenance checks.

All 629 prior passes remain. Images add 517/520/531 and 572–591 for 652 pass /
0 pending. `TestImageUpstreamAcceptance` directly ingests 572–593 and the three
image-dependent Links cases, with independent AST/source assertions. Nested semantic
alt text, bracket activation, reference projections and model binding tests
supplement the corpus. No pinned input or generated expectation was edited.

## Executable comparisons and passing baseline

Run `python3 -B tools/commonmark_conformance.py --require-complete` from the
repository root, or pass
`--strict` for exact HTML rather than retained-upstream-normalized comparison.
See [contributor instructions](../../../../../docs/contributing.md#run-commonmark-regression-checks)
for build-once adapter commands, filters, JSON, exit codes and baseline maintenance.

The default staged regression gate currently reports 652 pass, 0 fail, 0 error,
0 pending. Strict and normalized comparisons both pass the full unchanged corpus.
Synthetic empty/ASCII-blank documents also complete and render empty HTML.
`--require-complete` requires all selected cases to pass; an unfiltered run confirms
all 652 without exemptions. The runner retains explicit pending semantics for
synthetic protocol and regression tests; no real corpus case is pending.

The runner validates all original IDs before selection, rejects supported-pass
regressions including pass-to-pending, and requires newly passing IDs to be recorded
in `supported_html_examples`. Move those IDs out of pending groups in reviewed
syntax deliveries; retain prior passes and independent AST/source-span assertions.
No command automatically rewrites this ledger or generated upstream expectations.
CI requires the unfiltered complete gate. The tooling suite also invokes the
unchanged upstream `spec_tests.py` against a freshly built adapter in normalized
and strict modes, requiring exit 0 and exactly `652 passed, 0 failed, 0 errored,
0 skipped` in each mode. The contributor instructions provide reproducible
commands. Independent synthetic baselines verify that the complete gate rejects
pending cases even when the staged ledger is valid, and that the ratchet rejects
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

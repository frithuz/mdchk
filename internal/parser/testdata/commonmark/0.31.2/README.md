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

`foundation.json` is mdchk's explicit, separately maintained pass/pending
inventory. It lists every upstream ID exactly once: 84 supported HTML cases
(1–3, 6, 8, 11, 18–19, 21, 31, 36, 43, 47–48, 50–54, 69, 79, 85, 98, 100,
107, 110–112, 116–120, 122–127, 129–137, 139, 142–144, 146–147, 150–151,
153–154, 156–166, 171, 173, 178, 181, 183–184, 186, 189–191, 231, 236,
239–240, 289)
and 568 pending.
Indented/fenced code, thematic breaks,
HTML blocks, ATX/Setext headings, nested block quotes and the staged paragraph
lifecycle are implemented.
Documents of code with established info semantics, HTML, breaks, empty ATX headings
and blanks, including empty/inline-free quotes, complete with available
empty references and vacuous inline work; nonempty headings/paragraphs and unknown
input remain partial. Code-only passes in other sections do not establish general
list or inline recognition. Info requiring decoding remains pending #19.

Indented-code cases 108/109 retain list/container/reference/inline owners #15–#18;
113 retains #17/#18/#21; 114/115 retain #17/#18. Paragraph targets 219–227 retain
#17/#18, plus #21 where actual multiline/break processing applies; 225 now has
code and a following paragraph, without pending #10 ownership. ATX targets retain
#17/#18 for reference/text, #19 for escapes (65/66/76), #24/#25 for emphasis (66)
and #21 for multiline content (70). Thematic targets retain lists/containers
(#15/#16), reference/inline (#17/#18), breaks (#21) and emphasis (#24/#25) where
relevant. Setext 92/101 retain #17/#18, 93 also retains #21, and lists 94/99
retain #15/#16. Case 91 retains reference/text/code-span/inline-HTML providers
(#17/#18/#20/#23); recognized headings/fallbacks retain their actual reference/text,
multiline, escape and emphasis owners. Fenced
128 retains #17/#18; 121 retains #17/#18/#20; 138/145 additionally retain #21;
140/141 retain #17/#18 for surrounding text. Other
examples use broad remaining inline/core-completion scopes #18/#29, not a claim
that each needs every provider. Later coverage owners are not reverse native
blockers on staged deliveries. No fabricated inline interpretation establishes
progress. The #5 test-only adapter/runner is available; the inventory does not
alter upstream inputs, expectations or provenance.
Regeneration still writes only examples.json.

`go test ./internal/parser` checks complete/unique accounting against the
retained corpus and verifies parse outcomes match the ledger. Actual Parse
assertions from original examples 219–227 independently check paragraph
grouping and source spans; 225 asserts code followed by a paragraph; 227's heading
is now independently asserted. Those tests do not assert final HTML or use
constructed nodes as proof of recognition. These are coverage-contract
assertions, not HTML-conformance passes. `TestThematicUpstreamTargets`
separately exercises unchanged 43–61 and extra supported cases 11/98 with
independent actual-parser kinds, source envelopes and maximal contiguous
`thematic-marker` run ranges. `TestATXUpstreamTargets` adds all unchanged
62–79 plus 227, with independent node order, heading level/style,
opening/closing spans, trimmed source-backed content and full line envelopes.
It proves actual Parse/Evaluate output for empty-heading 79 without fabricated
inline children. `TestSetextUpstreamTargets` covers unchanged 80–106 and 59,
independently asserting whole-paragraph promotion, level/style, exact raw content,
underline-only markers and multiline physical envelopes, plus following blocks
and fallback/precedence. Nineteen Setext-section cases establish structural or
heading/paragraph progress, including 91's invalid multiline-tag suffix. Quotes
92/101 now precede outside breaks and 93 retains lazy text; list cases 94/99 now
retain the first item prefix, with continuation/closure and grouping still pending.
Cases 85/100 pass as code and breaks, and 98 remains two breaks. Source-only
deferral is not recognized heading coverage.
Extra Setext fixtures cover single markers, whitespace/backslash handling, tabs,
Unicode/NUL, LF/CRLF/CR/EOF, frozen accessors and interruption before/after
promotion. Nonempty/unsupported cases stay pending; extra ATX fixtures
check raw escapes, ASCII-only whitespace, Unicode/NUL provenance, endings/EOF,
completion guards and immutable ownership. Together these actual parser tests
cover the supported HTML outputs; pending cases stay distinct.
`TestIndentedUpstreamTargets` covers all unchanged 107–118 and newly passing
code-only inputs 1–3, 8, 18, 36, 134, 231 and 289 with independent source and
literal expectations. Thematic 48, ATX 69 and Setext 85/100 assert code in their
own fixtures. Extra code tests distinguish raw slices from normalized Literal,
LF/CRLF/CR and synthetic EOF LF provenance, indentation markers and trailing
blanks/whitespace; they also verify immutable snapshots and truthful interrupted
versus complete/pending outcomes. No code content is parsed inline. Independent
fixtures cover paragraph interruption, Setext/spaced-dash precedence, negative
markers, tabs/indentation, Unicode/NUL, endings/EOF, source ownership and
interruption. See the [model contract](../../../model.md) for supported
outcomes and later ownership.

`TestHTMLUpstreamTargets` adds all unchanged 148–191 plus cross-feature 21/31,
with independent original-byte envelopes/content ranges, form identity, node order
and source ownership. Twenty-five HTML-section cases pass; nineteen mixed or
container cases remain pending. Case 174 retains its quote and outside paragraph
(#17/#18); 175 retains list owners, and other
mixed cases retain their precise reference/text, break, inline-HTML and emphasis
providers in the ledger. Fixed-form opacity, paragraph interruption, complete-tag
near misses, terminating-line retention, code precedence, LF/CRLF/CR/EOF, NUL,
Unicode, tabs, immutable accessors and interruption have separate fixtures.
HTML preserves original endings in raw Content, with semantic NUL substitution;
recognition adds no sanitizer, inline interpretation or production renderer.

`TestQuoteUpstreamTargets` exercises all unchanged 228–252 with independent actual
Parse nesting, node order, envelopes, per-line prefix markers and logical content
ranges. Cases 236/239/240 newly pass HTML; literal-code 231 remains supported.
`TestQuotePartialTabUpstream6` verifies the fourth new pass with exact original
partial-tab/code provenance. Setext 92/93/101, fence 128 and HTML 174 also assert
quote structure. Reference cases 214/218 remain pending for #17/#18/#27; quote 235
retains #15–#18 list ownership, and other pending quote cases retain #17/#18 with
#21 where multiline text requires it. Separate fixtures verify all seven HTML
forms at quote boundaries, opaque payload, prefix-free raw content and residual
tabs, marked Setext versus unmarked laziness, omitted nested prefixes, code
transitions, NUL/Unicode/endings/EOF and immutable ownership. Deterministic checks
discard unfinished nested containers on cancellation/deadline while retaining
closed prefixes; strict UTF-8 failure keeps precedence. No copied stripped source,
new runtime dependency, public schema or production renderer is introduced.

`TestListMarkerUpstreamTargets` independently asserts all unchanged List items
examples 253–300: 40 bounded first-line item prefixes, seven non-item cases and
existing complete code case 289. Markers retain raw spelling/ordinal, effective
padding, relative content indentation and source-backed Prefix contributions,
including consumed/residual tab provenance. Parsing stops at the observed line;
no continuation/closure, List parent, nested content or tightness is guessed.
#15 owns continuation/closure and #16 grouping/tightness; reference/text/break
providers remain as individually recorded in foundation.json. Earlier cases
94/99, 108/109 and 235 likewise retain the first prefix, with #15–#18 remaining.
See the [bounded marker contract and per-case coverage table](../../../model.md#first-line-list-markers-and-staged-item-prefixes).
This structural progress adds no HTML pass: the same 84 supported IDs and 568
pending cases remain, and final completion still requires every original case.

`scanner.json` is a separate mdchk capability-target registry. It names exactly
Tabs IDs 1–11 as scanner/indentation validation targets. `TestScannerCorpusTargets`
checks their unique upstream identities, Tabs section and exact original input
spellings, then reconstructs physical lines and exercises scanner helpers without
changing the source. Independent coordinate, character, NUL and partial-tab fixtures
verify helper behavior. These checks add no supported HTML cases: example 11's
HTML pass comes from thematic parsing, not scanner helpers. Do not rewrite
upstream expectations or count helper assertions as HTML comparison passes.

`TestFencedUpstreamTargets` reads all original 119–147 and cross-feature 19 with
independent actual Parse assertions for node order/style, literal payload, raw
info, fence character/width, token-only markers, indentation and physical envelopes.
Twenty-three fenced-section cases match HTML, including prior indented-code 134;
the six pending cases retain the actual providers listed above. Separate fixtures
cover literal HTML/reference/entity/directive-looking payload, info escaping and
unavailable decoding, empty/blank/unclosed/EOF blocks, LF/CRLF/CR, partial tabs,
NUL/Unicode, immutable accessors, encoding precedence and interrupted prefixes.
A valid missing closer is distinct from interruption, and constructed nodes or
HTML alone cannot establish source-span acceptance. Source bytes and upstream
expectations remain unchanged; see the model's fenced-code contract.

## Executable comparisons and passing baseline

Run `python3 -B tools/commonmark_conformance.py` from the repository root, or pass
`--strict` for exact HTML rather than retained-upstream-normalized comparison.
See [contributor instructions](../../../../../docs/contributing.md#run-commonmark-regression-checks)
for build-once adapter commands, filters, JSON, exit codes and baseline maintenance.

The default staged regression gate allows explicitly pending cases: currently
84 pass, 0 fail, 0 error, 568 pending, with `syntax-unimplemented` and the
remaining owners above. All 80 earlier passes remain; four quote-related passes
are added (6, 236, 239, 240). Synthetic
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

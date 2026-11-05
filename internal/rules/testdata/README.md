# Reusable rule analysis fixtures

`md047.json` records original UTF-8 source and independent expected diagnostics.
`expected: null` means no finding; otherwise the six integers are original byte
start/end, one-based start line/Unicode-scalar column, and exclusive end
line/column. Tabs and combining scalars each count as one column. Endings are
literal escaped JSON bytes; CRLF is indivisible. Findings have rule MD047,
error severity and `fix_available: false`; missing material and surplus suffixes
have distinct messages asserted by the engine fixture test.

The fixtures are analysis-only inputs for downstream correction and transport
acceptance. Correction #145 must add separate edit/preservation expectations;
this data does not assert correction safety. The test also retains the parser's
`source_consumers/{mixed,references}.json` contexts across LF/CRLF/CR expansion,
with absent, single and surplus terminal sequences.

`md001.json` records complete authored heading envelopes (including the ending
when present) in source order. Container prefixes precede the heading envelope.
Each expected item gives the same six coordinates in `range` and an independent
literal `source` slice. Empty `expected` means no finding. Tests run both flavors
with explicit recognition/title selections and pin affected metadata coverage.
All MD001 findings remain report-only; no edits or changed documents are emitted.

`md022.json` fixes original byte/Unicode points and before/after messages for both
CommonMark and GFM. Its literal options exercise independent minima, extra blanks,
actual document edges, complete Setext extents, parser-owned container prefixes,
marker-looking prose, protected neighbors, headers and ASCII-only blankness.
The NUL fixture is asserted after JSON decoding. Repeats preserve original source
identity/bytes, produce no fixes and retain explicit uncertainty where required.

`md005.json` records fixed original marker byte/scalar ranges for both flavors.
Each actual group uses its first direct item as a structural indentation baseline;
nested traversal retains parent baselines. Full ordered markers are selected.
Protected syntax, empty/task/definition items, tabs, outer prefixes, ordinal
width, group splits, headers, Unicode and all line endings retain manual findings.


`md027.json` supplies literal original sources, SHA-256/size, independent recognition/completeness and optional flavor with exact excess-SPACE byte/scalar ranges, source slices and constant manual messages. Every consumer honors Flavor. `md027_active_metadata.json` and `md027_composition.json` retain independently fixed metadata/directive and MD047-only candidate geometry shared by native acceptance and normal-binary evidence.

`md044.json` preserves the 67 independent source-authored preimplementation MD044
literals unchanged, including format-specific URL ownership and exact byte/scalar
ranges. The fixture test verifies the frozen digest and original snapshot hashes.
Additional table/header/raw/control and option boundary cases live in md044_test.go.

`md049.json` is the frozen root source-authored 64-case MD049 oracle, adopted
before implementation or parser probes (SHA-256
`5faab73b3e65c2dbd6c967058e7aa8666a8f12ed5d787007c666514cf9fef1b6`).
Its 123 selected flavor executions reuse 64 independent literals and 62 authored
opening ranges. Batch/reverse/concurrent cases are additional isolation checks,
not independent oracle counts. Expected output must not be regenerated from
production results.

`md050.json` is the exact independent root MD050 V2 oracle: 68 records, 62
distinct source-byte strings, 49 two-byte Opening ranges and 129 selected
CommonMark/GFM executions. V2 explicitly corrects two original semantic
expectations after preserved first failures, following independent pinned-grammar
review, and adds three source-authored positive controls before their execution.
The original oracle and failure evidence remain preserved in local working notes. Tests
verify the complete file digest, original source digests and byte/scalar geometry,
manual analysis and immutable source. It is independent of production output.

`md065.json` is the exact frozen independent root-authored issue-77 oracle (SHA256
c337a9cb0d793790beecea345baf60c90a72016a10f10c45bd12858f77b10dfa), authored
before engineering or product/parser/helper probes. All 80 records retain original
source size/SHA, selected settings/flavors, independent side/line/prefix witnesses
and exact zero-width byte/scalar/message expectations. They represent 76 distinct
source byte strings, 75 authored ranges and 159 flavor repetitions. The fixture
test asserts the whole-file digest and original snapshot/manual/no-edit evidence;
supplementary tests cover ownership floors, raw tails, strict configuration,
suppression owners, incomplete syntax, isolation and original-operation controls.


`md081.json` is the byte-exact independent root-authored issue-78 oracle, SHA-256
501e217a9bb3657c605c47964b988ffdcb8cf50e916f5d721af8e9da58410ed5,
frozen before engineering or probes. All 107 records retain 88 distinct original
sources, 108 literal paragraph count witnesses, 66 full Content-hull ranges and
206 flavor repetitions. Tests assert the whole-file digest, source digests,
scalar witness arithmetic, original byte/scalar geometry, messages, settings and
manual/no-edit/source-immutable results. JSON floating decoding does not replace
separate trusted API and actual YAML/JSON numeric-type admission checks. Expected
results must never be regenerated from production output.

`md062.json` is the independent issue-104 MD062 oracle (SHA-256
`fb015ce6170104d19665a9e5ddfe96738fd8b67f4982dd7b3eebf36c6f796f6b`): 78
records with original source size/SHA, selected flavors and settings, and
authored candidate byte/scalar ranges with the constant
`Whitespace in link destination.` message. They cover the page examples, image
and tab candidates, every grammar exclusion, protected code, raw HTML and
frontmatter, containers, emphasis, GFM table cells, multibyte columns and
LF/CRLF endings. Its `invalid_configurations` rows are read by the config test.
Expected results must never be regenerated from production output.

MD092 has three separate immutable root-authored literal fixture sets:

| Fixture | Cases | SHA256 |
| --- | ---: | --- |
| `md092.json` | 81 | `1a60725172f8b8e5038604145766f2c9cd9c9ce00b3acb16d8b3ca078477bb25` |
| `md092_raw_lifetime.json` | 6 | `1d5ab8daa2ce6d3315e7439f36311d22a24881aac59b9d3f3831f81a2131df32` |
| `md092_unknown.json` | 3 | `486afc71751f0ea8c3ffd8a94f7bdfb09dced1731cd36a9ded5e0ce801827aed` |

These 90 sources specify 180 CommonMark/GFM flavor rows, each exercised through
both full production lint and inspection. Fixture hashes, full ordered visible,
suppressed and MD087 findings, audit decisions, coverage/completeness, exact
original geometry/snapshot identity and unchanged bytes are asserted. The raw
supplement preserves incoming versus owned raw transitions and actual form-1
ANY4 boundaries. The unknown supplement distinguishes atomic withholding from
known opaque remainder protection. These are fixed expectations; fixture presence
and planned row counts are not evidence of execution. Never fit them to output.

`md053.json` holds 36 hand-authored MD053 cases, each run under CommonMark and
GFM through both production lint and inspection, and by the compiled binary in
`cmd/mdchk/md053_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `ignored_labels` list, and `findings` with original `bytes`,
1-based `start`/`end` line and Unicode-scalar column pairs, `expected`
(`unused` or `shadowed`) and the exact `message`. The cases cover the rule-page
examples, unused and shadowed definitions, case, whitespace and full-folding
label variants, full, collapsed, shortcut and image uses, block-quote and
nested-list definitions, brackets in code spans, fenced and indented code and
HTML blocks, normalized and empty `ignored_labels` entries, LF and CRLF
documents, non-ASCII and multi-line labels. Tests pin the whole-file digest
(`fe652ecb027e06779a25cb37dcfcf65b1bb63396e7580b25d51aff406f39c58f`). Expected
values were written by hand; never regenerate them from production output.

`md055.json` holds 38 hand-authored MD055 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md055_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `style`, and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Unexpected pipes cover the one pipe byte; missing pipes are zero-width points.
The cases cover both rule-page examples, every style, `consistent` headers with
both, leading-only, trailing-only and no outer pipes, escaped edge pipes, code
span pipes, empty edge cells, extra and missing body cells, block-quote and
list-item tables, pipe text in fenced and indented code, HTML blocks and
paragraphs without a delimiter row, LF and paired CRLF input, trailing spaces,
non-ASCII cells, multiple tables and a CommonMark case. Its ten
`invalid_configurations` rows are read by the config test. Tests pin the
whole-file digest
(`5439144ca04395f31749913a620651b8c6c82ca67d33b4f729533df89b1dbefa`). Expected
values were written by hand; never regenerate them from production output.

`md056.json` holds 27 hand-authored MD056 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md056_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`,
and `findings` with original `bytes`, 1-based `start`/`end` line and
Unicode-scalar column pairs and the exact `message`. Each finding covers one
trimmed body row. The cases cover all four rule-page examples, short and long
body rows, a body row without pipes, a one-column table, escaped and unescaped
code-span pipes, a GFM header/delimiter mismatch, block-quote and list-item
tables, pipe text in fenced and indented code, HTML blocks and paragraphs
without a delimiter row, LF and paired CRLF input, leading indentation and
trailing spaces, non-ASCII cells, multiple tables with different widths and a
CommonMark case. Its eight `invalid_configurations` rows are read by the config
test. Tests pin the whole-file digest
(`99f108a6a78b9078e645d3c73b2697dec3c812958b3c124680b3b8dc4ff55dae`). Expected
values were written by hand; never regenerate them from production output.

`md058.json` holds 40 hand-authored MD058 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md058_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`,
and `findings` with original `bytes`, 1-based `start`/`end` line and
Unicode-scalar column pairs and the exact `message`. Every finding is a
zero-width point: before points start the table's first line after container
prefixes, after points end its last line before the line ending. The cases
cover both rule-page examples, before-side neighbours (heading, fenced code,
thematic break, reference definition), after-side neighbours (heading, fenced
code, block quote, bullet and ordered lists, HTML block, thematic break),
accepted spacing, document, block-quote and list-item edges, a prefix-only `>`
blank, tables in a quote, a list item and a quote inside a list item, indented
tables, trailing spaces, body rows, non-ASCII text, multiple tables, a missing
final ending, pipe text in fenced and indented code, HTML blocks and paragraphs,
raw HTML protection, LF and paired CRLF input, a CommonMark case and a disabled
case. Its six `invalid_configurations` rows are read by the config test. Tests
pin the whole-file digest
(`0329e5fe41d8e42d369e911602fe2085b3e9b002e7cd2beba0edb6ab45a4e095`). Expected
values were written by hand; never regenerate them from production output.

`md060.json` holds 46 hand-authored MD060 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md060_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `style`, and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Each finding covers one cell field; an empty `||` field is a zero-width point.
The cases cover both rule-page examples under all three styles, empty cells
under each style, rows with and without outer pipes including leading-only and
trailing-only rows, escaped pipes in a cell and in a code span, an unescaped
code-span pipe that splits a cell, multibyte text measured in code points,
unequal body widths, excess and missing cells, tabs in padding, right- and
center-placed text, block-quote and list-item tables, pipe text in fenced and
indented code, HTML blocks and paragraphs without a delimiter row, multiple
tables with independent widths, LF and paired CRLF input and a CommonMark case.
Its ten `invalid_configurations` rows are read by the config test. Tests pin
the whole-file digest
(`4dc4a37c8333402b1a9c5fe63b32e0b8caed5217b56909aa29ff433d08bd5837`). Expected
values were written by hand; never regenerate them from production output.

`md075.json` holds 37 hand-authored MD075 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md075_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
(and `frontmatter` for the front-matter case), and `findings` with original
`bytes`, 1-based `start`/`end` line and Unicode-scalar column pairs and the
exact `message`. Each finding covers one whole run, from the first line's
trimmed start to the last line's trimmed end. The cases cover all three
rule-page examples, each structure test on its own, runs that fail all three,
an incidental prose pipe, a run broken by a line without a structural pipe,
escaped pipes and backslash-run parity, code-span pipes, pipe text in fenced and
indented code, HTML blocks, front matter, setext headings and after a link
reference definition, a GFM header/delimiter mismatch, a valid table with short
and long body rows, a paragraph line directly before a table, block-quote,
list-item and quote-in-list runs, separate paragraphs, LF and paired CRLF
input, leading indentation and trailing spaces, non-ASCII cells, multiple runs,
`enabled: false` and a CommonMark case. Its eight `invalid_configurations` rows
are read by the config test. Tests pin the whole-file digest
(`999ebb4ad8cb5c3da271399b8f9845d2732a35d8f6b513c1683a62eeabf58eb0`). Expected
values were written by hand; never regenerate them from production output.

`md057.json` records 21 independent issue-102 MD057 cases, each run for both
CommonMark and GFM. Every case gives the original source with its size and
SHA-256, the `check_images` option, an optional `resources.index_file`, an
optional frontmatter format and a native layout of files, directories and
symbolic links created around the selected `docs/self.md`. Expected findings
carry original byte ranges, one-based line/Unicode-scalar start and exclusive
end positions, the literal source slice and the message; expected coverage
carries the same located range for context-unavailable records. Reference
origins report the whole Link or Image envelope; inline origins report the
destination bytes. LF and CRLF twins differ only in offsets. The expectations
were authored from the sources, not from production output, and must never be
regenerated from it.

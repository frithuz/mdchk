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

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

# MD013 fixtures

`md013.json` holds the MD013 analysis cases run by `md013_test.go` and by
`TestCompiledPlatformMD013ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. The options are `Length`, `CodeBlocks`, `Tables` and
`Headings`; `Recognize` and `Incomplete` set front matter recognition and
completeness, an optional `Flavor` restricts a case to one flavor, and
`PhysicalWidths` gives every line's independently counted width. Each expected
finding gives a `Range` of six integers: original byte start and end, then
one-based start line and Unicode-scalar column and exclusive end line and
column, with the exact source slice and message. MD013 is report-only.

`md013_active_metadata.json` fixes metadata and directive cases, with the
findings expected when front matter is recognized and when recognition is off.
`md013_composition.json` fixes MD047-only candidate geometry: each case gives
the source, the candidate after the MD047 edit with its SHA-256 and size, and
the MD013 finding expected on it. Both are read by `acceptance_test.go`.

# MD028 fixtures

`md028.json` holds the MD028 analysis cases run by `md028_test.go` and by
`TestCompiledPlatformMD028ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Recognize` and `Incomplete` set front matter recognition and
completeness, and an optional `Flavor` restricts a case to one flavor. Each
expected finding gives a `Range` of six integers: original byte start and end,
then one-based start line and Unicode-scalar column and exclusive end line and
column, with the exact source slice and message. MD028 is report-only.

`md028_active_metadata.json` fixes metadata and directive cases, with the
findings expected when front matter is recognized and the suppressions expected
when it is not. `md028_composition.json` fixes MD047-only candidate geometry:
each case gives the source, the candidate after the MD047 edit with its SHA-256
and size, and the MD028 finding expected on it. Both are read by
`acceptance_test.go`.

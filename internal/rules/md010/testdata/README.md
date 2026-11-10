# MD010 fixtures

`md010.json` holds the MD010 analysis cases run by `md010_test.go` and by
`TestCompiledPlatformMD010ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. The options are `CodeBlocks` and `Spaces`; `Recognize` and
`Incomplete` set front matter recognition and completeness. Each expected
finding gives a `Range` of six integers: original byte start and end, then
one-based start line and Unicode-scalar column and exclusive end line and
column, with the exact source slice and message. MD010 is report-only, so no
finding offers a fix.

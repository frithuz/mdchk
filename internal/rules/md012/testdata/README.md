# MD012 fixtures

`md012.json` holds the MD012 analysis cases run by `md012_test.go` and by
`TestCompiledPlatformMD012ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Maximum` is the configured blank-line limit; `Recognize` and
`Incomplete` set front matter recognition and completeness. Each expected
finding gives a `Range` of six integers: original byte start and end, then
one-based start line and Unicode-scalar column and exclusive end line and
column, with the exact source slice and message. MD012 is report-only, so no
finding offers a fix.

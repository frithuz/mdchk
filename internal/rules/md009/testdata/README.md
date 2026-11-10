# MD009 fixtures

`md009.json` holds the MD009 analysis cases run by `md009_test.go` and by
`TestCompiledPlatformMD009ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. The options are `Spaces` (`hard_break_spaces`), `Strict` and
`CodeBlocks`; `Recognize` and `Incomplete` set front matter recognition and
completeness, and an optional `Flavor` restricts a case to one flavor. Each
expected finding gives a `Range` of six integers: original byte start and end,
then one-based start line and Unicode-scalar column and exclusive end line and
column, with the exact source slice and message. `FixAvailable` is whether the
finding offers a fix, and `FixAvailableGFM`, when present, overrides it for GFM.

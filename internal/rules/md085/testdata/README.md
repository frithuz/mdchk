# MD085 fixtures

`md085.json` holds the MD085 analysis cases run by `md085_test.go` and by
`TestCompiledPlatformMD085ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Recognize` and `Incomplete` set front matter recognition and
completeness, and optional `Flavor` and `Formats` restrict where a case runs.
Each expected finding gives a `Range` of six integers: original byte start and
end, then one-based start line and Unicode-scalar column and exclusive end line
and column, with the exact source slice and message, and `FixAvailable` when the
finding carries a correction.

`md085_root_literals.json` holds root-level literal cases in the same shape,
without options, for the compiled analysis test. `md085_composition.json` fixes
MD085 and MD047 candidate geometry: each case gives the source and the candidate
after both edits in one run, each with its SHA-256 and size. Both are read by `acceptance_test.go`.

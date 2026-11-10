# MD085 fixtures

`md085.json` holds the MD085 analysis cases run by `md085_test.go` and by
`TestCompiledPlatformMD085ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Recognize` and `Incomplete` set front matter recognition and
completeness, and optional `Flavor` and `Formats` restrict where a case runs.
Each expected finding gives a `Range` of six integers: original byte start and
end, then one-based start line and Unicode-scalar column and exclusive end line
and column, with the exact source slice and message. MD085 is report-only.

`md085_root_literals.json` holds root-level literal cases in the same shape,
without options, for the compiled analysis test. `md085_composition.json` fixes
MD047-only candidate geometry: each case gives the source and the candidate
after the MD047 edit, each with its SHA-256 and size, and the MD085 finding
expected on the candidate. Both are read by `acceptance_test.go`.

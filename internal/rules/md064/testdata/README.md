# MD064 fixtures

`md064.json` holds the MD064 analysis cases run by `md064_test.go` and by
`TestCompiledPlatformMD064ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Maximum` is the configured space limit; `Recognize` and
`Incomplete` set front matter recognition and completeness, and optional
`Flavor` and `Formats` restrict where a case runs. Each expected finding gives a
`Range` of six integers: original byte start and end, then one-based start line
and Unicode-scalar column and exclusive end line and column, with the exact
source slice and message. MD064 is report-only.

`md064_root_literals.json` holds root-level literal cases in the same shape,
without options, for the compiled analysis test. `md064_composition.json` fixes
MD047-only candidate geometry: each case gives the source and the candidate
after the MD047 edit, each with its SHA-256 and size, and the MD064 finding
expected on the candidate. Both are read by `acceptance_test.go`.

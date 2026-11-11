# MD089 fixtures

`md089.json` holds the MD089 analysis cases run by `md089_test.go` and by
`TestCompiledPlatformMD089ProductionAnalysis` in `acceptance_test.go`. Keys are
lower camel case. Each case records a literal original `source` with its
`sha256` and `sizeBytes`, which the tests verify before use. `recognize` and
`incomplete` set front matter recognition and completeness, and optional
`flavor` and `formats` restrict where a case runs. Each expected finding gives a
`range` of six integers: original byte start and end, then one-based start line
and Unicode-scalar column and exclusive end line and column, with the exact
source slice and message. `manual` marks a document opening with an
unrecognized block, whose findings get no correction and keep
`fix_available: false`; every other finding is fixable.

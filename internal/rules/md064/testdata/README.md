# MD064 fixtures

`md064.json` holds the MD064 analysis cases run by `md064_test.go` and by
`TestCompiledPlatformMD064ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. `Maximum` is the configured space limit; `Recognize` and
`Incomplete` set front matter recognition and completeness, and optional
`Flavor` and `Formats` restrict where a case runs. Each expected finding gives a
`Range` of six integers: original byte start and end, then one-based start line
and Unicode-scalar column and exclusive end line and column, with the exact
source slice and message. Every finding of a complete case is fix-available,
except in cases marked `Manual`, whose findings the shared edit query withholds.

`md064_root_literals.json` holds root-level literal cases in the same shape,
without options, for the compiled analysis test. `md064_composition.json` holds sources whose MD064 and MD047 edits share one
round: each case gives the source and the candidate after both edits, each with
its SHA-256 and size. Both are read by `acceptance_test.go`.

`md064_corrections.json` holds the correction cases run by `fix_test.go` and
`fix_acceptance_test.go`. Each case gives the original `Source`, the corrected
`Candidate`, the `Maximum`, an optional `Flavor`, and an optional `Config`:
`md047` also enables MD047, `md009` enables MD009 and MD047, and `directives`
turns on directive processing, which only the compiled test runs.

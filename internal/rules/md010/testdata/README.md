# MD010 fixtures

`md010.json` holds the MD010 analysis cases run by `md010_test.go` and by
`TestCompiledPlatformMD010ProductionAnalysis` in `acceptance_test.go`. Each case
records a literal original `Source` with its SHA-256 and size, which the tests
verify before use. The options are `CodeBlocks` and `Spaces`; `Recognize` and
`Incomplete` set front matter recognition and completeness. A case with a
`Flavor` runs only in that flavor; GFM tables and tasks make some TABs
correctable only in CommonMark. Each expected finding gives a `Range` of six
integers: original byte start and end, then one-based start line and
Unicode-scalar column and exclusive end line and column, with the exact source
slice and message. `FixAvailable` is present and true only for a TAB in inline
paragraph, heading or table-cell text that `fix` corrects; the acceptance test
then expects a one-change MD010 preview instead of unchanged findings.

The correction cases themselves, with exact spans, replacements, line endings,
protected contexts, directives and MD009/MD047 composition, are in
`fix_test.go` and `TestCompiledPlatformMD010Corrections`.

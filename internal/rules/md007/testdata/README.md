# MD007 fixtures

`md007.json` holds the MD007 analysis cases read by `md007_test.go` and
`acceptance_test.go`. Each expected finding gives its six-integer `Range`
(original byte start/end, one-based start line/column and exclusive end
line/column), its literal source slice and whether it can be fixed.

`md007_corrections.json` holds the compiled fix cases run by
`fix_acceptance_test.go`. Each gives the original `source`, the expected
`candidate`, the `rules` and `edit_count` of the change, whether analysis
offers the fix and whether it is deferred, and a `config` variation.

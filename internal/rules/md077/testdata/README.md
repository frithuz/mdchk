# MD077 fixtures

`md077.json` holds the MD077 analysis cases read by `md077_test.go` and
`acceptance_test.go`. Each expected finding gives its six-integer `Range`
(original byte start/end, one-based start line/column and exclusive end
line/column), its literal source slice and the actual and expected
indentation columns.

`md077_corrections.json` holds the compiled fix cases run by
`fix_acceptance_test.go` and reparsed by `fix_test.go`. Each gives the
original `source`, the expected `candidate`, the `rules` and `edit_count`
of the change, and a `config` variation.

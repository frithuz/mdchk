# MD076 fixtures

`md076.json` holds the MD076 analysis cases read by `md076_test.go` and
`acceptance_test.go`, each with its `Style` option. Each expected finding
gives its six-integer `Range` (original byte start/end, one-based start
line/column and exclusive end line/column), its literal source slice and the
actual and expected spacing.

`list_spacing_corrections.json` holds the compiled fix cases run by
`fix_acceptance_test.go`. Each gives the original `source`, the expected
`candidate`, the `style`, the `rules` and `edit_count` of the change, the
number of MD076 findings still `reported`, and a `config` variation.

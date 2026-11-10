# MD004 fixtures

`md004.json` holds the MD004 analysis cases read by `md004_test.go` and
`acceptance_test.go`. Each case gives a `Source`, the `Marker` option (empty
means `consistent`), whether front matter is recognized and whether analysis
is incomplete. Each expected finding gives its six-integer `Range` (original
byte start/end, one-based start line/column and exclusive end line/column), its
literal source slice, its message and whether it is `Fixable`.

`md004_corrections.json` holds the compiled fix cases run by
`fix_acceptance_test.go` and reparsed by `fix_test.go`. Each gives the
original `source`, the expected `candidate`, the `rules` and `edit_count`
of the change, and a `config` variation.

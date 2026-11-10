# MD030 fixtures

`md030.json` holds the MD030 analysis cases read by `md030_test.go` and
`acceptance_test.go`, each with its `Spaces` option. Each expected finding
gives its six-integer `Range` (original byte start/end, one-based start
line/column and exclusive end line/column), its literal source slice and
whether it can be fixed.

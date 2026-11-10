# MD069 fixtures

`md069.json` holds the MD069 analysis cases read by `md069_test.go` and
`acceptance_test.go`. Each expected finding gives its six-integer `Range`
(original byte start/end, one-based start line/column and exclusive end
line/column) and its literal source slice.

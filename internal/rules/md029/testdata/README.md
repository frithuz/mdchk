# MD029 fixtures

`md029.json` holds the MD029 analysis cases read by `md029_test.go` and
`acceptance_test.go`. Each expected finding gives its six-integer `Range`
(original byte start/end, one-based start line/column and exclusive end
line/column) and its literal source slice.

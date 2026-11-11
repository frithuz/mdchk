# MD031 fixtures

`md031.json` holds the MD031 blank-line cases around fenced code blocks. Each
case gives its `source` with its byte count and SHA-256, the `flavors` it runs
under, the MD031 `options` and the expected `findings`. MD031 has no fixer, so
the data covers analysis only. It is read by `md031_test.go` and
`acceptance_test.go`.

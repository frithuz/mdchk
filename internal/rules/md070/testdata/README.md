# MD070 fixtures

`md070.json` holds the MD070 nested Markdown example cases. Each case gives its
`source` with its byte count and SHA-256, the `flavors` it runs under, the MD070
`options` and the expected `findings`. `invalid_configurations` lists MD070
values that must be rejected and the operations that check them. It is read by
`md070_test.go`, `acceptance_test.go` and `internal/config/md070_test.go`.

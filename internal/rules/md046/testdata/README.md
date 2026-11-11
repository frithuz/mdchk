# MD046 fixtures

`md046.json` holds the MD046 code-block style cases. Each case gives its
`source` with its byte count and SHA-256, the `flavors` it runs under, the MD046
`options` and the expected `findings`. `invalid_configurations` lists MD046
values that must be rejected and the operations that check them. It is read by
`md046_test.go`, `acceptance_test.go` and `internal/config/md046_test.go`.

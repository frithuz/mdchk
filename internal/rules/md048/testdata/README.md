# MD048 fixtures

`md048.json` holds the MD048 fence-style cases. Each case gives its `source`
with its byte count and SHA-256, the `flavors` it runs under, the MD048
`options` and the expected `findings`. `invalid_configurations` lists MD048
values that must be rejected and the operations that check them. It is read by
`md048_test.go`, `acceptance_test.go` and `internal/config/md048_test.go`.

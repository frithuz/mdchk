# MD040 fixtures

`md040.json` holds the MD040 fenced-language cases. Each case gives its `source`
with its byte count and SHA-256, the `flavors` it runs under, the MD040
`options` and the expected `findings`. `invalid_configurations` lists MD040
values that must be rejected and the operations that check them. It is read by
`md040_test.go`, `acceptance_test.go` and `internal/config/md040_test.go`.

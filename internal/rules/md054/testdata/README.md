# MD054 fixtures

`md054.json` holds 60 MD054 cases, read by `md054_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors`, an `options` object and `findings` with original
`bytes`, 1-based `start`/`end` line and Unicode-scalar column pairs and the
exact `message`. Its `invalid_configurations` rows are read by
`internal/config/md054_test.go`. Expected values must never be regenerated from
production output.

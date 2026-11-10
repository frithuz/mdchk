# MD039 fixtures

`md039.json` holds 48 MD039 cases, read by `md039_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors` and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Expected values must never be regenerated from production output.

# MD042 fixtures

`md042.json` holds 55 MD042 cases, read by `md042_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors` and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Expected values must never be regenerated from production output.

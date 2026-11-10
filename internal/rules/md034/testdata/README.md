# MD034 fixtures

`md034.json` holds 53 MD034 cases, read by `md034_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors`, an optional `frontmatter` and `findings` with
original `bytes`, 1-based `start`/`end` line and Unicode-scalar column pairs and
the exact `message`. Expected values must never be regenerated from production
output.

# MD051 fixtures

`md051.json` holds 22 MD051 cases, read by `md051_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors`, an optional `frontmatter` and `findings` with
original `bytes`, 1-based `start`/`end` line and Unicode-scalar column pairs and
the exact `message`. Expected values must never be regenerated from production
output.

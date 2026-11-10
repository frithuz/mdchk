# MD059 fixtures

`md059.json` holds 80 MD059 cases, read by `md059_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors`, an optional `frontmatter`, an `options` object and
`findings` with original `bytes`, 1-based `start`/`end` line and Unicode-scalar
column pairs and the exact `message`. Its `invalid_configurations` rows are read
by `internal/config/md059_test.go`. Expected values must never be regenerated
from production output.

# MD045 fixtures

`md045.json` holds 136 MD045 cases, read by `md045_test.go` and by the compiled
binary in `acceptance_test.go`. Each case has `name`, `source`, `source_bytes`,
`source_sha256`, `flavors`, an optional `frontmatter`, `allow_decorative`,
`problems` in the same shape as findings and `findings` with original `bytes`,
1-based `start`/`end` line and Unicode-scalar column pairs and the exact
`message`. Expected values must never be regenerated from production output.

# MD055 fixtures

`md055.json` holds 38 hand-authored MD055 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md055_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `style`, and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Unexpected pipes cover the one pipe byte; missing pipes are zero-width points.
The cases cover both rule-page examples, every style, `consistent` headers with
both, leading-only, trailing-only and no outer pipes, escaped edge pipes, code
span pipes, empty edge cells, extra and missing body cells, block-quote and
list-item tables, pipe text in fenced and indented code, HTML blocks and
paragraphs without a delimiter row, LF and paired CRLF input, trailing spaces,
non-ASCII cells, multiple tables and a CommonMark case. Its ten
`invalid_configurations` rows are read by the config test. Tests pin the
whole-file digest
(`5439144ca04395f31749913a620651b8c6c82ca67d33b4f729533df89b1dbefa`). Expected
values were written by hand; never regenerate them from production output.

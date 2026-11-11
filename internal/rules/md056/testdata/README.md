# MD056 fixtures

`md056.json` holds 27 hand-authored MD056 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md056_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`,
and `findings` with original `bytes`, 1-based `start`/`end` line and
Unicode-scalar column pairs and the exact `message`. Each finding covers one
trimmed body row. The cases cover all four rule-page examples, short and long
body rows, a body row without pipes, a one-column table, escaped and unescaped
code-span pipes, a GFM header/delimiter mismatch, block-quote and list-item
tables, pipe text in fenced and indented code, HTML blocks and paragraphs
without a delimiter row, LF and paired CRLF input, leading indentation and
trailing spaces, non-ASCII cells, multiple tables with different widths and a
CommonMark case. Its eight `invalid_configurations` rows are read by the config
test. Tests pin the whole-file digest
(`99f108a6a78b9078e645d3c73b2697dec3c812958b3c124680b3b8dc4ff55dae`). Expected
values were written by hand; never regenerate them from production output.

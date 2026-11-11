# MD058 fixtures

`md058.json` holds 40 hand-authored MD058 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md058_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`,
and `findings` with original `bytes`, 1-based `start`/`end` line and
Unicode-scalar column pairs and the exact `message`. Every finding is a
zero-width point: before points start the table's first line after container
prefixes, after points end its last line before the line ending. The cases
cover both rule-page examples, before-side neighbours (heading, fenced code,
thematic break, reference definition), after-side neighbours (heading, fenced
code, block quote, bullet and ordered lists, HTML block, thematic break),
accepted spacing, document, block-quote and list-item edges, a prefix-only `>`
blank, tables in a quote, a list item and a quote inside a list item, indented
tables, trailing spaces, body rows, non-ASCII text, multiple tables, a missing
final ending, pipe text in fenced and indented code, HTML blocks and paragraphs,
raw HTML protection, LF and paired CRLF input, a CommonMark case and a disabled
case. Its six `invalid_configurations` rows are read by the config test. Tests
pin the whole-file digest
(`0329e5fe41d8e42d369e911602fe2085b3e9b002e7cd2beba0edb6ab45a4e095`). Expected
values were written by hand; never regenerate them from production output.

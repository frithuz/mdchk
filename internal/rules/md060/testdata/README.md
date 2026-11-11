# MD060 fixtures

`md060.json` holds 46 hand-authored MD060 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md060_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `style`, and `findings` with original `bytes`, 1-based
`start`/`end` line and Unicode-scalar column pairs and the exact `message`.
Each finding covers one cell field; an empty `||` field is a zero-width point.
The cases cover both rule-page examples under all three styles, empty cells
under each style, rows with and without outer pipes including leading-only and
trailing-only rows, escaped pipes in a cell and in a code span, an unescaped
code-span pipe that splits a cell, multibyte text measured in code points,
unequal body widths, excess and missing cells, tabs in padding, right- and
center-placed text, block-quote and list-item tables, pipe text in fenced and
indented code, HTML blocks and paragraphs without a delimiter row, multiple
tables with independent widths, LF and paired CRLF input and a CommonMark case.
Its ten `invalid_configurations` rows are read by the config test. Tests pin
the whole-file digest
(`4dc4a37c8333402b1a9c5fe63b32e0b8caed5217b56909aa29ff433d08bd5837`). Expected
values were written by hand; never regenerate them from production output.

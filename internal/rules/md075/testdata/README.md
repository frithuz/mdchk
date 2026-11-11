# MD075 fixtures

`md075.json` holds 46 hand-authored MD075 cases, run through production lint
and inspection for their listed flavors and by the compiled binary in
`cmd/mdchk/md075_acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
(and `frontmatter` for the front-matter case), and `findings` with original
`bytes`, 1-based `start`/`end` line and Unicode-scalar column pairs and the
exact `message`. Each finding covers one whole run, from the first line's
trimmed start to the last line's trimmed end. The cases cover all three
rule-page examples, each structure test on its own, runs that fail all three,
an incidental prose pipe, a run broken by a line without a structural pipe,
escaped pipes and backslash-run parity, code-span pipes, pipe text in fenced and
indented code, HTML blocks, front matter, setext headings and after a link
reference definition, a GFM header/delimiter mismatch, a valid table with short
and long body rows, a paragraph line directly before a table, block-quote,
list-item and quote-in-list runs, separate paragraphs, LF and paired CRLF
input, leading indentation and trailing spaces, non-ASCII cells, multiple runs,
`enabled: false`, a CommonMark case and empty task items: checked, unchecked,
with a trailing space, with a space at EOF, with CRLF, in a block quote, in a
nested list, and before and after real rows. Its eight
`invalid_configurations` rows are read by the config test. Tests pin the whole-file digest
(`ab1f0cdd6c5e51fea5e3d7a9ea02bc4417c9d1ca264b2a5e26cbed3afa12b497`). Expected
values were written by hand; never regenerate them from production output.

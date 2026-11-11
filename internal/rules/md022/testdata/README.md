# MD022 fixtures

`md022.json` fixes original byte/Unicode points and before/after messages for both
CommonMark and GFM. Its literal options exercise independent minima, extra blanks,
actual document edges, complete Setext extents, parser-owned container prefixes,
marker-looking prose, protected neighbors, headers and ASCII-only blankness.
The NUL fixture is asserted after JSON decoding. Repeats preserve original source
identity/bytes, produce no fixes and retain explicit uncertainty where required.

`heading_blank_lines_corrections.json` holds the compiled correction cases run
by `TestCompiledHeadingBlankLinesFixWorkflow` in
`heading_blank_lines_fix_acceptance_test.go`. Each case gives the original
`source`, the expected `candidate`, a `config` variant and the `rules` whose
edits apply.

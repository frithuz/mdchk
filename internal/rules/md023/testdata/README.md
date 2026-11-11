# MD023 fixtures

`md023.json` records original UTF-8 `Source` literals and the indented heading
markers MD023 must report. Each expected item gives six coordinates in `Range`
(original byte start/end and one-based start and exclusive end line/column), an
independent literal `Source` slice and whether analysis offers the fix (`Fix`).
An empty `Expected` means no finding.

`heading_indentation_corrections.json` holds the MD023 correction cases run by
`TestHeadingIndentationFixturesReparseIdentically` in
`heading_indentation_fix_test.go` and by
`TestCompiledHeadingIndentationFixWorkflow` in
`heading_indentation_fix_acceptance_test.go`. Each case gives the original
`source`, the expected `candidate`, the `rules` whose edits apply, the
`edit_count` and a `config` variant.

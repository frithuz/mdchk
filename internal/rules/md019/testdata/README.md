# MD019 fixtures

`md019.json` records original UTF-8 `source` literals and the excess spaces
after opening hashes that MD019 must report. Each expected item gives six
coordinates in `range` (original byte start/end and one-based start and
exclusive end line/column) and an independent literal `source` slice. An empty
`expected` means no finding.

`heading_spacing_corrections.json` holds the MD019 and MD021 correction cases
run by `TestHeadingSpacingFixturesReparseIdentically` in
`heading_spacing_fix_test.go` and by `TestCompiledHeadingSpacingFixWorkflow` in
`heading_spacing_fix_acceptance_test.go`. Each case gives the original
`source`, the expected `candidate`, the `rules` whose edits apply, the
`edit_count` and a `config` variant.

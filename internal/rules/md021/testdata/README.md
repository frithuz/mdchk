# MD021 fixtures

`md021.json` records original UTF-8 `Source` literals and the closed heading
delimiter gaps MD021 must report. Each expected item gives six coordinates in
`Range` (original byte start/end and one-based start and exclusive end
line/column) and an independent literal `Source` slice. An empty `Expected`
means no finding. MD021 correction cases share
`internal/rules/md019/testdata/heading_spacing_corrections.json`.

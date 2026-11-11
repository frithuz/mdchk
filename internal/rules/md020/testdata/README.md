# MD020 fixtures

`md020.json` records original UTF-8 `source` literals and the zero-width MD020
points before closing hashes. Each expected item gives six coordinates in
`range` (original byte start/end and one-based start and exclusive end
line/column) and an independent literal `source` slice. An empty `expected`
means no finding. Optional `recognize` selects frontmatter recognition and
`incomplete` marks expected parser uncertainty. The NUL case is asserted after
JSON decoding. Tests run both flavors; findings are report-only.

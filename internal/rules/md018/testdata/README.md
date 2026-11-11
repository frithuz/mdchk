# MD018 fixtures

`md018.json` records original UTF-8 `source` literals and the zero-width MD018
points they must report. Each `expected` item is the point's original byte
offset followed by its one-based line and Unicode-scalar column; a case may give
`gfmexpected` for GFM instead. An empty `expected` means no finding. Optional
`recognize` selects frontmatter recognition and `incomplete` marks expected
parser uncertainty. Tests run both flavors; findings are report-only.

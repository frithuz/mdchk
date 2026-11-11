# MD001 fixtures

`md001.json` records complete authored heading envelopes (including the ending
when present) in source order. Container prefixes precede the heading envelope.
Each expected item gives the same six coordinates in `range` and an independent
literal `source` slice. Empty `expected` means no finding. Tests run both flavors
with explicit recognition/title selections and pin affected metadata coverage.
All MD001 findings remain report-only; no edits or changed documents are emitted.

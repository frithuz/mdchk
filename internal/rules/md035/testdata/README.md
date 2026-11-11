# MD035 fixtures

`md035.json` holds 24 hand-authored MD035 cases, run through production lint
for both flavors and by the compiled binary in `acceptance_test.go`. Each case
has a `name`, the original `source` with its `sha256` and `size_bytes`, the
configured `style`, an optional `flavor` that limits it to one flavor, the
`fixed` document `fix` produces (equal to `source` when nothing is corrected), and
`expected` findings with the six original coordinates in `range` (byte
start/end, one-based start line/Unicode-scalar column and exclusive end
line/column) and the literal marker `source` slice. Empty `expected` means no
finding. Expected values were written by hand; never regenerate them from
production output.

# MD044 fixtures

`md044.json` preserves the 67 independent source-authored preimplementation MD044
literals unchanged, including format-specific URL ownership and exact byte/scalar
ranges. The fixture test verifies the frozen digest and original snapshot hashes.
Additional table/header/raw/control and option boundary cases live in md044_test.go.

`md044_corrections.json` holds the correction cases run by `fix_test.go` and
`fix_acceptance_test.go`. Each case gives the original `Source` and the corrected
`Candidate`, which equals `Source` when every finding stays manual. Optional
`Names` (default `[GitHub]`) and `CodeBlocks` set the MD044 options, `Flavor`
restricts the case to one flavor, and `Config` adds rules: `md047` enables MD047,
`composition` enables MD009, MD047, MD064 and MD085, and `directives` turns on
directive processing, which only the compiled test runs.

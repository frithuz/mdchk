# MD044 fixtures

`md044.json` preserves the 67 independent source-authored preimplementation MD044
literals unchanged, including format-specific URL ownership and exact byte/scalar
ranges. The fixture test verifies the frozen digest and original snapshot hashes.
Additional table/header/raw/control and option boundary cases live in md044_test.go.

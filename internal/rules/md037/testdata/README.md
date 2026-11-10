# MD037 fixtures

`md037.json` holds 42 root source-authored MD037 cases, frozen before the rule
was implemented. Each case has a `name`, the original `source` with its
`source_sha256`, and per-edge excess whitespace as original byte `ranges`, their
literal `range_text` and one-based line/Unicode-scalar column `positions`. The
test pins the whole-file digest and runs every case under CommonMark and GFM.
Expected values must never be regenerated from production output.

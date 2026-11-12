# MD050 fixtures

`md050.json` is the exact independent root MD050 V2 oracle: 68 records, 62
distinct source-byte strings, 49 two-byte Opening ranges and 129 selected
CommonMark/GFM executions. V2 explicitly corrects two original semantic
expectations after preserved first failures, following independent pinned-grammar
review, and adds three source-authored positive controls before their execution.
The original oracle and failure evidence remain preserved in local working notes. Tests
verify the complete file digest, original source digests and byte/scalar geometry,
analysis and immutable source; fix availability is covered by
`md050_corrections.json`. It is independent of production output.

`md050_corrections.json` holds the correction cases run by `fix_test.go` and
`fix_acceptance_test.go`. Each case gives the original `Source` and the corrected
`Candidate`, which equals `Source` when every finding stays manual. Optional
`Style` (default `consistent`) sets the MD050 option, `Flavor` restricts the case
to one flavor, and `Manual` counts the MD050 findings that remain with
`fix_available: false`. `Config` adds rules: `md047` enables MD047, `composition`
enables MD009 and MD047, and `directives` turns on directive processing, which
only the compiled test runs.

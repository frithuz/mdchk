# MD053 fixtures

`md053.json` holds 36 hand-authored MD053 cases, each run under CommonMark and
GFM through both production lint and inspection, and by the compiled binary in
`acceptance_test.go`. Each case has `name`, `source`,
`source_bytes`, `source_sha256`, `flavors`, an `options` object with `enabled`
and an optional `ignored_labels` list, and `findings` with original `bytes`,
1-based `start`/`end` line and Unicode-scalar column pairs, `expected`
(`unused` or `shadowed`) and the exact `message`. The cases cover the rule-page
examples, unused and shadowed definitions, case, whitespace and full-folding
label variants, full, collapsed, shortcut and image uses, block-quote and
nested-list definitions, brackets in code spans, fenced and indented code and
HTML blocks, normalized and empty `ignored_labels` entries, LF and CRLF
documents, non-ASCII and multi-line labels. Tests pin the whole-file digest
(`fe652ecb027e06779a25cb37dcfcf65b1bb63396e7580b25d51aff406f39c58f`). Expected
values were written by hand; never regenerate them from production output.

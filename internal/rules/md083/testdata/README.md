# MD083 fixtures

`md083.json` is the hand-specified MD083 oracle, read by
`TestMD083FrozenAnalysisFixtures` in `fixtures_test.go`, which pins its
whole-file digest and its 57 cases. Each case has an `id`, the `source`, the
configured `sequences`, the `flavors` it runs for, optional `frontmatter`
recognition and `allow_directives`, and `findings` with original `bytes`,
one-based `start`/`end` line and Unicode-scalar column pairs, the matched
`text` and the `message`. Byte ranges were located by substring search; never
regenerate them from production output.

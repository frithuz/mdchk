# MD084 fixtures

`md084.json` is the hand-specified MD084 oracle, read by
`TestMD084FrozenAnalysisFixtures` in `fixtures_test.go`, which pins its
whole-file digest and its 50 cases. Each case has an `id`, the `source`, the
`allowed_codepoints`, the `flavors` it runs for, optional `frontmatter`
recognition and `allow_directives`, and `findings` with original `bytes`,
one-based `start`/`end` line and Unicode-scalar column pairs, the reported
`text` and the `message`. Expected scalars were marked by hand; never
regenerate them from production output.

# MD072 fixtures

`md072.json` holds 34 MD072 cases, run for CommonMark and GFM by
`TestMD072AnalysisFixtures` in `md072_test.go`. Each case gives the original
`Source`, the frontmatter `Formats`, whether recognition is `Disabled`, the
`Recursive` and `Insensitive` (case-insensitive) options, whether the result is
`Incomplete` or the header is `Unparsed`, and the `Expected` findings. Each
finding has original byte `Start`/`End`, the one-based `Line`, the
Unicode-scalar `Column` and exclusive `EndColumn`, and the message.

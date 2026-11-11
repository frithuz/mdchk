# MD071 fixtures

`md071.json` holds 34 MD071 cases, run for CommonMark and GFM by
`TestMD071AnalysisFixtures` in `md071_test.go` and by the compiled binary in
`TestCompiledPlatformMD071ProductionAnalysis` in `acceptance_test.go`. Each
case gives the original `Source` with its `SHA256` and `SizeBytes`, whether
frontmatter is recognized (`Recognize`) and with which `Formats`, whether the
result is `Incomplete` (an unterminated header the parser withholds), and the
`Expected` findings. Each finding has a `Range` of six integers (original byte
start/end, one-based start line/Unicode-scalar column, and exclusive end
line/column), the literal source slice and the message.

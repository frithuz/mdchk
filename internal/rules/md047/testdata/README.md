# MD047 fixtures

`md047.json` records original UTF-8 source and independent expected diagnostics.
`expected: null` means no finding; otherwise the six integers are original byte
start/end, one-based start line/Unicode-scalar column, and exclusive end
line/column. Tabs and combining scalars each count as one column. Endings are
literal escaped JSON bytes; CRLF is indivisible. Findings have rule MD047,
error severity and `fix_available: false`; missing material and surplus suffixes
have distinct messages asserted by the engine fixture test.

The fixtures are analysis-only inputs for downstream correction and transport
acceptance. Correction #145 must add separate edit/preservation expectations;
this data does not assert correction safety. The test also retains the parser's
`internal/parser/testdata/source_consumers/{mixed,references}.json` contexts across LF/CRLF/CR expansion,
with absent, single and surplus terminal sequences.

`md047_corrections.json` holds the compiled saved-apply cases run by
`TestCompiledProductionMD047ApplySavedWorkflow` in `acceptance_test.go` and by
`cmd/mdchk/md047_fix_acceptance_test.go`. Each case gives the original
`source`, the expected saved `candidate`, the exit `status`, whether analysis offers the fix (`eligible`) and
whether it is `deferred`, and a `config` that is empty, `off` (MD047
disabled) or `blocked` (MD047 directives not allowed).

# MD094 fixtures

`md094.json` holds 42 MD094 cases, read by
`internal/engine/md094_fixtures_test.go` through `ValidateEncoding` and by the
compiled binary in `cmd/mdchk/md094_acceptance_test.go`. Each case has `name`,
`source_hex` (the lowercase hex of the exact source bytes, because JSON strings
cannot hold invalid UTF-8) and `expected`, a list with one entry per invalid
maximal subpart in source order. Valid UTF-8 has an empty list. Each entry gives
the zero-based `byte_offset` of the subpart's leading byte, its exclusive
`byte_end`, the one-based `line` and `column` of that byte, and `bytes_hex`, the
subpart's own bytes. A column counts each valid scalar and each earlier invalid
subpart on the line as one. The cases cover an invalid lead byte, a lone
continuation byte, truncated 2-, 3- and 4-byte sequences at EOF, a malformed
continuation, overlong forms, an encoded surrogate, a value above U+10FFFF, bad
bytes on line 1, after multibyte text, after LF, CRLF and lone CR lines and as
the final byte, bad bytes inside fenced code, a block quote, a nested list in a
block quote, YAML front matter and an HTML comment, a directive before the bad
byte, both rule-page examples, the `boundary-*` subpart boundary and position
table, and valid empty, BOM, literal U+FFFD and mixed-ending input. Each case
runs with MD094 attribution enabled and disabled. Tests pin the whole-file digest
(`651f3353d42bbf33707fc1d227ecc7a1aba34375d60cf85fee3e924be07ae810`). Expected
subparts come from Python's strict UTF-8 decoder, which reports the same maximal
subparts, not from mdchk; never regenerate them from production output.

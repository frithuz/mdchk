# MD057 fixtures

`md057.json` records 21 independent issue-102 MD057 cases, each run for both
CommonMark and GFM. Every case gives the original source with its size and
SHA-256, the `check_images` option, an optional `resources.index_file`, an
optional frontmatter format and a native layout of files, directories and
symbolic links created around the selected `docs/self.md`. Expected findings
carry original byte ranges, one-based line/Unicode-scalar start and exclusive
end positions, the literal source slice and the message; expected coverage
carries the same located range for context-unavailable records. Reference
origins report the whole Link or Image envelope; inline origins report the
destination bytes. LF and CRLF twins differ only in offsets. The expectations
were authored from the sources, not from production output, and must never be
regenerated from it.

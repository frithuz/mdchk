# MD088 fixtures

`md088.json` is the root independently source-authored issue-79 MD088 oracle:
93 cases written from raw source geometry before the rule was implemented. Each
case has an `id`, the original `source` with its `source_bytes` and
`source_sha256`, its `flavors`, the authored eligible target occurrences and
every literal `candidates` entry with original `bytes`, one-based line/Unicode
scalar `start`/`end`, `text`, `code_point` and the constant `message`. The test
pins the whole-file digest and checks findings, settings and unchanged
analysis input; fix availability is covered by `md088_corrections.json`.
Expected values must never be regenerated from production output.

`md088_corrections.json` holds the correction cases run by `fix_test.go` and
`fix_acceptance_test.go`. Each case gives the original `Source` and the corrected
`Candidate`, which equals `Source` when every finding stays manual. `Flavor`
restricts the case to one flavor, and `Manual` counts the MD088 findings that
remain with `fix_available: false`. `Config` adds settings: `md047` enables
MD047, `composition` enables MD009 and MD047, `frontmatter` recognizes front
matter, and `directives` turns on directive processing, which only the compiled
test runs.

# MD088 fixtures

`md088.json` is the root independently source-authored issue-79 MD088 oracle:
93 cases written from raw source geometry before the rule was implemented. Each
case has an `id`, the original `source` with its `source_bytes` and
`source_sha256`, its `flavors`, the authored eligible target occurrences and
every literal `candidates` entry with original `bytes`, one-based line/Unicode
scalar `start`/`end`, `text`, `code_point` and the constant `message`. The test
pins the whole-file digest and checks findings, settings and manual, no-edit
results. Expected values must never be regenerated from production output.

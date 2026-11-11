# MD014 fixtures

`md014-v2.json` holds the MD014 shell-prompt cases. Each case gives its `source`
with its byte count and SHA-256, the MD014 and MD087 `configuration`, and the
`expected` findings, suppressed findings, MD087 findings, raw directive
decisions and coverage. `invalid_configurations` lists MD014 values that
configuration loading must reject before any target is read. The `-v2` name
records that this oracle supersedes an earlier one (`supersedes_oracle_sha256`).
It is read by `md014_test.go`, `acceptance_test.go` and
`internal/config/md014_test.go`, which all pin its SHA-256, so an edit must
update them.

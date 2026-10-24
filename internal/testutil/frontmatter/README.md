# Frontmatter operation parity fixtures

`operations.json` is the operation-independent ledger for optional opaque YAML
and TOML recognition. It is loaded as fresh values by the test-only `Load`
helper. The helper depends only on the standard library and performs no parsing,
configuration resolution, rule analysis or filesystem acquisition.

Each of the 15 rows is exercised under both CommonMark and GFM with byte-identical
source and recognition settings. Rows cover omitted/disabled/default settings,
format subsets and unselected formats, opaque directive-looking content, malformed
YAML/TOML, BOM, LF/CRLF/CR, empty and unterminated headers, header-only EOF, and
explicitly deferred JSON recognition. The 65 lexical boundary fixtures remain the
independent exhaustive boundary oracle; this ledger does not duplicate them.

The stable row fields are:

- `id`: unique case name; `source`: original UTF-8 bytes represented by JSON text.
- `frontmatter`: null means the entire configuration mapping is omitted. Otherwise
  `enabled` is explicit; omitted `formats` uses the YAML/TOML defaults.
- `header`: null means ordinary selected-flavor interpretation. Otherwise format,
  envelope/opener/content/optional closer are explicit zero-based half-open byte
  ranges; `body_offset` is the first byte after the opaque envelope. Null closer
  means unterminated, while an empty content range remains present.
- `complete`, `diagnostics`, and `diagnostic_position`: expected reliable analysis
  and MD047 observations. Positions use one-based Unicode code-point coordinates.
- `fix`: whether a safe body correction is expected; `candidate`: exact expected
  bytes after correction, or the unchanged source when no edit is eligible.

Expected source and ranges are fixed data, never obtained by calling the parser
under test. Consumers compare source facts and outcomes, not identical complete
ASTs or operation envelopes: inspect may successfully expose incomplete analysis,
lint reports violations, and fix can eliminate eligible body violations.

Current consumers are the in-memory shared-engine/production-fix test and the CLI
lint/inspect/preview test. Native apply runs both header formats under both flavors
on the verified Darwin arm64+cgo adapter; saved inspection and a second fix pass
prove convergence. Other hosts retain the existing unsupported-write policy.
Production-binary smoke covers all four CommonMark/GFM and YAML/TOML pairs.

Future project and standalone MCP tests can consume the same `source`, settings,
header facts and candidate bytes, using logical names rather than fixture paths.
Those transports are not implemented or tested by this ledger. No metadata values
are decoded or reordered, and JSON recognition remains deferred.

Reproduce current consumers from the repository root:

```sh
GOMAXPROCS=2 go test -p 2 ./internal/engine ./internal/cli -run FrontmatterOperationParity -count=1
GOMAXPROCS=2 go test -p 2 ./cmd/mdchk -run TestCompiledFrontmatterConsumers -count=1
```

Set `MDCHK_RESULT_VALIDATOR_PYTHON` to an interpreter with the pinned result-schema
validator dependencies to validate actual CLI JSON and inspection mutation controls.
The ledger itself contains no platform paths, caller credentials, transport state
or mutable shared source snapshots.

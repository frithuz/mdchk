# Delivered CLI readiness

[MD087 unused-suppression analysis](../rules/html-and-comments/md087.md) is available for CommonMark and GFM,
disabled by default, with no rule-specific options and permanent report-only support.
Each allowed disable target is judged using its actual owned interval and shared
original-source observations. Unknown observations retain incomplete coverage.

[MD086 unmatched-comment analysis](../rules/html-and-comments/md086.md) is available for CommonMark and GFM,
enabled by default with no rule-specific options and permanently report-only.
Actual eligible HTML owners and their tails are analyzed once; matching ends at
the owner/container boundary. Each unmatched opener reports only its original
four bytes. Code, metadata, whole Images, raw payload and opaque HTML tokens stay
protected; ordinary unowned Text is excluded. Unavailable owned named-token
context withholds the complete MD086 inventory with explicit coverage. MD047
remains the sole current correction producer; MCP parity remains downstream.

[MD033 embedded-HTML analysis](../rules/html-and-comments/md033.md) is available for CommonMark and GFM,
disabled by default and permanently report-only. `allowed_tags: []` accepts a
string list and matches whole names with ASCII case folding, without trimming.
Each disallowed complete named tag has its own full original range. Opaque HTML
tokens and actual raw payload stay protected; unavailable owned-tag context
withholds the entire MD033 inventory with explicit coverage. No MD033 edits or
previews are produced; MD047 remains the sole current fixer.


The ordinary executable has a compiled acceptance journey in
[`TestCompiledCLIReadinessJourney`](../../cmd/mdchk/readiness_acceptance_test.go).
It uses one physical disposable project and the same production binary for
`config validate`, `lint`, `inspect`, fix preview and fix application. The fixture
selects CommonMark and MD047 explicitly, disabling the other ordinary catalog
rules. Mandatory UTF-8 validation remains active regardless of MD094 settings.
This reduced configuration tests delivered behavior without implying that every
catalog rule or dialect is implemented.

The journey independently specifies Unicode, tab and CRLF bytes, scalar source
positions, SHA-256 identities and a structured line diff. It checks allowed
suppression, an ancestor prohibition that a child cannot relax, and the deliberate
difference between invocation-directory lint/fix configuration and target-based
inspection. Downward configuration validation finds an unselected malformed
nested definition; lint/fix do not select it. Configuration scans check definitions,
not Markdown documents. Inspection reports observations with exit 0 and its own
projection version, independently of validation's validity decision.

All read-only stages preserve the project's bytes, file and directory inventory,
identities, modes and modification times. The preservation oracle captures each
file and directory through an immediately statted handle, reads file bytes from
that handle, and checks every close before running commands. This freezes native
identity at observation time, including on Windows; directory enumeration and
path metadata can defer identity lookup. Each command checkpoint reports exact
changed paths and fields. Independent filesystem mutation controls verify byte,
size, type, mode, mtime, inventory and file/directory replacement detection without
retries or timestamp tolerances. Access time is deliberately excluded because
reads may update it. An actual missing target produces I/O
exit 13 while retaining an independent readable file's finding or proposed
candidate. Selected text and editor reports agree with literal status and scalar
location expectations. Inspection's binary version equals this executable's
`--version`. When `MDCHK_RESULT_VALIDATOR_PYTHON` names an interpreter with the
checked-in validator's dependencies, every journey JSON result also passes the
[result schema and semantic checks](../contracts/results/validate.py).

Application probes the actual native capability, rather than inferring it from
an operating-system name. On verified Darwin arm64 with cgo and local APFS it
must write the independently expected candidate bytes, publish an observed saved
identity, leave protected/suppressed and unrelated inputs unchanged, and perform
a second application without changes or inode replacement. Other adapters must
explicitly refuse as unsupported with exit 13 and no effects. An arbitrary I/O
failure is not accepted as adapter evidence. The existing three-OS `cli-platform`
CI job runs this journey on macOS, Linux and Windows.

Other boundary proofs remain separate, named acceptance tests:

- [`metadata_acceptance_test.go`](../../cmd/mdchk/metadata_acceptance_test.go)
  checks arguments, help, ordinary build metadata and actual version propagation.
- [`platform_acceptance_test.go`](../../cmd/mdchk/platform_acceptance_test.go)
  and [`platform_interrupt_test.go`](../../cmd/mdchk/platform_interrupt_test.go)
  exercise native paths, streams, output failures, actual process interrupts and
  platform capabilities. Windows permission-bit non-enforcement is explicit;
  cross-vet alone is not evidence of runtime behavior.
- [`fix_apply_darwin_test.go`](../../cmd/mdchk/fix_apply_darwin_test.go) and
  [`fix_boundary_acceptance_darwin_test.go`](../../cmd/mdchk/fix_boundary_acceptance_darwin_test.go)
  retain actual native partial publication, stale/configuration/cancellation
  boundaries. The [VS Code](../../internal/cli/vscode_saved_darwin_test.go) and
  [GitHub](../../internal/cli/github_saved_darwin_test.go) saved-result tests
  distinguish observed saved coordinates from original and clean candidate views,
  including written/refused/unknown outcomes.
- [`integration_examples_test.go`](../../internal/cli/integration_examples_test.go)
  couples published editor examples to matcher/runner grammar fixtures;
  [`cli_documentation.py`](../../tools/cli_documentation.py) inventories and runs
  the documented command examples. These grammar fixtures do not claim to run a
  hosted GitHub Actions runner.

[Exit precedence](../exit-codes.md) remains operation-specific: lint/fix use
14 > 13 > 15 > 10 > 0, while config validation uses 14 > 13 > 12 > 15 > 0.
A completed inspection can return 0 with incomplete observations; a completed
invalid configuration scan can return 12 with `complete: true`. Positive tiny
reporting deadlines may legitimately finish; deterministic already-expired
context tests establish interruption behavior separately.

Readiness is bounded by the [current capabilities](../index.md) and
[platform limitations](../engineering.md#platform-and-process-acceptance). The CommonMark 652-example parser gate
establishes core syntax conformance, not every consumer or rule. MD047 is the
first ordinary production rule and correction; MD001, MD003, MD004, MD005, MD007, MD029, MD030, MD032, MD069, MD076, MD077, MD009, MD010, MD012, MD013, MD027, MD028, MD064, MD085, MD089, MD035, MD037, MD038, MD081, MD088, MD033, MD086, MD087, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026, MD036, MD041, MD043, MD063, MD080, MD082, MD090 and MD093 analysis are also available. MD013, MD026, MD036, MD043, MD063, MD080, MD082, MD085, MD089, MD090, MD081, MD088, MD033, MD087 and MD093 are disabled by default. Unavailable implicit rules
remain coverage gaps (15), explicit unavailable rules reject configuration (12),
and default GFM now uses the verified whole profile. Actual GFM lint/fix/inspect
consumer journeys include source facts and native saved-byte checks. Optional contexts/resources, MCP delivery, broader traversal accounting, the remaining
rules and release/install artifacts are separate work.

[MD081 paragraph-emphasis analysis](../rules/formatting/md081.md) is available for CommonMark and
GFM, disabled by default and permanently report-only, with no edits or previews.

MD010 analysis/configuration is available for CommonMark and GFM. Each original
TAB is a separate manual one-byte finding; code_blocks only admits actual code
block bytes, while CodeSpan/HTML/recognized metadata remain protected. Positive
spaces_per_tab is analysis-neutral. Required acceptance includes full-context
directives and inspection, unchanged source/configuration, and MD047-only
candidate composition; MD010 adds no edit or apply capability.

MD012 analysis/configuration is available for both flavors. Literal fixtures and
compiled CLI checks cover original body ranges, actual protection and container
transitions, active directives/metadata, inherited denials, slicing, strict options
and all reporters. Findings remain manual, including zero-width bodies. MD047
alone supplies candidate corrections; MD012 reanalysis identifies that snapshot.

[MD088 ASCII-punctuation analysis](../rules/formatting/md088.md) is available for CommonMark and GFM,
disabled by default with sole string option `style: ascii`. Invalid styles and
unknown `replacements` reject even when disabled. Findings are manual in Pass 1;
PlannedFix retains future correction scope. No MD088 edits or previews are emitted.

## MD087 CLI examples

With MD087 and MD001 enabled, `<!-- mdchk:disable MD001 -->` followed by a
single clean heading reports `Suppression for MD001 hides no findings.` on the
first full MD001 token. A hidden heading jump instead makes its owner used.

```sh
mdchk lint doc.md --format=json
mdchk inspect doc.md --lines=4-4 --format=json
mdchk fix --dry-run doc.md --format=json
```

Inspection evaluates the complete document before selecting lines and retains
preceding owners as context_only. Its per-target reasons distinguish used,
unused, uncertain, self-excluded and inactive-audit decisions while preserving
blocked-effect provenance. Coverage deficits keep inspection's existing
complete=false/exit policy; actual operational errors remain separate.
A separately authorized MD047 correction recomputes MD087 on candidate and
actual saved snapshots. The audit itself supplies no edit, preview or corrected
content. A second successful write run is stable; a dry run repeats its proposal
from the unchanged original. Native write-acquisition refusal without CGO
retains the existing early refusal contract.

# Delivered CLI readiness

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
first ordinary production rule and correction; MD001, MD003, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026 and MD036 analysis are also available. MD026 and MD036 are disabled by default. Unavailable implicit rules
remain coverage gaps (15), explicit unavailable rules reject configuration (12),
and default GFM now uses the verified whole profile. Actual GFM lint/fix/inspect
consumer journeys include source facts and native saved-byte checks. Optional contexts/resources, MCP delivery, broader traversal accounting, the remaining
rules and release/install artifacts are separate work.

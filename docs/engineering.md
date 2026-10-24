# Markdown linter engineering design

mdchk uses one Markdown analysis and correction engine for both its command-line interface and MCP server. Shared behavior is essential: the same files and configuration must produce the same findings and fixes through either interface.

## Confirmed choices

- Name the command `mdchk`.
- Implement the [89-rule catalog](rules/index.md) with stable numeric IDs and skipping MD002, MD006, MD008, MD015, MD016, and MD017.
- Implement the project in Go.
- Build and maintain an internal Markdown parser instead of using a third-party parser.
- Minimize third-party dependencies across the project.
- Use CommonMark 0.31.2 as the base syntax and support GitHub Flavored Markdown.
- Adopt the [GFM compatibility contract](engineering/gfm-compatibility.md): the shared CommonMark 0.31.2 core plus all five published GFM extensions, with pinned sources, documented differences, source-preserving representations, and explicit conformance gates.
- Make frontmatter recognition independently optional for every supported Markdown format.
- Provide a read-only `lint` subcommand that reports problems.
- Provide a `fix` subcommand that applies safe corrections and reports problems it cannot fix.
- Provide a read-only `inspect` command for one file, with optional line-range focus, exposing processing details without a validation pass or fail result.
- Provide an MCP server exposing lint and fix actions to AI agents.
- Support MCP project mode with inferred configuration and standalone mode with submitted configuration and file contents, including web service operation.
- Provide project configuration for expectations.
- Adopt the [initial configuration contract](configuration.md): `.mdchk.yaml`, schema version 1, canonical MDxxx IDs without aliases, and the pinned YAML dependency in the [configuration design](engineering/configuration.md).
- Preserve catalog defaults independently of implementation availability. Report partial lint/fix analysis as `incomplete` with exit 15; reject explicitly requested unavailable capabilities before analysis.
- Bound explicit configuration selection and resolve ancestor permissions with logical AND. Child true values under a denial are valid but ineffective; prohibited exclusion lists are errors.
- Provide `config validate` to validate every eligible configuration definition in a project, respecting Git ignores and configuration exclusions.
- Support inline directives that enable or disable rules onward through a file, with global and per-rule configuration permissions.
- Inherit ancestor directive prohibitions without permitting descendant relaxation; retain independent child rule settings.
- Use configuration for whole-file exclusion from all rule linting, and allow higher-level configuration to prohibit lower-level exclusion additions.

- Adopt the [resource contract](engineering/resources.md): bounded read-only adapters, partial standalone inventories, shared anchor/index policy, immutable observations, and coordinated fix views.

The [accepted contract index](engineering/contracts.md) records the additional 21 policy groups, including optional contexts, exact rule behavior, fixes, results, limits, MCP and HTTP. Package layout suggestions remain implementation guidance; accepted behavior is normative.

## Go and delivery

Use Go for the engine, CLI, and MCP server. Aim to distribute a single executable for macOS, Linux, and Windows without requiring users to install a language runtime.

Use the Go standard library by default. For each proposed third-party dependency, document the capability it provides, why an internal implementation is insufficient, its transitive dependencies, license, and maintenance cost. Minimize dependencies rather than assuming an absolute zero-dependency requirement. Prefer pure-Go packages when an external dependency is justified.

Choose a supported Go version and record the minimum version in `go.mod`. Pin dependency versions and commit `go.sum`. Benchmark realistic repositories before adding concurrency or caches.

## Markdown support and parsing

Use [CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/) as the shared core for CommonMark and GFM. The approved [GFM compatibility contract](engineering/gfm-compatibility.md) adds tables, task lists, single/double-tilde strikethrough, extended autolinks, and disallowed raw HTML handling from a frozen specification snapshot. It defines a profile with documented differences from the older `0.29-gfm` base. The configuration contract selects `commonmark` or `gfm`, defaulting to `gfm`. See [Markdown formats](markdown-formats.md) for the confirmed scope and current conformance status. Do not imply compatibility with every Markdown renderer.

Implement the parser in `internal/parser` using the Go standard library. Use the Markdown specifications as references for syntax behavior; do not depend on a third-party parser at runtime. Owning the parser also means owning its correctness, compatibility, and performance tests.

### Delivered CommonMark core

`internal/parser` now implements the CommonMark 0.31.2 core. All **652/652**
unchanged specification examples pass both normalized and strict HTML comparisons,
with no exemptions. Direct runs of the pinned upstream test driver also report
`652 passed, 0 failed, 0 errored, 0 skipped` in both modes. This verifies the core
syntax; it does not establish rule coverage or extension conformance. The HTML
adapter in `internal/conformance` is test infrastructure, not a production renderer.

Acquire an immutable snapshot with `parser.NewSource(content)`, then call
`parser.Parse(ctx, source)` with a non-nil context. `NewSource` copies input and
`Source.Bytes` returns a copy. Parse has no flavor or frontmatter option: it
recognizes core syntax with extensions/frontmatter disabled. Configuration-driven
selection and bounded read-only acquisition are connected to CLI lint, including
the shared mandatory UTF-8 gate, parser and registered-rule engine. GFM uses the
same lifecycle through `ParseGFMWithBudget`; the legacy Parse APIs stay core-only.
`ParseWithOptions` additionally accepts immutable frontmatter recognition settings
and independent complete GFM selection. Recognized headers are opaque typed
document records, with body parsing against the same original snapshot; legacy
entry points remain unchanged. Shared CLI consumers preserve recognition settings,
protect header envelopes and project source-backed records. Semantic metadata,
other production rules, MCP and rendering remain separate.

The first phase identifies paragraphs, ATX/Setext headings, fenced/indented code,
thematic breaks, all seven HTML-block forms and nested quotes/lists, and completes
the document-wide reference-definition inventory. Only then does the inline phase
process paragraph/heading payloads: escapes/entities, code spans, soft/hard breaks,
inline HTML, angle autolinks, emphasis/strong, and inline/full/collapsed/shortcut
links and images. Code and raw HTML payloads remain opaque. References retain
source order and duplicates; lookup selects the first normalized definition.

Consumers must inspect `Status`, `AST`, `Phases`, reference availability and
`Uncertainty`. Neither nil `Failure` nor empty children/references proves a
complete parse. Interrupted work yields explicit partial availability; unfinished
inline work never replaces the completed block tree. Rules must skip unreliable
syntax and fixes must not edit uncertain regions. The
[model contract](../internal/parser/model.md) defines typed outcomes, immutable
accessors, properties and reference ownership in detail.

Spans are snapshot-owned, half-open original byte ranges. Node envelopes, syntax
markers, logical `Content` and semantic `Value` are distinct: container-prefix gaps
and trimmed bytes stay in Source. Explicit Exact, Escape, Entity, Normalized and
PartialTab provenance connects values to original bytes. Synthetic contributions
may be spanless or carry a source anchor, such as a zero-width EOF span for an
inserted code-block LF. A partially consumed tab can back overlapping contributions,
so provenance is not a set of disjoint edits. Semantic offsets are never edit
offsets. Reference-use destinations/titles are spanless Synthetic projections; authoritative
ranges belong to their selected definition. Original Unicode, NUL, LF/CRLF/CR and
missing final newlines are retained. Diagnostic positions are one-based Unicode
scalar columns (tabs count as one); structural indentation uses four-column tab
stops. Apply targeted source edits rather than rendering Markdown back from AST.

`Parse` starts a private default operation. For shared document/reparse accounting,
use `DefaultLimits()`, `NewOperationBudget(limits)` and
`ParseWithBudget(ctx, source, operation)`. Shared operations serialize parses and
adapter `ChargeWork` calls; independent operations can run concurrently. The
[parser accounting specification](engineering/parser-limits.md) defines finite
byte/depth/construction/work/time limits, validated overrides, cumulative budgets
and explicit limit-exceeded outcomes. Exhaustion reports `Incomplete`/`Partial`,
nil `Failure`, `Interruption=LimitExceededReason` and a typed `Limit`, with
whole-source uncertainty. Source acquisition must enforce admission before copying
oversized input. Parse admits document size before scanning;
UTF-8 validation is itself work-budgeted. Once malformed encoding is established,
its typed original-byte failure precedes cancellation/deadline. Prior exhaustion
or insufficient validation work can stop before encoding is established. MD094
controls downstream attribution, never whether admitted bytes are validated.

GFM uses CommonMark 0.31.2 and all five pinned extensions: 2,117 approved
comparison units pass, including every published case. Default and explicit GFM
selection share the real lint, fix and inspection pipeline, including source
facts, directives, protected regions and candidate reparsing. MD047 is the first
production rule and correction; other named rules remain unavailable. See the [parser verification record](engineering/gfm-verification.md).

### Reproduce the core delivery

From the checkout root, the following commands use retained pinned inputs without
local external paths or runtime parser dependencies (Go 1.27.2+, Python 3.9+):

```sh
go build ./...
go test ./...
go vet ./...
go test -race ./...
python3 -B tools/commonmark_corpus.py
python3 -B tools/commonmark_corpus.py --check
python3 -B tools/unicode_casefold.py --check
python3 -B tools/html_entities.py --check
python3 -B -m unittest discover -s tools -p 'test_commonmark*.py' -v
python3 -B tools/commonmark_conformance.py --require-complete
python3 -B tools/commonmark_conformance.py --strict --require-complete
```

Corpus regeneration must leave tracked files unchanged. Both unfiltered harness
runs must report 652 pass, zero fail/error/pending and full conformance. The Python
suite also verifies direct upstream execution in both comparison modes, requiring
exactly 652 passed and zero failed/errored/skipped. See the
[contributor validation guide](contributing.md#regenerate-and-verify-the-commonmark-corpus)
for individual commands, direct-driver reproduction and generated-table provenance,
and [hardening checks](contributing.md#parser-resource-race-and-fuzz-verification)
for fuzz and benchmark commands. Race checks require a supported host.

The [corpus provenance](../internal/parser/testdata/commonmark/0.31.2/README.md)
pins `commonmark/commonmark-spec` commit
`9103e341a973013013bb1a80e13567007c5cef6f` and `spec.txt` SHA-256
`257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80`.
Independent AST/source assertions complement HTML comparisons. The
[performance record](engineering/parser-performance.md) separates successful parses
from explicit exhaustion and records measured accounting costs without claiming
universal linear time or a machine-independent throughput target.

For GFM, retain original table cell counts before semantic padding or truncation, task-marker spans and spelling, strikethrough delimiter spans, and bare versus angle-delimited autolink origins. Keep filtered raw HTML protected and preserve its source. The [extension source contract](engineering/gfm-compatibility.md#extension-source-contract) defines the information shared by lint, fix, inspect, and MCP.

Make frontmatter recognition independently optional for every supported flavor. When enabled, treat recognized headers as opaque blocks, protected from Markdown style rules and directive interpretation. When disabled, parse the source using ordinary flavor rules. The [frontmatter contract](engineering/frontmatter.md) fixes disabled-by-default YAML/TOML recognition, delimiters, metadata semantics and malformed-header protection. Exclude code blocks, inline code, and raw HTML from generic style rules unless a rule explicitly targets those constructs. Preserve LF, CRLF and CR bytes using the [fix contract](engineering/fixes.md). For the delivered core, UTF-8 admission and budget precedence follow the parser contract above; MD094 attribution belongs to the future engine.

## Shared engine and package boundaries

The engine accepts a context, documents with logical paths and source contents, resolved configuration, operation options, and a read-only resource adapter. Resource outcomes distinguish existence, content availability, authoritative absence, blocked access, and operational failure. Both project and standalone adapters implement the same logical namespace and never register writes through resource lookup. Filesystem adapters load and save project files; standalone MCP supplies documents in memory and returns corrected content. The engine should return findings, proposed or applied changes, and operational errors without printing output or choosing process exit codes.

Suggested boundaries:

| Package | Responsibility |
| --- | --- |
| `cmd/mdchk` | Process entry point |
| `internal/cli` | Arguments, command dispatch, output and exit codes |
| `internal/config` | Discovery, validation and effective settings |
| `internal/files` | File selection and bounded filesystem access |
| `internal/parser` | Markdown structure, source spans and syntax context |
| `internal/engine` | Parser coordination, rule execution and result aggregation |
| `internal/rules` | Rule definitions and implementations |
| `internal/fix` | Edit validation, application and writes |
| `internal/report` | Text, JSON, VS Code problem matcher output and GitHub annotations |
| `internal/mcp` | MCP tool schemas and engine adaptation |

Keep packages internal initially. A public Go API can be designed later if there is demand.

## Rules and diagnostics

Use the [ruleset to implement](rules/index.md) as the canonical scope. It contains 89 planned rules grouped by area, with one page per rule. Preserve the MDxxx IDs. Each rule page defines its internal behavior, options, default enablement, and correction policy.

Give each rule a summary, default setting, option schema, and documented fix policy. Use the defaults in the rule pages. Keep known-rule metadata separate from executable registration so unavailable checks retain their identity, defaults, and option schemas. Use canonical MDxxx IDs throughout configuration, directives, and reports.

A diagnostic should carry the relative file path, rule ID, severity, message, source range, and fix availability. Internal spans should use byte offsets; public positions should use one-based lines and Unicode code-point columns, with tabs counted as one column. Document exclusive range ends. Sort results by path, source position, and rule ID for repeatable output.

Follow the catalog's implementation contract for rule specifications, fixtures, format activation, and resource boundaries. Keep heading hierarchy and missing code language fixes report-only when author intent cannot be inferred safely. MkDocs, Quarto and slide contexts follow the [integration contract](engineering/integrations.md); metadata and encoding checks follow the [frontmatter](engineering/frontmatter.md) and [special rule contracts](engineering/rule-behavior.md).

Start with one violation severity and no warning threshold controls. Add severities only when concrete rules justify them.

## Safe correction contract

`lint` never modifies source or configuration. Only fix apply may write.
The [correction contract](engineering/fixes.md) defines atomic multi-edit groups,
all-conflicting-group deferral, candidate reparse/preservation, ten changing rounds
plus a read-only convergence check, and rollback of nonconvergent candidate components.
Stable output must be idempotent; remaining manual findings need not be clean.

Build coordinated candidate views before any writes. Preview and standalone fixes
use candidates; project results use confirmed saved bytes and invalidate stale or
uncertain dependencies. Resource-only snapshots remain read-only. Preserve exact
line endings, unrelated bytes, permissions and required metadata.

Use verified per-file native atomic replacement and cooperating-writer locks.
Refuse unsupported write guarantees. Noncooperating writers can still be overwritten
if they change a file after the final check; no portable compare-and-swap or
whole-project transaction is promised. A late symlink entry can be replaced without
following it; do not claim every late substitution is refused. Report successful,
refused, failed and uncertain writes truthfully, even after interruption.

## Configuration and file selection

Use `.mdchk.yaml` with a required `version: 1` when a file is present. Support rule enablement, rule options, and excluded paths. Reject unknown keys, rule IDs, invalid options, and unsupported schema versions before processing fixes.

YAML is approved. Use `github.com/goccy/go-yaml` v1.19.2 for syntax parsing and validate source nodes against the explicit configuration schema before decoding effective settings. The [configuration design](engineering/configuration.md#yaml-dependency-and-decoding) records the dependency rationale, license, alternatives, and coercion pitfalls. Share semantic validation with standalone MCP JSON objects.

For CLI lint and fix invocations, use the working directory as the invocation root. No lint/fix `--root` flag is defined. Search upward for the nearest `.mdchk.yaml`, stopping after checking the nearest enclosing repository root, or the filesystem root otherwise. Recognize `.git` directories and worktree `.git` files. Resolve relative `--config` from the working directory and reject configs outside the established boundary, in another repository scope, or reached through symlinks. Inspect starts discovery at the canonical target's parent. Do not merge ancestor rule settings or apply nested overrides. MCP project discovery stays within its startup root; standalone MCP uses only submitted configuration.

Use precedence: built-in defaults, discovered or explicit configuration, then supported CLI overrides. Interpret exclusion patterns relative to the configuration directory, or the invocation root if no configuration exists. Implement the approved [pattern grammar](file-selection.md#exclusion-pattern-grammar): per-segment Go `path.Match` semantics plus whole-segment `**`, with case-sensitive matching. Keep config expectations separate from permissions: configuration cannot broaden the MCP server's filesystem scope or turn lint into fix.

Resolve inherited directive permissions as logical AND within the same ancestor discovery boundaries, preserving restriction provenance without merging rule settings. See [inherited directive permissions](engineering/configuration.md#inherited-directive-permissions).

Resolve higher-level `allow_nested_excludes` policies separately from rule settings, as defined in the [configuration design](engineering/configuration.md#higher-level-exclusion-policy). A selected lower-level configuration cannot bypass an ancestor's restriction, including through explicit config selection. Keep ancestor policy reads within the operation's discovery boundary.

With no paths, recursively select `.md` and `.markdown` files (also `.qmd` when Quarto is enabled) beneath the invocation root using ASCII-case-insensitive extension matching. Include hidden directories except `.git`; skip symlinks. Explicit regular files are eligible regardless of extension, but still honor exclusions, including matching ancestor directories. Explicit unreadable or missing paths are errors. Report explicitly excluded paths as skipped. Selecting no files is a usage error.

The [configuration validation design](engineering/configuration.md) defines the separate project-wide scan used by `config validate`, including Git ignore support and scoped exclusions from nested definitions. Nested validation does not introduce overrides for Markdown operations. Defer Git ignore support for lint and fix, nested configuration overrides, stdin input, and global mdchk user configuration until their behavior is designed. The [configuration guide](configuration.md) defines user-facing settings and examples.

The [inspection design](engineering/inspect.md) defines processing instrumentation and directive attribution. The [directive processing design](engineering/directives.md) defines shared comment recognition, rule-state transitions, configuration permissions, and suppression for inspect, lint, fix, and MCP.

## CLI execution status

`cmd/mdchk` delegates to `internal/cli.RunContext`, with its interrupt context,
supplied arguments and output streams. `Run` remains a background-context
convenience for other callers. Only main exits the process. Help and version
perform no project I/O.
After syntax checks, lint calls the shared `config.Resolve` with its working
directory and optional explicit config, before accessing targets. Production
assembly uses the complete catalog with the MD047 read-only analysis callback
and asserts CommonMark, GFM and shared directive-processing capabilities. No
configuration selects built-in GFM through the same whole-profile parser. Configuration and read
failures return 12; output failures return 13.

Preparation retains the full owned selection, resolution and admitted execution
plan.
Its exclusion seam binds the selected compiled matcher to the configuration
directory (or invocation directory without a file) and calls `Patterns.Excludes`
for native explicit paths, including ancestor directories. Directory identity
comes from the reusable [filesystem selection provider](engineering/file-selection.md),
which implements portable traversal and structured skips/failures. Execution
acquires immutable original-byte snapshots and passes them to the admitted plan
with the same context and operation budget. The engine owns mandatory UTF-8
validation, parsing and available registered checks. MD047 checks original physical terminal
endings and assesses context-specific correction eligibility without generating
edits. Its production dry-run provider supplies independently checked minimal
terminal corrections through the common fix pipeline; filesystem application
is available on the verified Darwin arm64+cgo/local APFS adapter. Other missing enabled defaults produce genuine coverage deficits and return
15. Valid UTF-8 with ordinary rules disabled can return clean 0; available checks
can produce violations and retained partial findings. Lint emits text by default
or one schema-v1 JSON object on stdout, with execution logs on stderr. Ordinary
usage/configuration errors preserve a valid selected JSON presentation. Reporters
retain canonical outcomes, snapshot identities, coverage, errors and partial
observations; encoded-byte limits reduce whole records with explicit omissions.
The reporting deadline is independent of expired analysis. The VS Code adapter
implements bounded diagnostic stdout and operational stderr, including actual
candidate/saved results. Lint, fix and config validate support all four formats;
inspect remains text/JSON only. The GitHub
renderer is implemented with explicit trusted repository/CWD/path-flavor context,
repository-relative annotations or conservative unlocated findings, independent
workflow percent encoding and command-safe human stderr. It reuses the bounded
two-stream count, omission and emergency reserve paths.

## CLI and result schema

Provide `lint [paths...]`, `fix [paths...]`, `inspect [options] <file>`, the `config` command group, and MCP startup in project or standalone mode. Both lint and fix should support `--config PATH` and `--format text|json|vscode|github`, defaulting to text; fix should additionally support `--dry-run`. The [result formats guide](output-formats.md) defines their user-facing behavior. See the [MCP design](engineering/mcp.md) for server flags and transports. Keep help and version reporting available without a project.

The [documentation index](index.md) defines global options and links to each command reference. Support global `--help` and `--version` without loading configuration or processing documents. Lint and fix accept options before or after path arguments and `--` to terminate option parsing. Keep command-specific flags scoped to their commands; reject unsupported flags as usage errors.

The [exit code guide](exit-codes.md) defines the user-facing result contract, including validation failures, partial runs, dry runs, and error precedence. Keep CLI behavior and integration tests aligned with that guide.

For lint and fix, validate arguments, then configuration, before processing files. Config validate checks definitions during traversal and aggregates errors, following its command guide. Return handled errors to the CLI entry point for status mapping; avoid `log.Fatal` and default argument-parser exits. Unrecovered panics retain the runtime's exit behavior. Test exit codes against the compiled executable, since [`go run`](https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program) does not preserve the compiled program's exit status.

Version the JSON envelope independently of the executable. Include the operation, schema version, status, completeness, checked and skipped files, changes, remaining diagnostics, coverage reasons, and operational errors, using the [result formats guide](output-formats.md). Missing default-enabled implementations, uncertain parsing, and missing required context produce `incomplete` with exit 15 unless an operational error takes precedence. Preserve available findings. Explicitly requested unavailable capabilities fail configuration validation before analysis. Give operational errors stable categories (`usage`, `configuration`, `io`, or `internal`). For dry runs, label changes as proposed. Send results to stdout and execution logs to stderr; preserve structured error output when the selected format is valid.

Implement all reporters as adapters over the shared result model. Emit deterministic, undecorated diagnostic lines and escape embedded line breaks so each finding occupies one physical line. CLI report paths are relative to the working directory; GitHub annotations use repository-relative paths and line locations. Escape GitHub workflow command data and properties according to the runner's encoding rules, including percent signs, line breaks, commas, and colons where required. Do not echo raw Markdown into workflow output. Test reporter output for clean checks, partial failures, Unicode, Windows paths, hostile message contents, and previews; verify the VS Code problem matcher against representative diagnostic lines.

## MCP server

The [MCP engineering design](engineering/mcp.md) defines project and standalone modes, stdio and HTTP transports, input schemas, configuration resolution, filesystem boundaries, and result handling. The [MCP user guide](commands/mcp.md) explains how callers use both modes.

Both modes use the shared engine and correction policy. Project mode reads and writes within a configured root. Standalone mode processes submitted documents in memory and returns corrected contents without project filesystem access.

## Verification and release criteria

Use fixture-based rule tests with valid documents, violations, expected spans, configuration variants, and expected corrections. Include hard breaks, nested lists, fenced and indented code, front matter, HTML, Unicode, and both newline conventions.

Maintain parser fixtures derived from specification examples, respecting their license and attribution requirements. Assert parsed structure and source spans as well as resulting diagnostics. Cover delimiter precedence, container nesting, continuation lines, malformed input, and extension interactions. Include adversarial inputs to detect excessive time or memory use. Separate parser coverage from rule coverage so a passing rule test does not imply dialect conformance.

The [GFM corpus contract](engineering/gfm-compatibility.md#corpus-ingestion-and-expectations) requires offline, reproducible ingestion of pinned inputs, all 652 CommonMark 0.31.2 cases, all 28 published GFM extension cases, and explicit accounting for all 677 published GFM cases. Preserve upstream expectations separately from reviewed profile adaptations. Neither silently skipped examples nor agreement with cmark-gfm establishes conformance. An HTML adapter may be used in tests without adding a production renderer or parser dependency.


Test engine properties: lint leaves bytes unchanged; fixing preserves protected content; fixes are idempotent; conflicting edits are reported; invalid configuration produces no writes; changed source snapshots are rejected. Add integration tests for CLI exit codes, JSON contracts, MCP parity, root boundaries, cancellation, and partial write failures.

Use Go fuzzing for the internal parser and edit application. Run `go test ./...`, `go vet ./...`, and race checks where supported. Benchmark a documented sample repository before setting a numeric performance target.

Before the first release, publish rule documentation, install instructions, platform artifacts, checksums, license, configuration examples, and versioning policy. Treat changes to default rules, fix behavior, configuration, and structured output as compatibility decisions.

## Remaining work and decisions

1. Implement and verify the accepted contracts, syntax profiles and resource adapters.
2. Select source-preserving metadata adapters, extend parser accounting and the AST property catalog to those adapters, and complete Git-ignore source discovery in their owning issues.
3. Verify native filesystem write adapters and independent MCP/HTTP interoperability and deployment tests before claiming support.
4. Choose a supported Go toolchain and maintain exact dependency checksums and licenses for the approved SDK and adapters.
5. Choose a project license, release platforms and installation channels in release planning; these are outside the audited implementation policy decisions.


## CLI readiness acceptance

The [delivered CLI readiness record](engineering/cli-readiness.md) maps the
ordinary compiled five-operation journey and supporting boundary tests to their
current capability limits.

## Platform and process acceptance

The `cli-platform` CI jobs execute actual compiled commands on pinned macOS,
Linux and Windows runner images. They print GOOS, GOARCH and CGO_ENABLED and
exercise native separators, spaces, Unicode, dash-prefixed paths, alias and
ordering stability, raw CRLF bytes, public report paths, real output descriptors,
filesystem errors and read-only previews. Editor and documented integration
fixtures run on each host too. Cross-vet checks compilation separately; it is not
runtime evidence. These jobs build for testing and do not publish executables.

Symlink tests first create a real link. Permission tests probe whether the same
user actually receives a read denial; mode changes alone do not establish that
capability on Windows or privileged hosts. Unavailable capabilities have explicit
native-error or non-enforcement skips, while independent missing-target and
non-directory error tests still run. POSIX backslashes remain filename characters;
Windows report paths use slash separators. Source bytes are never normalized.

The executable handles `os.Interrupt`: SIGINT on Unix and console Control-C or
Control-Break events on Windows. SIGTERM and forced termination are outside this
handled-request promise and may prevent any report. The process fixture overlays
only startup synchronization after signal-handler registration, then sends a real
SIGINT or a Windows CTRL_BREAK_EVENT to the child's isolated process group.
It checks the normal incomplete/cancelled JSON report and exit 15. A Windows
runner without working console-event delivery records the actual event-generation
failure as a capability skip; ordinary compiled deadline and domain cancellation
checks remain separate evidence. A harness timeout kills only its owned child
and never counts as successful graceful cancellation.

Inherited read-only output descriptors exercise actual stdout and stderr delivery
failures; Unix closed-pipe tests separately check SIGPIPE handling. Injected short
writers test the distinct short-write boundary. Delivery failures return 13 unless
an internal failure requires 14. The other stream is still attempted, and ordinary
text/JSON reports are not copied to stderr as a fallback.

Safe publication remains limited to the verified Darwin arm64+cgo/local APFS
adapter. Other builds refuse apply with 13 and unchanged files, while dry-run
works. A macOS runner's name alone does not prove those adapter prerequisites;
architecture, cgo and filesystem capabilities must be verified independently.

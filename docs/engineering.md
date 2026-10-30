# Markdown linter engineering design

[MD033 embedded-HTML analysis](rules/html-and-comments/md033.md) is available for CommonMark and GFM,
disabled by default and permanently report-only. `allowed_tags: []` accepts a
string list and matches whole names with ASCII case folding, without trimming.
Each disallowed complete named tag has its own full original range. Opaque HTML
tokens and actual raw payload stay protected; unavailable owned-tag context
withholds the entire MD033 inventory with explicit coverage. No MD033 edits or
previews are produced; MD047 remains the sole current fixer.


mdchk uses one Markdown analysis and correction engine for both its command-line interface and MCP server. Shared behavior is essential: the same files and configuration must produce the same findings and fixes through either interface.

[MD088 ASCII-punctuation analysis](rules/formatting/md088.md) is available for CommonMark and GFM,
disabled by default with sole string option `style: ascii`. Invalid styles and
unknown `replacements` reject even when disabled. Findings are manual in Pass 1;
PlannedFix retains future correction scope. No MD088 edits or previews are emitted.

## Confirmed choices

- Name the command `mdchk`.
- Implement the [87-rule catalog](rules/index.md) with stable numeric IDs and skipping MD002, MD006, MD008, MD015, MD016, MD017, MD078, and MD079.
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
protect header envelopes and project source-backed records. On-demand internal
YAML/TOML decoding preserves native values and original source spans; production
metadata integration,
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
production correction; MD001 heading-increment, MD003 heading-style, MD004 bullet-marker
analysis, MD005 sibling-list alignment analysis, MD007 bullet nesting indentation
analysis, MD029 numbered-list prefix analysis, MD030 list-marker separator analysis,
MD032 list-boundary blank-line analysis, MD069 repeated-list-marker analysis, MD076 list-item spacing analysis, MD077 list-continuation indentation analysis, MD009 trailing-whitespace analysis, MD010 tab-character analysis, MD012 blank-body-run analysis, MD013 maximum-line-width analysis, MD027 blockquote-separator analysis, MD028 blockquote-gap analysis, MD064 repeated-prose-space analysis, MD085 paragraph-continuation analysis, MD089 CJK/Latin-spacing analysis, MD035 horizontal-rule-style analysis, MD037 emphasis-whitespace analysis, MD038 inline-code-whitespace analysis, MD065 horizontal-rule blank-line analysis, MD081 paragraph-emphasis analysis, MD088 ASCII-punctuation analysis, MD033 embedded-HTML analysis, MD018 missing opening-hash spacing, MD019 extra
opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading
delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation
analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026
heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041
opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization
analysis, MD080 heading-anchor collision analysis, MD082 empty-section analysis, MD090
thematic-break-before-heading analysis and MD093 inline-formatting-in-headings analysis
are also available. MD065 is enabled by default for CommonMark and GFM; its
findings are manual and corrections are deferred. MD013, MD026, MD036, MD043, MD063, MD080, MD082, MD085, MD089, MD090, MD081, MD088, MD033 and MD093 are
available but disabled by default. Other named rules remain unavailable. See the [parser
verification record](engineering/gfm-verification.md).

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

The original-operation heading projection retains existing semantic Visible,
raw-content presence and nonempty-ATX OpeningSeparator fields. Closed ATX also
exposes ClosedOpeningGap and ClosedClosingGap as owned original source spans;
raw-empty headings expose only the shared ClosedOpeningGap. These derive from
actual opening/closing markers and first/last raw Content source contributions,
including split NUL contributions. They confer no correction authority. See the
[MD021 policy](rules/headings/md021.md) and [accounting](engineering/parser-limits.md).

MD022 analysis uses a separate completed-parser projection of whole physical
heading endpoints and neighboring blank body runs, leaving existing HeadingFact
consumers unchanged. Actual per-line container markers supply prefix ownership;
finite source/line passes share original controls and never loop over configured
minima. See [MD022 policy](rules/headings/md022.md) and
[accounting](engineering/parser-limits.md). Automatic blank-line correction is
future work.


MD023 retains a private positive leaf-indentation aggregate while the block parser
still owns each line's actual post-matched-container cursor, including partial
tabs, lazy prefixes and deep Setext continuations. Leading reference extraction
recomputes the aggregate from surviving retained paragraph parts. Only actual
heading dispatch retains one original opening/underline marker; paragraph and
table ownership publishes none. Existing HeadingFact and MD022 facts are unchanged.

Private aggregate updates use the existing metered line visits; retained markers
use metered append reservations. The narrow IndentedHeadings projection requires
the completed original parse, source and operation. It charges each copied marker,
record and amortized slice growth before publication and withholds the inventory
on failure. RuleContext joins caller and originating controls; the callback
charges each finding and retains reliable prior findings with a later error.
There is no new parse, source, allowance, whole-document rescan or public-tree copy.
This is finite application-work accounting, not a process memory or throughput
guarantee. No correction provider or write authority is added. See [MD023 policy](rules/headings/md023.md).

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

Use the [ruleset to implement](rules/index.md) as the canonical scope. It contains 87 planned rules grouped by area, with one page per rule. Preserve the MDxxx IDs. Each rule page defines its internal behavior, options, default enablement, and correction policy.

Give each rule a summary, default setting, option schema, and documented fix policy. Use the defaults in the rule pages. Keep known-rule metadata separate from executable registration so unavailable checks retain their identity, defaults, and option schemas. Use canonical MDxxx IDs throughout configuration, directives, and reports.

A diagnostic should carry the relative file path, rule ID, severity, message, source range, and fix availability. Internal spans should use byte offsets; public positions should use one-based lines and Unicode code-point columns, with tabs counted as one column. Document exclusive range ends. Sort results by path, source position, and rule ID for repeatable output.

Follow the catalog's implementation contract for rule specifications, fixtures, format activation, and resource boundaries. Keep heading hierarchy and missing code language fixes report-only when author intent cannot be inferred safely. MkDocs and slide contexts follow the [integration contract](engineering/integrations.md); metadata and encoding checks follow the [frontmatter](engineering/frontmatter.md) and [special rule contracts](engineering/rule-behavior.md).

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

With no paths, recursively select `.md` and `.markdown` files beneath the invocation root using ASCII-case-insensitive extension matching. Include hidden directories except `.git`; skip symlinks. Explicit regular files are eligible regardless of extension, but still honor exclusions, including matching ancestor directories. Explicit unreadable or missing paths are errors. Report explicitly excluded paths as skipped. Selecting no files is a usage error.

The [configuration validation design](engineering/configuration.md) defines the separate project-wide scan used by `config validate`, including Git ignore support and scoped exclusions from nested definitions. Nested validation does not introduce overrides for Markdown operations. Defer Git ignore support for lint and fix, nested configuration overrides, stdin input, and global mdchk user configuration until their behavior is designed. The [configuration guide](configuration.md) defines user-facing settings and examples.

The [inspection design](engineering/inspect.md) defines processing instrumentation and directive attribution. The [directive processing design](engineering/directives.md) defines shared comment recognition, rule-state transitions, configuration permissions, and suppression for inspect, lint, fix, and MCP.

## CLI execution status

`cmd/mdchk` delegates to `internal/cli.RunContext`, with its interrupt context,
supplied arguments and output streams. `Run` remains a background-context
convenience for other callers. Only main exits the process. Help and version
perform no project I/O.
After syntax checks, lint calls the shared `config.Resolve` with its working
directory and optional explicit config, before accessing targets. Production
assembly uses the complete catalog with real MD001, MD003, MD004, MD005, MD007, MD029, MD030, MD032, MD069, MD076, MD077, MD009, MD010, MD012, MD013, MD027, MD028, MD064, MD085, MD089, MD035, MD037, MD038, MD081, MD088, MD033, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026, MD036, MD041, MD043, MD063, MD080, MD082, MD090, MD093 and MD047 analysis callbacks.

[MD081 paragraph-emphasis analysis](rules/formatting/md081.md) is available for CommonMark and
GFM, disabled by default and permanently report-only, with no edits or previews.

It admits CommonMark, GFM, shared directive processing and optional YAML/TOML
header recognition. Metadata interpretation supports MD001, MD025 and MD041 opt-in
`frontmatter_title` consumer under the original engine-owned document and budget;
MD072 remains independently unavailable. MD001, MD025 and MD041 are report-only;
their title access grants no header findings or corrections. Broader metadata
inspection, consumer and correction integration remain separate deliveries.
Omitted flavor configuration selects built-in GFM through the same whole-profile
parser. Configuration and read
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

MD024 adds a raw Label span to HeadingFact without changing existing fields.
First/last original Content source bounds preserve NUL mappings and multiline
Setext gaps; raw-empty ATX uses opening.End. The original-operation HeadingKey
bridge collapses parser.IsUnicodeWhitespace and uses pinned Unicode17 FoldCase
without decoding again. All scans, builders and fold expansion reserve work and
construction before allocation, with long-loop checkpoints. The callback reserves
map capacity for the complete finite heading inventory before construction, so
ordinary insertions do not grow the map or rehash prior variable-length keys;
whole-key lookup/comparison, key retention, fixed outline state and output growth
are separately charged. One linear heading pass uses actual occurrence IDs for
nearest-shallower parents, preserving all original context across suppression.
Projection failure withholds facts; a later callback error retains prior reliable
findings and explicit incomplete coverage. No fresh source, parse, allowance or
public-tree clone is created. This is finite application-work accounting, not a
process memory or throughput guarantee. See [MD024 policy](rules/headings/md024.md).

MD025 counts matching-level headings with one saturated baseline flag and no
text comparison or container partition. Opt-in metadata is decoded once through
the original title provider; unknown semantics withhold only the first matching
body comparison and retain reliable later duplicates with incomplete coverage.
The callback charges each heading, each retained finding and potential slice
copy before append through the original compound caller/operation and sticky
work/node/elapsed controls. It uses original raw Label spans and adds no source,
parse, index, allowance, edit or write authority. See [MD025 policy](rules/headings/md025.md).

MD026 adds HeadingTerminals, a narrow provenance projection sharing the exact
semantic walk with HeadingFact.Visible. Existing Visible and Label contracts stay
unchanged. It selects one final non-whitespace scalar before code/escape exclusion;
Exact text selects original rune bytes and transformed tokens remain atomic.
Code text participates in selection, so it cannot expose earlier punctuation.
The projection borrows actual immutable inline segments under the completed
original parse and allowance, charges tree/segment/rune visits and output growth,
and withholds all facts on failure, including final publication checks.
RuleContext joins caller and origin controls; the callback reserves scalar-set
construction, membership and finding growth, retaining reliable prior findings
with a late interruption. No source/parse/index/allowance/tree clone, correction
provider or write authority is added. This is finite application-work accounting,
not a process memory or throughput guarantee. See [MD026 policy](rules/headings/md026.md).

MD036 adds EmphasisHeadings, a narrow completed-parser projection of actual
one-line emphasized Paragraph labels and their immediate same-parent prose
followers. It borrows immutable internal nodes and the original physical index;
actual retained container prefixes and one cumulative nonblank-line pass prevent
jumping source-only reference definitions. Candidate descendants and follower
ordinary Text use actual parser ownership, including task and raw-region
exclusions. Original outer envelopes retain both delimiter markers.
All line arrays, tree/marker/line/rune visits, binary searches and retained output
growth share the original work/node/time allowance. Caller/origin controls and
final withholding apply; callback interruptions retain reliable prior findings.
No fresh source, parser, index, budget, public-tree clone, edit or write authority
is added. Accounting bounds application work, not process RSS or throughput.
See [MD036 policy](rules/headings/md036.md).


MD041 adds OpeningBlock, a narrow original-operation projection of the first
actual root body block after fully comment-owned introductory blocks. It borrows
actual comment/tail and raw-protection facts and scans only uncovered ASCII
whitespace gaps, without copying comment payload or a public tree. It retains
one original zero point/heading-level record; empty bodies use original EOF.
Root/comment/marker/source-gap visits and the fixed output record are charged
before work/publication. Caller/origin and sticky work/node/time controls remain
shared; any failed projection withholds its fact, including at the final check.
The callback short-circuits a sufficient opening heading before metadata decode;
otherwise opt-in title truth supplies an alternative or explicit affected
coverage. No Source, parse, index, allowance, edit or write authority is added.
Finite application accounting is not a process-memory or throughput guarantee.
See [MD041 policy](rules/headings/md041.md).

MD043 compiles its exact single-line descriptor list through HeadingOutline after
reserving independent entry storage, without Options' eager list clone or mutable
option borrowing. An additive semantic list domain rejects invalid entries at
admission, with per-element source locations even while disabled. Empty outlines
avoid heading projection and comparison workspace.
For nonempty patterns, existing HeadingFact level/Visible/Label/Envelope facts
supply the whole source-ordered sequence. A checked suffix cost/action grid reserves
both arrays under the original allowance before allocation; cells, transitions,
chunked repeated exact byte comparisons and canonical reconstruction are charged.
Worst-case matching uses O((patterns+1) * (headings+1) * (label-bytes+1)) work
and O((patterns+1) * (headings+1)) state, rather than unbounded wildcard backtracking. Overflow or exhausted
original work/node/time controls produces explicit incomplete analysis, not a
guessed mismatch. The completed-original parser/index witness and compound
caller/origin checks also apply to empty option access. Failed alignment withholds
its observation through the final check; existing late-operation handling retains
one already justified finding. No source, parse, index, budget, public tree clone,
second Visible definition, correction or write authority is introduced. These are
finite application controls, not hard RSS or OS preemption guarantees.
See [MD043 policy](rules/headings/md043.md).

MD063 reuses the shared heading Visible walk with independently owned scalar,
code-owner and exact/atomic source-token records. Proper-name list storage is
reserved before copying; immutable string bytes may be borrowed without exposing
mutable engine options. Name protection is settled before selecting a dependent
initial, with repeated scalar comparisons, protection writes, word visits and
finding records charged to the original operation. Worst-case matching work is
O(heading-scalars * names * name-scalars); retained state is linear in projected
heading scalars and option-list entries, bounded by the original work/node/time
allowance rather than a new name cap. Caller/origin cancellation and completed
private parse/source/index identity remain required, including final withholding.
Already justified earlier-heading findings follow existing late-error handling.
No Source, parse, index, allowance, second renderer, public tree clone or correction
authority is added. These are application accounting controls, not hard RSS or OS
preemption guarantees.
See [MD063 policy](rules/headings/md063.md).

MD080 uses OriginalHeadingAnchors to invoke the existing shared resource index
builder on the actual completed original parse. A private profile/options/source/
index/operation witness prevents forged public parse fields from granting facts.
Only owned heading records and availability escape; no Service/cache/view/content
witness, resource read, Root or new parse/source/allowance is introduced. Original
caller and origin cancellation, sticky work/node/time and final withholding apply.
The shared builder retains raw labels from the same HeadingFact and preserves its
whole-index uncertainty. MD080 reserves a finite seen-map capacity before allocation,
charges borrowed base bytes before hashing/equality and reserves actual output
records; immutable string bytes are not charged again as constructed nodes. Every
later present-base duplicate, including empty bases, uses the original full Label.
Local AnchorStyle defaults github, validates github/explicit and persists through
plans/candidates. This does not enable index_file or cross-file consumers. Finite
application accounting is not a hard RSS or OS-preemption guarantee.


### Empty sections

MD082 uses the parser-owned EmptySections projection under the complete original
production profile, parse options, source/index and operation allowance. One
pending actual heading and intervening body flag resolve each heading once; a
deeper next heading itself satisfies its parent. All actual container and hidden
headings participate before filtering. Only the globally last actual heading per
document can receive the boolean allow_final_section exemption. Labels share the
raw heading-label helper, including empty-label points; own inline labels are not
body. Semantic whitespace, actual object presence and confirmed comment ownership
follow the [MD082 policy](rules/headings/md082.md).

Traversal stack growth and derived output are reserved before allocation. Borrowed
AST visits and bounded semantic UTF-8 byte scans use work from the original
allowance; both caller and origin controls and final publication are checked.
An interrupted projection withholds its inventory atomically. Raw/comment state
uses actual private nodes and owned content/tails, excluding container prefixes.
This provider does not build an anchor index or consume AnchorStyle, metadata,
resource availability, correction, write or MCP authority. MD082 remains default
disabled and report-only; MD047 retains the sole production correction.


### Thematic breaks before headings and explicit slides

MD090 borrows direct actual body-block siblings under the complete original
production profile/options/source/index/budget witness. An iterative reserved stack
keeps each container sequence separate; owned first/last thematic markers provide
one original range without raw scanning. Comments interrupt this adjacency. Labels,
traversal/output storage and final publication share original operation controls.

A narrow immutable per-path slide provider stays on the selected execution plan,
including candidate and saved views. Configuration owns the existing direct bounded
matcher, first match and exact origin; adapters bind invocation paths to the selected
configuration directory once. The annotation follows mandatory encoding validation
and changes neither selection nor format. Option-aware applicability may use only
admitted options and explicit context before syntax; it cannot borrow partial syntax.
Known slide exemptions are retained independently of unrelated parser/directive
uncertainty. Inspection prepares context before callbacks and preserves truthful
completed sections when a later operation stop occurs. MD090 stays default disabled
and manual; MD047 remains the sole production correction.

## Heading inline construct projection

MD093 borrows actual inline kinds and complete source envelopes from each real
Heading using the completed private production profile, options, source/index and
original operation allowance. It walks nested wrappers independently, retains raw
HTML closer traversal and image-alt isolation, and never redispatches code, HTML,
destination or title payload. Escaped/unresolved text remains parser-owned text.
No new parse, renderer, public-tree clone, anchor index or resource service is used.

Traversal stacks, output and finding growth reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold incomplete inventories. The immutable admitted
allow list is read under the same controls into five fixed allowance bits; returned
facts own their slice and cannot mutate syntax or options. Nested constructs stay
independent and source geometry uses original envelopes, including container gaps.
The rule remains default disabled, RequiresSyntax and NoFix; MD047 alone owns edits.

## Original list structure projection

MD004 borrows actual List/ListItem ancestry and first-line marker facts through
the completed private production profile/options/source/index/original-budget
witness. Detached records retain group/item/parent envelopes, exact marker/prefix
spans and scalar list properties without mutable syntax handles or public-tree
cloning. Body-container preorder preserves outer-before-inner source order; code,
HTML, heading and paragraph payload is not reparsed. Later list rules can reuse
this seam without a second parse, renderer, anchor index or resource service.

Stack/fact storage and copied prefixes reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold unfinished inventories. MD004 selects one
per-document first-bullet baseline before directive filtering, or an admitted
explicit marker, and emits marker-only manual findings. It remains enabled by
default with PlannedFix metadata; MD047 alone supplies executable corrections.

MD005 reuses the same detached list facts. It compares structural marker
indentation (`ContentIndent - MarkerWidth - Padding`) against each actual group's
first direct item, before directive filtering. Full source-backed group spans
retain independent nested baselines without replacing an enclosing baseline.
Ordered-width and post-marker padding changes do not move marker starts.
Per-analysis baseline storage, comparisons and finding growth reserve original
work and records before construction, including aligned groups. Final checks
withhold interrupted output. Exact full item-marker spans produce manual
findings; default-enabled PlannedFix metadata grants no correction authority.

MD007 reuses the complete original list inventory and admitted scalar options.
For each actual bullet item, local indentation is ContentIndent minus MarkerWidth
minus Padding. With a bullet parent, its actual marker width plus padding plus
that local indentation must equal configured indent. With an ordered parent,
local indentation must be zero: its content column is the target without extra
indent. Parentless bullets require zero, or exactly indent with start_indented.
Quotes strip prefixes without resetting actual ancestry; hidden or misindented
parents remain anchors. A bounded invocation-local map keys constant source-token
and item offsets, retaining ordered anchors before children; storage, work and
finding growth reserve the original allowance even on clean inputs. Final caller
and origin checks withhold interrupted findings. Findings own one bullet byte
and remain manual despite PlannedFix metadata; MD047 alone produces edits.

MD029 reuses the original detached list facts and borrows its admitted `style`
under the same original operation controls. Each actual ordered group keeps an
immutable first direct numeric start and a direct ordinal, including wrong and
suppressed items. Independent nested groups and quotes retain enclosing state;
ordered descendants below bullets are targets. Numeric leading zeros are equal,
zero starts are valid, and comparing actual minus first to ordinal avoids growing
numeric sums. `one` requires numeric 1 even on the first item. State uses constant
source-token and group offsets, with storage, work, capacity, copies and finding
records reserved before construction, including clean inputs. Caller and origin
checks atomically withhold interrupted findings. Diagnostics own the full digits
and delimiter only. Default-enabled PlannedFix remains metadata; findings are
manual and MD047 alone supplies executable edits.

MD030 derives a detached separator inventory from the actual ListItems exactly
once. A precharged binary lookup in the original physical-line index establishes
first-line source presence (`Prefix.End < line.end`); marker end through prefix
end owns the diagnostic. No source clone, extra parse or grammar is introduced.
The original completed production witness, options, allowance and caller/origin
controls govern fact capacity, records, lookup, comparisons and finding growth.
Final checkpoints withhold all output on failure. The borrowed positive integer
is compared directly, without work or storage proportional to its value.

MD032 merges real body children and retained reference definitions in one bounded
original-tree traversal. Parent-prefix rollback and terminal content floors
distinguish list-owned separators from empty markers and opaque-child payload.
Its detached missing-side points remain manual, with no edit producer.

MD069 projects actual list items once into maximal identical-bullet runs using
direct ancestry, the original physical line index and ASCII space/tab gaps.
Mixed, ordered and quote boundaries retain independent inner subruns. Its
full-run manual findings preserve the permanent NoFix catalog policy.

MD076 projects direct sibling separator mismatches from completed production
syntax, selected options, original source/index and operation authority. The
bounded traversal follows ListSpacing's terminal-floor and ancestor-prefix
mechanics, preserving opaque leaves and source-only definitions. Consistent
adopts each actual list's exact first count before suppression. Deficits use the
next marker and surplus uses only the first extra blank body line through its
original ending. PlannedFix remains metadata; findings are manual.


MD077 projects detached continuation mismatches from final actual private
ownership and recognized container markers. Original cursor phase distinguishes
consumed quote columns from byte extents; nearest-item baselines retain all actual
ancestor item obligations on lazy lines. No grammar, public AST schema, parse
retention or correction capability is added.

MD009 projects manual physical suffix violations from completed original private
syntax, scalar options and the original operation. It does not clone public AST
or source/configuration, reparse, retain new parser metadata or produce edits.

MD010 projects manual one-byte original TAB findings through completed private
production ownership and the original operation. CodeSpan remains protected;
actual code Content/intrinsic indentation and trimmed semantic raw-body boundaries
follow [MD010 policy](rules/whitespace/md010.md). Charged confirmed-prefix replay
preserves physical phase and residual atomic TAB ownership without grammar or
public model changes. MD047 remains the only correction producer.

MD012 projects manual first-excess body spans from global maximal original
physical blank runs. It borrows completed production syntax, actual local prefix
ends and protected code/raw/header intervals under the original allowance. Empty
payload endings retain ownership; protected lines break runs. Full context precedes
START suppression and inspection slicing. Capacity, copies, records and lookups
are reserved before work; caller/origin final checks withhold interrupted output.
See [MD012 policy](rules/whitespace/md012.md); no correction provider is added.

MD013 maximum-line-width analysis is available but disabled by default. A narrow completed-original projection borrows immutable source/index/options/profile/reference facts and the originating operation, returning detached first eligible scalar spans. Four admitted options pass through the facade; precharged ownership/raw-prefix/URL work and final producer/facade/callback checkpoints retain caller and origin controls. Findings are manual, with no correction authority.

MD085 analysis and configuration are implemented for CommonMark and GFM, disabled by default with no rule-specific options. The completed original parser projection excludes whole container owners, fixes the first actual Paragraph content baseline, and separates ordinary Text witnesses from protected multiline prefixes and actual HardBreak successors. Original SPACE/TAB visual four-column indentation yields detached surplus-only manual evidence; MD047 alone produces corrections. Producer, guarded facade and callback share the original finite allowance and caller/origin publication checks.

MD065 analysis uses a narrow completed-original ThematicSpacing projection and an
optionless RuleContext facade. A full tree/inline/tail walk preserves semantic raw
lifetimes and image isolation; a separate body walk merges retained definitions
with direct source participants. Actual prefix rollback and terminal list/item
content floors preserve immediate physical separators. Detached Point/Before facts
carry present zero-width original evidence only. No source, parse, index, options,
allowance, AST inspection field or correction capability is added. Producer,
facade, comparison and whole callback reserve work/storage before use and finalize
under both caller and originating controls; any error withholds dependent output,
including empty, clean, protected-only and post-construction paths. MD065 remains
default enabled and report-only for CommonMark and GFM.


MD081 uses ParagraphEmphasis to borrow the completed private original production
source/index/options/profile and operation. Detached full Content-hull/count facts
retain actual Paragraph ownership, semantic Text provenance, Emphasis/Strong
boolean union and full raw-tag/closing-tail lifetime. Image descriptions are
opaque and cannot change outer raw state. No renderer, reparse, public AST/schema,
source/index/options clone, resource/metadata or correction authority is added.
Every traversal, terminal probe, provenance/byte/scalar/raw/option test, capacity,
copy and fact/finding reservation precedes work under the original finite controls.
Unicode table shape is guarded before bounded scalar classifications. Producer,
getter, facade, comparison and distinct whole-callback final checks bind caller
and origin and atomically withhold all dependent output on error, including empty,
clean, protected, minimum-skipped and post-output paths. MD081 remains disabled
and manual; MD047 alone produces corrections.


## MD088 original ASCII punctuation analysis

ASCIIPunctuation borrows the completed private original parse, source, production
profile, options, physical index and operation budget. It returns detached scalar
ranges only: actual Paragraph/Heading/GFM prose-cell Exact Text, transparent
formatting, all Link findings excluded with global Link raw transitions, and fully
opaque Image children/tails. It follows private rawTag/rawClosing/htmlTail lifetime
without resetting at owners. Code, comments/attributes, headers and retained
definitions stay protected. Entity/Escape/Normalized/PartialTab contributions
supply no candidates; Exact bytes are validated against original source.

Reservations precede structural and terminal visits, source/provenance checks,
byte association, bounded UTF-8 decode and fixed-six classification, raw comparisons,
stack/output growth, copied prefixes and retained facts/findings. Producer, style
getter, facade, comparison and distinct outer callback final gates retain caller
AND originating controls and sticky work/node/elapsed limits; any error atomically
withholds all dependent MD088 output, including empty/clean/protected/post-output.
No reparse, renderer, public-tree/source/index clone, new allowance or calibration
is used. Finite application accounting is not a CPU/RSS or throughput guarantee.

Exactly one original three-byte/one-code-point range per eligible scalar uses
`Use ASCII quotes and dashes in prose.` Full context precedes shared START
suppression and inspect intersection. Default OFF/style ascii/PlannedFix remain;
this pass creates no MD088 edits, previews or corrected content. MD047 is sole fixer.

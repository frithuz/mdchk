# Markdown linter engineering design

mdchk uses one Markdown analysis and correction engine for both its command-line interface and MCP server. Shared behavior is essential: the same files and configuration must produce the same findings and fixes through either interface.

## Confirmed choices

- Name the command `mdchk`.
- Implement the [89-rule catalog](rules/index.md) with stable numeric IDs and skipping MD002, MD006, MD008, MD015, MD016, and MD017.
- Implement the project in Go.
- Build and maintain an internal Markdown parser instead of using a third-party parser.
- Minimize third-party dependencies across the project.
- Use CommonMark 0.31.2 as the base syntax and support GitHub Flavored Markdown and Pandoc Markdown.
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

- Adopt the [fixed Pandoc profile](engineering/pandoc-compatibility.md), including source-preserving footnotes, tables, attributes, and rule-consumer behavior.
- Adopt the [resource contract](engineering/resources.md): bounded read-only adapters, partial standalone inventories, shared anchor/index policy, immutable observations, and coordinated fix views.

All choices below are proposals unless explicitly included in this list.

## Go and delivery

Use Go for the engine, CLI, and MCP server. Aim to distribute a single executable for macOS, Linux, and Windows without requiring users to install a language runtime.

Use the Go standard library by default. For each proposed third-party dependency, document the capability it provides, why an internal implementation is insufficient, its transitive dependencies, license, and maintenance cost. Minimize dependencies rather than assuming an absolute zero-dependency requirement. Prefer pure-Go packages when an external dependency is justified.

Choose a supported Go version and record the minimum version in `go.mod`. Pin dependency versions and commit `go.sum`. Benchmark realistic repositories before adding concurrency or caches.

## Markdown support and parsing

Use [CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/) as the shared core for CommonMark and GFM. The approved [GFM compatibility contract](engineering/gfm-compatibility.md) adds tables, task lists, single/double-tilde strikethrough, extended autolinks, and disallowed raw HTML handling from a frozen specification snapshot. It defines a profile with documented differences from the older `0.29-gfm` base. The approved [Pandoc contract](engineering/pandoc-compatibility.md) pins the Pandoc 3.12.1 `markdown` reader and all 74 extension states: 37 enabled and 37 disabled. Pandoc has its own block and inline interpretation. The configuration contract selects `commonmark`, `gfm`, or `pandoc`, defaulting to `gfm`. See [Markdown formats](markdown-formats.md) for the confirmed scope and current conformance status. Do not imply compatibility with every Markdown renderer.

Implement the parser in `internal/parser` using the Go standard library. Use the Markdown specifications as references for syntax behavior; do not depend on a third-party parser at runtime. Owning the parser also means owning its correctness, compatibility, and performance tests.

Build in stages: first identify block structure, including paragraphs, ATX and Setext headings, fenced and indented code, block quotes, lists, thematic breaks, and HTML blocks; then recognize inline constructs needed to protect content and support rules, including code spans, escapes, links, images, emphasis, and hard breaks. Handle nested containers and continuation lines explicitly. Add the selected extensions with dedicated fixtures rather than treating them as incidental line patterns.

The initial parser should preserve source spans and syntax context needed for linting and fixing. It does not need an HTML renderer. Document implemented syntax and known gaps; only claim CommonMark conformance after the relevant specification examples pass. Where syntax is unsupported or ambiguous, mark the affected region as uncertain, skip rules that require a reliable interpretation there, and report reduced coverage. Never auto-fix a region whose interpretation is uncertain.

Keep original source bytes alongside the syntax tree. Rules should use syntax context and source spans, rather than matching every line with regular expressions. Apply targeted source edits rather than rendering the whole document back to Markdown.

For GFM, retain original table cell counts before semantic padding or truncation, task-marker spans and spelling, strikethrough delimiter spans, and bare versus angle-delimited autolink origins. Keep filtered raw HTML protected and preserve its source. The [extension source contract](engineering/gfm-compatibility.md#extension-source-contract) defines the information shared by lint, fix, inspect, and MCP.

Make frontmatter recognition independently optional for every supported flavor. When enabled, treat recognized headers as opaque blocks, protected from Markdown style rules and directive interpretation. When disabled, parse the source using ordinary flavor rules. YAML and TOML support remains the initial proposal; the setting, default, delimiters, and malformed-header behavior need decisions before implementation. Exclude code blocks, inline code, and raw HTML from generic style rules unless a rule explicitly targets those constructs. Preserve LF or CRLF conventions. Reject unsupported encodings with a file error rather than silently transcoding them.

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

Follow the catalog's implementation contract for rule specifications, fixtures, format activation, and resource boundaries. Keep heading hierarchy and missing code language fixes report-only when author intent cannot be inferred safely. MkDocs, Quarto, metadata key inspection, and invalid encoding checks require explicit integration designs before coding their rules.

Start with one violation severity and no warning threshold controls. Add severities only when concrete rules justify them.

## Safe correction contract

`lint` must never modify source or configuration files, including when configuration requests fixing. Only the fix operation may write.

For each file, compute edits against an immutable snapshot. Validate bounds and replacement preconditions, deduplicate identical edits, and defer conflicting edits with an explanation. Apply nonoverlapping edits from the end of the file toward the beginning.

Reparse and validate the candidate result before writing. Define rule-specific preservation checks; syntax-tree equality alone is insufficient to prove every fix safe. Re-run enabled rules on the final content. A second fix run on unchanged inputs must make no further changes.

Preserve unrelated bytes, line endings, and file permissions. Use a temporary sibling file and replacement where the platform supports it. Before replacement, detect whether the source has changed since the snapshot; refuse stale edits. Resolve the remaining check-to-write race with an explicit filesystem strategy before claiming protection from concurrent writers. Never write through symlinks in the first release.

Writes are atomic per file where supported, not across a whole directory. Continue processing independent files after a file error and report both successful changes and failures. Do not claim that a failed run changed nothing. In dry-run mode, return diffs and predicted remaining findings without filesystem writes.

For cross-file checks, first build one candidate overlay for all fix targets and then analyze it consistently. Resource-only snapshots remain unchanged. After project writes, build the saved view from successful writes and confirmed unchanged snapshots; invalidate stale or uncertain dependencies and recheck affected results. A partial write must not produce findings against an imagined all-successful view. The [resource snapshot contract](engineering/resources.md#snapshots-caching-fixes-and-limits) defines these guarantees and their concurrency limits.

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

The [Pandoc corpus contract](engineering/pandoc-compatibility.md#corpus-licenses-and-completion-requirements) requires pinned offline fixtures, independent source-ledger assertions, and consumer preservation checks. Oracle agreement alone cannot prove retention of unused notes, duplicate IDs, or excess cells. Verify the entire fixed profile before advertising it.

Test engine properties: lint leaves bytes unchanged; fixing preserves protected content; fixes are idempotent; conflicting edits are reported; invalid configuration produces no writes; changed source snapshots are rejected. Add integration tests for CLI exit codes, JSON contracts, MCP parity, root boundaries, cancellation, and partial write failures.

Use Go fuzzing for the internal parser and edit application. Run `go test ./...`, `go vet ./...`, and race checks where supported. Benchmark a documented sample repository before setting a numeric performance target.

Before the first release, publish rule documentation, install instructions, platform artifacts, checksums, license, configuration examples, and versioning policy. Treat changes to default rules, fix behavior, configuration, and structured output as compatibility decisions.

## Remaining work and decisions

1. Define frontmatter configuration and header recognition details.
2. Implement and verify the approved Pandoc profile and resource adapters.
3. Complete safe-write mechanics and partial-write reporting against the approved resource snapshot contract.
4. Define internal parser milestones and choose an MCP implementation after small compatibility prototypes and a dependency review.
5. Choose a license, initial release platforms, and installation channels.


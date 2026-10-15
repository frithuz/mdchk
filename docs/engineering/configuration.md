# Configuration engineering design

The shared configuration loader implements the [configuration contract](../configuration.md) for CLI and MCP. `mdchk config validate` also scans a project for definitions and validates each eligible file independently. Its [command guide](../commands/config/validate.md) defines selection, exclusions, output, and exit behavior. The contracts below are approved; implementation is pending.

## Confirmed requirements

- Provide validation under the `config` command group as `config validate`.
- Scan every eligible project configuration file, including nested files.
- Respect Git ignores and configuration exclusions.
- Validate configuration definitions without modifying files.
- Discover `.mdchk.yaml` only, require schema version 1, and accept canonical MDxxx rule IDs without descriptive aliases.
- Keep catalog defaults independent of executable rule registration and expose incomplete analysis explicitly.
- Confine explicit configuration selection to the operation's existing discovery boundary and repository scope.

## YAML dependency and decoding

Use `github.com/goccy/go-yaml` v1.19.2 behind `internal/config`, pinned when the Go module is introduced. Its source revision is `92bc79cb5f685e999ad131473168fc45215d12d9`. It supplies general YAML parsing, syntax nodes, source positions, and duplicate-key diagnostics. The [pinned module](https://github.com/goccy/go-yaml/blob/v1.19.2/go.mod) declares no external module requirements, and its [license](https://github.com/goccy/go-yaml/blob/v1.19.2/LICENSE) is MIT.

YAML's comments and readable mappings justify this one dependency for hand-edited configuration. Standard-library JSON was the dependency-free alternative. An internal YAML parser would create a separate syntax-compatibility burden unrelated to the internal Markdown parser. At the 2026-10-14 review, the YAML organization's maintained v4 line was still a release candidate, while v3 received security fixes only. Track the selected library's maintenance and releases, retain its license, and test the adapter when upgrading. This decision does not select a metadata rewriting library.

Parse to syntax nodes before constructing effective settings. A focused v1.19.2 decoder check showed that `Strict()` still coerces quoted numbers, truncates `version: 1.9` to `1`, accepts null and custom tags, and accepts the first of multiple documents. The option does not establish mdchk's schema. Validate the complete document count, node kinds, scalar spellings, string keys, aliases, duplicates, and option domains independently. Retain source locations and both key locations for duplicate diagnostics.

Use the explicit YAML restrictions in the user guide: a single UTF-8 mapping document; decimal integers; true/false booleans; no null, merge keys, custom tags, cyclic aliases, or external references. Accept ordinary presentation forms and acyclic same-document aliases. Validate an alias's resolved value without losing its use-site provenance. Do not enable the library's external file/directory reference helpers. Do not implement a private YAML grammar or assume that generic struct decoding provides strict schema validation.

Standalone JSON input uses the same semantic validator. Reject duplicate JSON names, invalid raw UTF-8, and unpaired Unicode surrogate escapes at the transport boundary before ordinary decoding; retain property paths and avoid scalar coercion. A valid literal U+FFFD remains valid. Neither serialization permits an unsupported field to disappear silently.

## Defaults and availability

Maintain metadata for all known catalog IDs separately from executable implementations. Metadata supplies defaults and option domains even for unavailable rules. Reject reserved IDs, aliases, unknown IDs, duplicate entries, and invalid options on disabled rules. Empty option mappings select defaults. Record whether a rule has an explicit entry so capability validation can distinguish requested behavior from unmentioned defaults.

An unavailable rule with an explicit entry and effective `enabled: true` is a configuration error. Explicitly disabled unavailable rules are valid after option validation. Unmentioned unavailable default-enabled rules remain enabled expectations and produce `rule-unavailable` coverage records during analysis; unavailable default-disabled rules do not. Evaluate dialect applicability separately: a rule inapplicable to the selected dialect does not reduce coverage. Preserve the catalog defaults rather than enabling only registered implementations.

Validate effective flavor and directive-processing availability before analysis. Default GFM never falls back to CommonMark. Before directive processing exists, effective global directive permission must be false. Validate the approved `frontmatter`, `integrations.quarto` and `documents.slides` settings. Explicitly enabled recognition and enabled-rule options requiring unavailable metadata capabilities fail capability validation. See [initial capability handling](../configuration.md#initial-capability-handling).

Separate schema/policy validation from selected-operation capability validation. Ancestors consulted only for policy must be completely schema-valid, but their unselected rule options and flavor do not undergo execution-availability checks or become inherited settings. Each definition visited by `config validate` is checked as a selected definition using inherited permissions. Unmentioned unavailable default rules do not make that scan incomplete because it does not run Markdown checks.

## Shared resource policy

Accept optional top-level `resources` with only `index_file` and `anchor_style`, using the domains and defaults in the [configuration guide](../configuration.md#shared-resource-settings). Validate both even if no enabled rule consumes them. Reject the removed per-rule options `MD057.index_file`, `MD051.anchor_style`, and `MD080.anchor_style`, including on disabled rules. Keep schema version 1 and all catalog rule enablement defaults unchanged.

Track explicit shared settings for capability validation. Unsupported requested behavior is a configuration error; unavailable unmentioned defaults create coverage deficits only when applicable analysis requires them. A selected `pandoc` flavor requires the whole [pinned profile](pandoc-compatibility.md); no extension-option mapping is accepted.

Resource roots, lookups, snapshots, and standalone maps follow the [resource contract](resources.md). They do not change config discovery or introduce per-resource settings. Only the selected definition supplies resource policy over built-in defaults; ancestor resource settings are not inherited.

## Discovery and selection

Lint/fix start discovery at the working directory; inspect starts at the canonical target's parent. Stop after checking the nearest enclosing repository root, recognizing both `.git` directories and worktree `.git` files; otherwise stop at the filesystem root. Use one selected file for settings. Keep project MCP discovery inside its startup root and standalone configuration entirely in memory.

Resolve relative `--config` from the working directory, then reject selection outside the operation's established boundary or in another repository scope. Do not follow symlinks for configuration access or expand scope to accommodate an explicit config. Explicit CLI documents are not restricted to the selected config's directory. External shared configuration needs a separate future policy design.

Use the [file-selection grammar](../file-selection.md#exclusion-pattern-grammar) for configuration patterns. Implement per-segment `path.Match` semantics and a whole-segment globstar with bounded, memoized matching. Validate the full pattern before matching, and test ancestor-directory exclusions for explicit files. This matcher neither traverses the filesystem nor implements Git ignore semantics. Use the command's shared file-selection adapter for case handling, hidden directories, symlinks, and skip reports.

## Shared validation

Use the same configuration decoder, schema, rule registry, option validation, and exclusion matcher as lint, fix, and MCP. Detect syntax errors, duplicate keys, unsupported versions, unknown fields and rules, invalid types and options, and invalid patterns. Preserve source locations for configuration diagnostics. A definition is invalid when any of these checks fails; do not apply a partially decoded exclusion list.

Separate validation of definitions from configuration selection. This command validates nested files without introducing nested overrides or merging for lint and fix. Its project argument sets a bounded scan root; it never discovers configuration above that root.

## Higher-level exclusion policy

Use boolean `allow_nested_excludes`, default true. A valid definition setting it to false locks descendants against adding nonempty `exclude` lists. Its own exclusions remain permitted unless an earlier ancestor already imposed a lock. Combine ancestor permissions with logical AND; no descendant or explicit config selection can reopen a denied permission. Omission and an explicit child true remain valid but ineffective under a lock. Reject a prohibited nonempty exclusion list as a located configuration error identifying the governing ancestor. Preserve restriction provenance for ineffective permission values, consistently with directives.

Keep policy validation separate from rule-setting selection. When loading a selected configuration for lint, fix, inspect, or project MCP, resolve ancestor policies between the selected file and the operation's discovery boundary, including with `--config`. Validate applicable ancestor schemas and policies before trusting them. Invalid ancestor definitions fail configuration resolution; inspect reports unresolved configuration. Their unselected execution capabilities are not inherited. Do not merge ancestor rule settings or silently activate nested rule overrides.

Project-wide validation carries policy state top-down within its selected scan root. It does not read ancestors outside that root. A policy-invalid descendant contributes no exclusions or new policy settings, while inherited valid restrictions remain active. Standalone MCP has only supplied configuration and no filesystem hierarchy; validate the field's type but do not discover server-local ancestors. This controls mdchk `exclude` additions, not Git ignore files; no separate mdchk ignore-file format is defined.

## Inherited directive permissions

Resolve global `directives.enabled` and per-rule `allow_directives` as the logical AND of the selected definition and all applicable ancestor values. Omitted values contribute true; any false remains binding. Child true values are valid but ineffective under a restriction, unlike prohibited exclusion lists. Record each restricting definition path and field for inspection and blocked-effect notes. Do not merge ancestor rule enablement, options, or flavor. This policy restricts comments, not child configuration edits.

Use the existing exclusion-policy ancestor discovery boundaries, including explicit `--config`. Validate applicable ancestors before trusting permission values; invalid or unreadable ancestors fail normal resolution and leave inspection unresolved. Project MCP remains bounded by its startup root. Standalone MCP uses submitted permissions only. During project validation, carry permission state top-down within the scan root; invalid definitions contribute no new permissions and inherited valid restrictions remain in force.

Verify global and per-rule ancestor denials, omission, explicit child true, additional child restrictions, multiple ancestors, explicit config selection, boundary-limited validation and MCP, invalid ancestors, and provenance. Confirm child rule enablement and options remain independently selected.

## Traversal and exclusion scope

Use a top-down traversal. Check inherited Git and mdchk exclusions before entering a directory. In an eligible directory, validate its recognized `.mdchk.yaml`, then activate its exclusions for descendants only if the entire definition is valid. Validate the current configuration before testing its own exclusion patterns. Inherited configuration exclusions accumulate and cannot be negated by descendants.

Continue below an invalid or unreadable configuration using inherited exclusions, recording a configuration error. Continue through other independent directories after access failures and mark the scan incomplete. Skip symlinks and Git metadata. Do not read configurations or ignore files inside pruned directories. Record skipped directory roots and reasons without claiming to enumerate their unseen contents.

Follow [Git ignore pattern semantics](https://git-scm.com/docs/gitignore), including directory pruning, pattern precedence, nested files, negation, and applicable repository-local and user ignore sources. For this scan, apply matching patterns regardless of Git tracking status. Use internal matching and discovery so the command does not require a Git executable. Outside a repository, honor `.gitignore` files within the selected root. Directory-level exclusions prevent traversal even when a tracked configuration exists below that directory. Test pattern behavior against Git in development without adding it as a runtime dependency.

Git ignores and valid mdchk exclusions combine as exclusion sources; a path must pass both. Configuration patterns are relative to their defining directory. No configuration can broaden the scan root, exclude its own validation, or cause skipped descendants to be read. Scanning multiple definitions is separate from evaluating Markdown rules.

## Results and command handling

Accept one optional project-directory argument and `--format text|json|vscode|github`, the shared repeatable `--limit name=value` option, plus global options. Reject unsupported flags such as `--config` and `--dry-run`. An existing directory with no eligible configuration files is a successful empty scan, distinct from lint or fix selecting no Markdown files.

Use the shared result envelope with operation `config validate`, configuration paths in checked files, skip reasons, empty `diagnostics` and `coverage` lists, no changes, and categorized `errors`. Invalid definitions are configuration errors, not Markdown rule violations. A fully traversed scan can be complete even when definitions are invalid; unreadable files or directories and untrusted exclusions from invalid configs must be explained in the report. A valid config exclusion is intentional scope, not incomplete work.

Include optional paths and source ranges in error records. Text reports validation counts; JSON retains structured errors; VS Code formats located errors as problem-matcher lines using stable identifiers such as `config-definition`; GitHub emits located error annotations. Location-free errors remain stderr messages in VS Code mode and annotations without source coordinates in GitHub mode. All formats preserve the command's error categories and exit precedence from the command guide.

## Verification

Cover multiple valid and invalid definitions, duplicate keys, unknown rules, invalid options and globs, unreadable files and directories, nested scope, ancestor pruning, self-exclusion, invalid-config exclusions, inherited exclusion locks, descendant attempts to reopen permission, explicit-config selection under a lock, discovery boundaries, Git negation and tracked-file behavior, symlinks, empty scans, and all four reporters. Verify no filesystem writes and that lint, fix, inspect, and MCP use shared policy resolution without merging rule settings.

Use the [configuration examples](configuration-examples.md) as fixture inputs and expected outcomes. Include coercion traps, multiple documents, aliases, disabled-rule validation, explicit versus implicit unavailable rules, inherited false versus explicit true, and excluded explicit descendants. Verify incomplete analysis uses the shared [coverage result](../output-formats.md#incomplete-analysis) and exit 15 without turning configuration validation or inspect into lint operations.

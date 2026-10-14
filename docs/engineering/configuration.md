# Configuration validation engineering design

`mdchk config validate` scans a project for configuration definitions and validates each eligible file independently. The [command guide](../commands/config/validate.md) defines selection, exclusions, output, and exit behavior.

## Confirmed requirements

- Provide validation under the `config` command group as `config validate`.
- Scan every eligible project configuration file, including nested files.
- Respect Git ignores and configuration exclusions.
- Validate configuration definitions without modifying files.

## Shared validation

Use the same configuration decoder, schema, rule registry, option validation, and exclusion matcher as lint, fix, and MCP. Detect syntax errors, duplicate keys, unsupported versions, unknown fields and rules, invalid types and options, and invalid patterns. Preserve source locations for configuration diagnostics. A definition is invalid when any of these checks fails; do not apply a partially decoded exclusion list.

Separate validation of definitions from configuration selection. This command validates nested files without introducing nested overrides or merging for lint and fix. Its project argument sets a bounded scan root; it never discovers configuration above that root.

## Higher-level exclusion policy

Add boolean `allow_nested_excludes`, default true. A valid definition setting it to false locks descendants against adding nonempty `exclude` lists. Its own exclusions remain permitted unless an earlier ancestor already imposed a lock. Combine ancestor permissions with logical AND; no descendant or explicit config selection can reopen a denied permission. Do not treat omission of the field as an override of an inherited lock. Reject an explicit attempt to reopen it or a prohibited exclusion list as a located configuration error identifying the governing ancestor.

Keep policy validation separate from rule-setting selection. When loading a selected configuration for lint, fix, inspect, or project MCP, resolve ancestor policies between the selected file and the operation's discovery boundary, including with `--config`. Validate applicable ancestor definitions before trusting their policies. Invalid ancestor definitions fail configuration resolution; inspect reports unresolved configuration. Do not merge ancestor rule settings or silently activate nested rule overrides.

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

Accept one optional project-directory argument and `--format text|json|vscode|github`, plus global options. Reject unsupported flags such as `--config` and `--dry-run`. An existing directory with no eligible configuration files is a successful empty scan, distinct from lint or fix selecting no Markdown files.

Use the shared result envelope with operation `config validate`, configuration paths in checked files, skip reasons, an empty Markdown `diagnostics` list, no changes, and categorized `errors`. Invalid definitions are configuration errors, not Markdown rule violations. A fully traversed scan can be complete even when definitions are invalid; unreadable files or directories and untrusted exclusions from invalid configs must be explained in the report. A valid config exclusion is intentional scope, not incomplete work.

Include optional paths and source ranges in error records. Text reports validation counts; JSON retains structured errors; VS Code formats located errors as problem-matcher lines using stable identifiers such as `config-definition`; GitHub emits located error annotations. Location-free errors remain stderr messages in VS Code mode and annotations without source coordinates in GitHub mode. All formats preserve the command's error categories and exit precedence from the command guide.

## Verification

Cover multiple valid and invalid definitions, duplicate keys, unknown rules, invalid options and globs, unreadable files and directories, nested scope, ancestor pruning, self-exclusion, invalid-config exclusions, inherited exclusion locks, descendant attempts to reopen permission, explicit-config selection under a lock, discovery boundaries, Git negation and tracked-file behavior, symlinks, empty scans, and all four reporters. Verify no filesystem writes and that lint, fix, inspect, and MCP use shared policy resolution without merging rule settings.

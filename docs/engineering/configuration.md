# Configuration engineering design

The shared configuration loader implements the approved
[configuration contract](../configuration.md) for lint preparation and target
execution through the shared parser/rule engine. Shared decoding, semantic
validation and bounded one-file policy resolution are implemented as described
below. Missing enabled rule implementations produce genuine coverage deficits;
lint presents shared results as readable text or schema-v1 JSON. VS Code/GitHub
reporters remain deferred.

The contract also covers MCP and the implemented `mdchk config validate` command,
which scans a project for definitions and validates each eligible file
independently. Its [command guide](../commands/config/validate.md) defines
selection, exclusions, output and exit behavior. Other command adapters remain pending; downward command/report integration
is implemented for text and JSON. The shared downward validation
adapter is implemented in `internal/configscan.Validate`: it admits each eligible
definition before atomically publishing exclusions and permission denials, carries
valid inherited policy after rejected or unreadable files, and prunes directories
and individual definition files before acquisition. Its owned outcomes retain
same-read source hashes and located errors. Readable invalid definitions do not
make an otherwise fully traversed scan incomplete; acquisition failures do.
Typed source roles distinguish definition acquisition from traversal/Git-source
failures without basename inference. Shared operation accounting covers YAML and
alias construction, semantic validation and exclusion matching.

## Implemented configuration boundary

`internal/config` defines passive version-1 schema types for all approved fields,
including shared resources, optional contexts, exclusions and directive permissions.
Pointers retain omission versus explicit zero values; rule-map membership retains
an explicit empty entry. Rule options reuse `engine.Options`. Field origins retain
definition identity, property paths and optional source positions for future
diagnostics and ancestor-restriction inspection. These data types do not validate
input or resolve inherited policy.

`DefaultSettings` returns independent built-in non-rule defaults, including GFM;
it does not mark fields explicit or depend on executable availability. Rule
defaults remain catalog metadata. Callers must carry the effective configured
flavor explicitly into the engine rather than relying on its internal CommonMark
fallback. Presence must survive validation, including explicit resource values
equal to defaults and rule entries containing only permissions or options.

`DecodeYAML` implements the filesystem-independent, strict version-1 syntax
adapter. It validates UTF-8, document count, the fixed structural schema, scalar
kinds and spelling, duplicate keys at every depth, aliases, tags and nulls before
returning a definition. `Definition.Syntax()` exposes an immutable owned tree for
semantic validation, including original scalar spelling and nested locations;
collection and origin accessors return defensive copies. Alias values are checked
at every use with use-site provenance; each alias keeps the anchor binding from
its original source occurrence even if that anchor name is reused later. Block
scalar spelling retains original header/body bytes, including significant trailing
newlines, using immutable shared source storage across alias uses. Explicit YAML core tags determine scalar
kind; tagged numbers and booleans still require decimal and true/false spelling.
Unknown rule IDs, option schemas, domains, patterns and capabilities remain the
responsibility of semantic validation. Successful syntax decoding alone grants no
availability.

The adapter enforces the accepted default 1 MiB configuration byte limit and 64
container levels, preflighting library tokens before recursive parsing and
checking actual AST containers (including alias uses). Source tokens and expanded
syntax records are each bounded by the 250,000 document-node budget; alias
traversal is capped at 256 and expansion work at 250,000,000 units. Each node visit
charges its path and scalar token lengths, and constructing child paths charges
their lengths before allocation. Source/index scanning, block extraction and
explicit-tag lexical validation are charged on first inspection; immutable block
spelling/text, successful tag validation and primitive numeric conversion results
(including unrepresentable results) are reused without body-sized alias copies or
repeated scans. Every fallback retains its own alias-use origin. Source indexing
recognizes CR, LF and CRLF and validates parser-derived boundaries before slicing. These local bounds do not claim aggregate
operation accounting or configurable operator limits, which belong to later
application integration.

The pinned lexer has a narrow empty/blank-block dedent defect. The adapter repairs
only its recognized header/body/error token pattern, with matching source-position
and following-token guards; original source bytes remain unchanged and the pinned
parser still validates the remaining structure. Real blank-line content remains
distinct from an absent body. Unrelated invalid tokens are never suppressed.
Recognized block header tokens are checked for one optional indentation indicator
and one optional chomping indicator, in either order, as YAML 1.2.2 specifies.

`DecodeJSON` validates raw supplied JSON through the same owned syntax and
semantic schema as YAML. Complete catalog metadata,
semantic and capability validation, exclusion matching, and bounded read-only
filesystem selection are implemented. The lint adapter uses `Resolve` before target access and retains its full
selection snapshot, with exclusions bound to the selected directory. Other
command adapters remain pending; config validate reuses the downward
multi-definition validation package and the shared text/JSON reporters.

## Bounded filesystem selection

`Resolve(SelectionOptions, registry, providers, capabilities)` implements initial
one-file discovery and ancestor policy resolution. `WorkingDirectory` supplies
relative explicit-path resolution; `StartDirectory` independently supplies the
upward discovery start, allowing inspect to use the resolved target's parent.
Both default to the invocation working directory. `BoundaryDirectory`, when
supplied, is an inclusive caller cap, allowing project MCP to retain its startup
root. Directory identities are resolved against the working directory; the
caller cap must identify its canonical nonsymlink directory. Every config
component must be a nonsymlink directory or regular file as appropriate. A target
adapter that explicitly resolves a document symlink supplies the resolved parent;
this does not allow symlinks in a configuration path.

Within the cap, discovery first establishes the nearest enclosing repository
from a `.git` directory or regular worktree file, without reading Git metadata or
running Git. That repository further narrows the boundary. Without either a
repository or a supplied cap, discovery reaches the filesystem root. Selection
then chooses the nearest `.mdchk.yaml`, or the explicitly named YAML file.
Explicit paths cannot widen the boundary or enter another repository scope.
A same-scope sibling is permitted; its ancestors come from the selected path,
not the working directory. A custom filename replaces the recognized filename
in its own directory. Explicit document targets do not participate in this API.

All recognized definitions in strict parent directories of the selected file,
through the inclusive boundary, undergo complete schema/domain validation in
top-down order. Before adding each definition's own permissions, the resolver
rejects its nonempty exclusion list under any prior nested-exclusion denial.
`PolicyError` retains the addition's origin and every governing restriction.
Only permissions combine; ancestor settings and exclusion patterns never merge.
Only the selected definition undergoes execution admission. With no selected
file, built-in and catalog defaults undergo normal admission, including the GFM
and global directive gates. No failure returns a partial usable `Selection`.

`Selection` owns the selected path, top-down ancestor paths, established
`Boundary` (including the repository identity when known), and an independent
`Resolution`. These identities are reusable by later inspect, resource-root and
multi-definition adapters; they grant no target or resource access themselves.
Production filesystem access uses confined `os.Root` directory handles, checks
all config components for symlinks, rejects nonregular files, and caps bytes read
before YAML decoding. On Unix, nonblocking/no-follow opens also prevent a
substituted FIFO from hanging startup or a final symlink from being followed.
Opened file and directory identities are checked against their pre-open identity.
Invalid, unreadable, missing explicit, symlinked or special definitions fail
without falling back to another file. IO causes remain inspectable through
`ValidationError`; YAML and semantic errors retain original file locations.

`Load(path)` is the low-level regular-file YAML syntax adapter. It rejects
symlinks and special files and retains owned source data, but does not establish
operation scope or validate semantic domains, permissions or capabilities.
Operation adapters must use `Resolve` for bounded selection and admission.

## Semantic validation API

`DecodeObject(map[string]any)` supplies the same owned syntax boundary as YAML
for already typed in-memory input. Its exact supported types are `bool`, `string`,
`int`, finite `float64`, `[]string`, `[]any`, and `map[string]any`. Named types,
null values (including nil maps/slices), invalid UTF-8, coercions, and cyclic
containers are rejected. Empty nonnil mappings/sequences are supported. A JSON
transport must preserve integer versus number kinds before calling this API;
ordinary JSON decoding to float64 does not satisfy the integer schema. Mapping
keys are sorted for deterministic object diagnostics. Object origins have
property paths and no filename, textual position, or invented scalar spelling.
Depth, syntax-node and expansion-work budgets bound traversal. Input maps and
slices are borrowed only during decoding; the returned snapshot retains no
mutable input collection. Callers must not mutate input concurrently with decode.
Raw-byte `DecodeJSON` rejects decoded duplicate names at every object depth,
malformed raw UTF-8 and unpaired Unicode surrogate escapes before conversion.
Literal U+FFFD and valid surrogate pairs remain valid. Integer tokens remain
integers; decimal/exponent tokens remain numbers, without float64 coercion.
Unknown fields survive until schema validation. Supplied JSON has property paths
and no invented file positions. The reusable `internal/strictjson` boundary also
accepts scalar, array and null roots for future callers; configuration rejects
those shapes through its schema. The caller supplies a positive byte limit
(1 MiB for configuration), with 64 container levels, 250000 syntax nodes and
250000000 traversal work units. No transport framing is implemented here.

`Validated.Configuration(capabilities)` returns an owned view before execution
admission, including unavailable explicit requests. It retains the selected
definition identity, effective settings, full rule catalog, and explicit/default
provenance for every setting and rule option. Empty rule entries remain explicit
while their omitted enabled/options leaves retain catalog-default provenance.
Resource defaults and explicit requests remain distinct. Build availability is
independent of enablement, dialect applicability and permission inheritance.
Rule options also require their selected value's capabilities. A true
`frontmatter_title` on MD001/MD025/MD041 is unavailable without metadata
interpretation, even when its registered rule is disabled. A false option
requires no metadata support.
Ancestor rule/flavor/options are never merged.

`Configuration.Shared()` projects this view into the schema-v1 shared
`ConfigurationData` model. Effective values use configuration field names; rule
records also expose `available` and `explicit` metadata, and `availability` maps
JSON Pointers to build support for all settings. Provenance uses sorted RFC 6901
JSON Pointers and source kinds `default`, `selected-config` or `supplied-config`.
Known YAML points become zero-width ranges; supplied objects and defaults have
no fabricated ranges or file hashes. Every call owns its maps, lists and positions.
`selected_permissions` maps permission JSON Pointers to selected values before
inherited restrictions, preserving explicit child true alongside effective false.
Inherited denials add `ancestor-config` sources and provenance records with
`inherited: true`, including every governing path and its actual field point.
Global denials retain `/directives/enabled`; they do not invent per-rule origins.

`ResolveValidated(options, registry, providers)` reuses bounded normal selection
and complete ancestor schema/policy validation without capability admission.
`Selection.Configuration(capabilities)` exposes the effective inspection view;
`Selection.Admit(capabilities)` admits its private retained snapshot with same-read
source evidence on failures. Invalid or unreadable ancestors return no partial
selection. Existing `Resolve` combines both steps for lint. Canonical-target
adapters supply `StartDirectory`; project adapters supply their startup root as
`BoundaryDirectory`. No inspect/MCP transport or command is introduced here.

`Configuration.OwnPermissions` and `EffectivePermissions` are owned passive
snapshots. `DirectivePermission(ruleID)` uses a private effective snapshot and
returns all global and rule-specific denials. Mutating exported settings, origins,
or returned decisions cannot grant permission. Nil/unresolved configurations and
unknown rule IDs grant nothing. This decision does not parse comments or mutate
directive rule-state intervals.

`PolicyState` provides the top-down transition for later bounded scan walkers.
Its zero value represents the explicit scan root and reads no ancestors.
`Apply` validates the entire decoded definition and inherited exclusion policy
before returning new state and an effective view. Any schema or policy error
returns the unchanged inherited state and no view. A caller retains its previous
state on decode/read errors as well. Only valid definitions activate immutable
compiled exclusion scopes and additional denials. The walker remains responsible
for bounds and pruning before reads; Git ignore discovery and scan commands are
separate work.

`ValidateDefinition(definition, registry, providers)` performs the entire schema
and policy-domain check without admitting execution. Both decoders feed this
same validator. It reconstructs supplied settings from `Definition.Syntax()`;
mutating the passive fields or `Provenance` does not change validation. Manually
constructed passive definitions without owned syntax are rejected. The registry
must retain all accepted catalog IDs, option schemas and enablement defaults;
use `engine.NewCatalogRegistry` with the build's actual registration subset.
Synthetic partial or altered catalogs are rejected. Rule domain resolution uses
`Registry.ResolveSettings` and its validation providers, including disabled-rule
and ancestor-only hostname validation. Patterns use `CompilePatterns` for the
entire exclusion and slide lists before returning a candidate.

The returned `Validated` is an immutable schema-valid snapshot, suitable for an
ancestor consulted only for policy. Unavailable ancestor flavors, rule
implementations and optional contexts do not fail this layer. No settings become
inherited. `Validated.Permissions(inherited)` snapshots inherited and own false
values with all governing origins. Its zero-value input permits everything.
Only directive and nested-exclusion denials combine; an explicit child true
cannot reopen them. Discovery supplies the bounded ancestor order and rejects
prohibited descendant exclusion additions; neither operation is performed here.

`Validated.Admit(capabilities, inherited)` checks the selected definition against
effective permissions and returns a fresh owned `Resolution`, or nil on any
failure. Config validation uses this same admission for every selected definition;
it does not run Markdown checks or report unavailable unmentioned default rules
as coverage. Rule availability comes only from executable registrations and the
existing MD094 core gate. GFM selected by omission still requires GFM support.
Global directive processing is required unless an own or inherited global false
is effective; per-rule permission denials do not substitute for that global gate.

`Capabilities` is an explicit trusted assertion by the build adapter, independent
of default settings. Separate dimensions cover CommonMark/GFM, directive processing, YAML and TOML recognition, metadata
interpretation, slide annotation, index handling and each
anchor style. An explicit index basename also requires the pure active-adapter
`RepresentIndexFile` predicate. No capability flag implements those features or
grants resource access. Explicit resource values equal to built-ins remain
requests; an empty resource mapping requests neither field. Disabled recognition
needs no format capability. A nonempty slide list requests annotation directly.
Available enabled rules requesting `frontmatter_title: true` additionally require
metadata interpretation. Context selection never enables rules or another context.

`Resolution` owns effective settings/options, rule explicitness/availability,
explicit-field origins, and separate own/effective permission denial origins.
A missing origin denotes a built-in or catalog default, including omitted rule
options; present values equal to defaults remain explicit. Compiled pattern
matchers are immutable. Result collection mutations do not affect another
admission or validation. `ValidationError` locates semantic/capability failures;
`engine.SettingError` identifies rule, option and optional zero-based list element.
Both preserve provider causes through `errors.Is`/`errors.As`. YAML diagnostics
retain source positions, including alias use sites; object diagnostics retain
only property paths. No validation step calls a parser, rule check, filesystem,
reporter or write adapter, and failures return no partial usable resolution.

`SourceError` wraps failures after successful configuration acquisition and
preserves underlying typed errors through `Unwrap`. `Sources()` returns owned
path, SHA-256 and byte-count metadata computed from the same bytes used for
decoding, including acquired ancestors needed for governing policy origins.
Reporting matches each origin to that metadata and never rereads a path to
construct a location. Missing or unreadable files have no fabricated snapshot
or range; replacing a configuration after failure cannot change its reported
identity. Standalone syntax/semantic validation still performs no filesystem IO.

## Admitted lint execution

`Resolution.ExecutionPlan()` returns the immutable admitted bridge to the shared
engine. A passive or manually constructed resolution grants no execution
capability. The plan retains the original executable registry, schema-validated
provider results, selected flavor, options, explicit rule membership, effective
permission denials and provenance. `Configuration()` returns a fresh owned view;
mutating that view or the original resolution cannot change execution.

Create target snapshots with `plan.NewInput(path, bytes)` and run `plan.Lint`
with one shared operation budget. This delegates to `Registry.Lint` and its
existing mandatory UTF-8 gate, parser, coverage collection and finalization.
Resolved defaults are not converted into supplied settings: absent enabled
implementations still produce `rule-unavailable`, while explicit enabled absent
implementations fail admission before target access. Disabled and positively
dialect-inapplicable checks do not reduce coverage. Catalog table rules apply to
GFM; footnote contracts are inactive for both supported profiles. Missing optional resource or
integration context never establishes dialect inapplicability.

CLI preparation retains this plan and execution passes acquired snapshots to it
with the same acquisition budget and context. The engine owns the single UTF-8
gate, parser and callback execution. Selection/acquisition observations merge
through `engine.FinalizeLintResult`, which regenerates canonical IDs, cause links
and per-file references before deriving the shared outcome. Public CLI paths
are relative to the invocation directory; acquisition keeps native names private.
Lint text and JSON reporters present the canonical result without rereading
configuration sources or target documents. CommonMark/GFM, MD001 analysis with opt-in metadata titles, MD003 heading-style
analysis, MD004 bullet marker analysis, MD005 sibling-list alignment analysis, MD007 bullet nesting indentation analysis, MD029 numbered-list prefix analysis, MD030 list-marker separator analysis, MD032 list-boundary blank-line analysis, MD069 repeated-list-marker analysis, MD076 list-item spacing analysis, MD077 list-continuation indentation analysis, MD009 trailing-whitespace analysis, MD010 tab-character analysis, MD012 blank-body-run analysis, MD013 maximum-line-width analysis, MD027 blockquote-separator analysis, MD028 blockquote-gap analysis, MD064 repeated-prose-space analysis, MD085 paragraph-continuation analysis, MD089 CJK/Latin-spacing analysis, MD035 horizontal-rule-style analysis, MD018/MD019 opening-hash spacing analysis, MD020 terminal-hash spacing, MD021 closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026 heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041 opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization analysis, MD080 heading-anchor collision analysis, MD082 empty-section analysis, MD090 thematic-break-before-heading analysis and MD093 inline-formatting-in-headings analysis, MD047, directives,
shared suppression and VS Code/GitHub reporters are implemented. MD013, MD026, MD036, MD043, MD063, MD080, MD082, MD085, MD089, MD090 and MD093 are disabled by default. Other rule
callbacks and optional contexts remain separate deliveries.

## Confirmed requirements

- Provide validation under the `config` command group as `config validate`.
- Scan every eligible project configuration file, including nested files.
- Respect Git ignores and configuration exclusions.
- Validate configuration definitions without modifying files.
- Discover `.mdchk.yaml` only, require schema version 1, and accept canonical MDxxx rule IDs without descriptive aliases.
- Keep catalog defaults independent of executable rule registration and expose incomplete analysis explicitly.
- Confine explicit configuration selection to the operation's existing discovery boundary and repository scope.

## YAML dependency and decoding

Use `github.com/goccy/go-yaml` v1.19.2 behind `internal/config`, pinned when the Go module is introduced. Its retained [MIT notice](../../third_party/go-yaml/README.md) records provenance. Its source revision is `92bc79cb5f685e999ad131473168fc45215d12d9`. It supplies general YAML parsing, syntax nodes, source positions, and duplicate-key diagnostics. The [pinned module](https://github.com/goccy/go-yaml/blob/v1.19.2/go.mod) declares no external module requirements, and its [license](https://github.com/goccy/go-yaml/blob/v1.19.2/LICENSE) is MIT.

YAML's comments and readable mappings justify this one dependency for hand-edited configuration. Standard-library JSON was the dependency-free alternative. An internal YAML parser would create a separate syntax-compatibility burden unrelated to the internal Markdown parser. At the 2026-10-14 review, the YAML organization's maintained v4 line was still a release candidate, while v3 received security fixes only. Track the selected library's maintenance and releases, retain its license, and test the adapter when upgrading. This decision does not select a metadata rewriting library.

Parse to syntax nodes before constructing effective settings. A focused v1.19.2 decoder check showed that `Strict()` still coerces quoted numbers, truncates `version: 1.9` to `1`, accepts null and custom tags, and accepts the first of multiple documents. The option does not establish mdchk's schema. Validate the complete document count, node kinds, scalar spellings, string keys, aliases, duplicates, and option domains independently. Retain source locations and both key locations for duplicate diagnostics.

Use the explicit YAML restrictions in the user guide: a single UTF-8 mapping document; decimal integers; true/false booleans; no null, merge keys, custom tags, cyclic aliases, or external references. Accept ordinary presentation forms and acyclic same-document aliases. Validate an alias's resolved value without losing its use-site provenance. Do not enable the library's external file/directory reference helpers. Do not implement a private YAML grammar or assume that generic struct decoding provides strict schema validation.

Standalone JSON input uses the same semantic validator. Reject duplicate JSON names, invalid raw UTF-8, and unpaired Unicode surrogate escapes at the transport boundary before ordinary decoding; retain property paths and avoid scalar coercion. A valid literal U+FFFD remains valid. Neither serialization permits an unsupported field to disappear silently.

## Defaults and availability

Maintain metadata for all known catalog IDs separately from executable implementations. Metadata supplies defaults and option domains even for unavailable rules. Reject reserved IDs, aliases, unknown IDs, duplicate entries, and invalid options on disabled rules. Empty option mappings select defaults. Record whether a rule has an explicit entry so capability validation can distinguish requested behavior from unmentioned defaults.

An unavailable rule with an explicit entry and effective `enabled: true` is a configuration error. Explicitly disabled unavailable rules are valid after option validation. Unmentioned unavailable default-enabled rules remain enabled expectations and produce `rule-unavailable` coverage records during analysis; unavailable default-disabled rules do not. Evaluate dialect applicability separately: a rule inapplicable to the selected dialect does not reduce coverage. Preserve the catalog defaults rather than enabling only registered implementations.

Validate effective flavor and directive-processing availability before analysis. Default GFM never falls back to CommonMark. The shared CommonMark lint engine implements directive processing; adapters must declare that capability only when bound to the shared processor. Validate the approved `frontmatter` and `documents.slides` settings. Explicitly enabled recognition and enabled-rule options requiring unavailable metadata capabilities fail capability validation. See [initial capability handling](../configuration.md#initial-capability-handling).

Separate schema/policy validation from selected-operation capability validation. Ancestors consulted only for policy must be completely schema-valid, but their unselected rule options and flavor do not undergo execution-availability checks or become inherited settings. Each definition visited by `config validate` is checked as a selected definition using inherited permissions. Unmentioned unavailable default rules do not make that scan incomplete because it does not run Markdown checks.

## Shared resource policy

Accept optional top-level `resources` with only `index_file` and `anchor_style`, using the domains and defaults in the [configuration guide](../configuration.md#shared-resource-settings). Validate both even if no enabled rule consumes them. Reject the removed per-rule options `MD057.index_file`, `MD051.anchor_style`, and `MD080.anchor_style`, including on disabled rules. Keep schema version 1 and all catalog rule enablement defaults unchanged.

Track explicit shared settings for capability validation. Unsupported requested behavior is a configuration error; unavailable unmentioned defaults create coverage deficits only when applicable analysis requires them.

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

Accept one optional project-directory argument and implemented `--format text|json`, the shared repeatable `--limit name=value` option, plus global options. VS Code/GitHub output remains deferred and is rejected
before project access. Reject unsupported flags such as `--config` and `--dry-run`. An existing directory with no eligible configuration files is a successful empty scan, distinct from lint or fix selecting no Markdown files.

Use the shared result envelope with operation `config validate`, configuration paths in checked files, skip reasons, empty `diagnostics` and `coverage` lists, no changes, and categorized `errors`. Invalid definitions are configuration errors, not Markdown rule violations. A fully traversed scan can be complete even when definitions are invalid; unreadable files or directories and untrusted exclusions from invalid configs must be explained in the report. A valid config exclusion is intentional scope, not incomplete work.

Include optional paths and source ranges in error records. Implemented text
reports validation counts; JSON retains structured errors. The accepted future
VS Code reporter will format located errors as problem-matcher lines using
stable identifiers such as `config-definition`; the future GitHub reporter will
emit located error annotations. Location-free errors will remain stderr messages
in VS Code mode and annotations without source coordinates in GitHub mode.
Reporters must preserve the command's error categories and exit precedence from
the command guide.

## Verification

Cover multiple valid and invalid definitions, duplicate keys, unknown rules, invalid options and globs, unreadable files and directories, nested scope, ancestor pruning, self-exclusion, invalid-config exclusions, inherited exclusion locks, descendant attempts to reopen permission, explicit-config selection under a lock, discovery boundaries, Git negation and tracked-file behavior, symlinks, empty scans, and implemented text/JSON reporters. Compiled acceptance
tests exercise the production executable and shared result schema/semantic
validator, including real permission-denial probes, tracked Git setup, source
identity checks, finite quotas and reporting deadlines. Test environment Git
settings are isolated, and fixture hashes verify no filesystem writes.

Future VS Code/GitHub reporters require their own acceptance. Lint and config
validate use shared policy resolution without merging rule settings; fix, inspect
and MCP must preserve this behavior when implemented.

Use the [configuration examples](configuration-examples.md) as fixture inputs and expected outcomes. Include coercion traps, multiple documents, aliases, disabled-rule validation, explicit versus implicit unavailable rules, inherited false versus explicit true, and excluded explicit descendants. Verify incomplete analysis uses the shared [coverage result](../output-formats.md#incomplete-analysis) and exit 15 without turning configuration validation or inspect into lint operations.

The `config validate` command now calls `configscan.Validate` directly, preserving
source roles for configuration versus ignore/traversal failures. Its shared result
summary distinguishes completed validity decisions from interrupted acquired
files and retains actual scan completeness through bounded text/JSON reporting.
See the [command guide](../commands/config/validate.md) for enforced limits.

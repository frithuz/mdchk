# Explicit integration contexts

Issue #174 implements the shared context and policy boundary for the accepted
[integration contract](../../docs/engineering/integrations.md). It does not
advertise concrete MkDocs recognition to CLI, inspect or MCP. The existing
configuration capability checks remain in force.

`NewScope` borrows one existing resource Operation, its exact immutable View and
the adapter's explicit selected settings. `Operation.OwnsView` is an identity
check, and `View.TargetCount` permits charging an identity lookup without copying
targets. Neither creates resource, target, inventory or write authority. A
selected `Document` has private source/view proof; equal bytes from another Source
or another operation/view cannot borrow it. Unavailable Saved snapshots do not
fall back to originals. Scope creates no reader, worker, cache or new allowance.
Owner checks compare constant-size Source tokens rather than whole source byte
strings. Tokens remain identity only; they grant no parse or content admission.

Every working policy call shares the operation's work/node/time budget and caller
context. Independently observed caller cancellation, operation deadline and the
first actual quota remain inspectable together. A stopped context is unavailable,
never a clean missing nav/page result. Callers retain the returned actual
error alongside the typed status for the existing result/cause producer. This
package adds no result wire fields, diagnostic constructor or error exit class.

## MkDocs policy versus acquired context

`MkDocsPolicy.Enabled` is the effective MD074 enablement, not a second integration
switch. The pinned static reference is MkDocs 1.6.1. `ConfigPath` is a canonical
resource-root-relative key, default `mkdocs.yml`. `PlanDocsRoot` plans a literal
relative `docs_dir` beside that config, default `docs` when absent. Explicit empty
and absent values remain distinct. The result keeps the existing Root and grants
no directory existence or native access. Escapes and native/URL roots are rejected.

`LeadingSlash` applies only the extra integration rule: skip a single leading
slash by default; with `CheckAbsolute`, interpret it as a source path below the
planned docs root. Protocol-relative URLs remain untouched for shared remote
classification. This is not a second URL decoder. The #176 producer must then use
the shared resource normalizer for once decoding, query/fragment-independent
existence, blocked native/file paths, exact names, no-follow reads and the one
configured index. `WithinDocsRoot` enforces the lexical docs-subroot after that
normalization. It does not infer `site_url`, `use_directory_urls`, generated
directory URLs or `.html`-to-Markdown rewrites.

`Navigation` validates and snapshots a semantic policy tree with original config
definition/entry spans, not a Markdown Link or a claimed YAML parse. Absent/null
nav is automatic; a literal list, including `[]`, is manual; unsupported syntax
is unknown. Path strings, title/path records and nested titled sections preserve
source order. Copies charge actual shared work/nodes and reject foreign Source or
out-of-definition spans; cycles/depth cannot evade limits. Mutable inputs and
returned slices cannot modify the retained tree.

The concrete safe-YAML/config acquisition provider is #176. It must permit
unrelated ordinary settings and bounded acyclic aliases, retain exact spans, and
report duplicate keys/cycles/tags/includes/INHERIT/environment/dynamic contexts as
affected uncertainty without executing them. `StaticNavigation` describes plugin
policy: built-in search and literal `enabled:false` plugins do not introduce
dynamic navigation; other active plugins/hooks make dependent negative claims
uncertain. These pure policy inputs do not themselves prove YAML interpretation.

`EligibleInventory` has no public constructor or completeness setter. Complete
resource names, selected lint targets and queried-key caches cannot create it.
The #176 trusted producer establishes complete authorized **build-eligible** page
facts for the exact config Source/scope: included, excluded, draft, not-in-nav and
README/index conflicts under the pinned defaults and Gitignore-like MkDocs
patterns. The mdchk config pattern grammar is not MkDocs eligibility syntax.
`CanCheckOmissions` requires manual navigation, enabled omission policy, static
context and that exact complete inventory; partial or foreign inventories fail
closed. Automatic navigation has no manual omissions.

Until the real provider exists, enabled `Scope.MkDocs` is unavailable and disabled
is inactive. Standalone acquisition will use the same exact YAML file entry at
`resources[config_path]` plus docs/inventory in the existing resource namespace;
no competing context request, host fallback, network or new transport is added.

## Explicit slides

`Slides` annotates an exact selected Document using the existing configuration
pattern grammar, case-sensitive direct `MatchFirstBounded` semantics and the
selected-config-directory base (virtual root for standalone). The trusted adapter
supplies the normalized logical name and established containment; this mapping
grants no filesystem authority. A directory-only pattern does not annotate all
descendants through exclusion-ancestor semantics. Matching is one bounded scan
over the complete pattern list, including actual visited memo-row allocations.

The deterministic first matching pattern and copied configuration Origin are
retained. Empty/no-match/outside-base is an ordinary selected document; invalid
patterns remain configuration errors; unavailable Saved data or actual stops are
unavailable, not ordinary. Filename conventions, frontmatter and
separators do not implicitly classify slides. Annotation changes no selection,
flavor, frontmatter, integration or rule setting. MD090 and actual inspect/CLI/MCP
presentation remain later consumers.

No production MD074/MD090 callbacks, findings, fixes, TOC, transport or
writer changes are implemented here. #176 still supplies the MkDocs provider;
later #177 must prove complete consumer journeys under these shared contracts.

## Bounded MkDocs producer

`Scope.WithMkDocs` immutably binds the existing operation/view to its registered
Service. `Scope.MkDocs` acquires the literal configured resource key even with no
selected Markdown targets. MD074 enablement is the sole activation. Config uses
TextContent admission, with no Markdown parse, transport field, host fallback,
plugin execution or new worker. The resulting context retains the exact acquired
Source, original config/nav spans, docs-root plan, ordered entries and independent
existence observations. Caller-owned returned errors retain actual I/O and all
observed controls; source-bound interpretation problems remain typed context.
`KnownTarget`, `KnownPage` and name-inventory accessors inspect retained immutable
facts after stop without starting analysis. Collection copies remain budgeted.

Full configuration YAML uses a bounded lexical/source adapter with a direct
original-byte map.
CR/CRLF normalization is mapped back to original bytes; standard tags, quoted
payloads, aliases and Unicode locations retain their authored ranges. A narrow
numeric-escape check rejects malformed hex digits accepted by the pinned lexer.
Other integration files and the Markdown parser remain denied YAML imports.

Missing/null nav is automatic; literal `[]` is manually empty. This is an
intentional project override of MkDocs 1.6.1's empty-list coercion. Literal relative
docs_dir is confined below the original resource root and never URL-decoded.
Nav targets reuse the shared normalizer once, with a docs-subroot guard, explicit
virtual-directory representation and exactly one configured index lookup.
Existence accepts any regular file, including binary assets; fragment validity
never changes that separate fact. No deployed-URL or generated-HTML inference is
performed.

For manual omission analysis the producer enumerates authorized names through the
registered adapter, then derives private config/source/view-bound eligibility.
Partial inputs, unavailable branches, unsupported plugins, acquisition instability
and stops withhold completeness while preserving known names and positive targets.
Lint selection never supplies site inventory. Pinned eligibility uses MkDocs
1.6.1's five case-sensitive documentation suffixes, defaults, exclude/draft/
not-in-nav priority, and destination conflicts after inclusion. Patterns compile
once with Unicode RE2 and GitIgnoreSpec match priority, without Git parent pruning.
The reproducible compatibility baseline is Python pathspec 0.11.1 (MkDocs declares
`pathspec>=0.11.1`, rather than pinning a transitive version). Runtime uses no Python.
These typed producers do not advertise CLI/MCP capability or implement MD074's
later rule callback, findings or correction behavior.

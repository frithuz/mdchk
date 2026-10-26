# Integration and document-context contract

Status: accepted on 2026-10-15; implementation and end-to-end verification remain pending.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

The shared typed context/policy boundary is implemented under
[internal/integration](../../internal/integration/README.md). Its contract tests
cover source/view ownership, static navigation/omission prerequisites and explicit
slide annotation. Policy decisions do not establish acquired context. Bounded
MkDocs acquisition/eligibility is implemented by the bound context provider.
MD090 consumes explicit slides in production lint, fix candidate analysis and
inspection. MkDocs rule consumers and MCP end-to-end verification remain separate work.
Interface availability alone does not
advertise those runtime capabilities.

## MkDocs navigation

### Activation

Enable MD074 explicitly in mdchk configuration; that is the integration opt-in, without a second
enabled switch. Keep config_path default mkdocs.yml. Resolve config_path as a canonical path
relative to the operation resource root, not the current Markdown document. Analyze one configured
MkDocs project per operation.

### Source

Use MkDocs 1.6.1 as the built-in static navigation reference. Parse one bounded YAML mapping,
accepting ordinary safe data and acyclic bounded aliases, preserving source spans, rejecting
duplicate keys and cyclic structures. Accept nav lists containing path strings, title-to-path
entries and nested titled sections. Allow unrelated ordinary settings without treating this as full
MkDocs configuration validation. docs_dir defaults to docs relative to the MkDocs config; initially
support relative docs_dir only, resolved within authorized resource access.

### Local targets

Resolve relative nav targets from docs_dir and confine them to that subtree, with shared decoding,
exact-case names, symlink restrictions and resources.index_file. Strip query/fragment for existence
checks; MD074 does not add fragment validation. Existing files pass existence regardless of Markdown
suffix. Distinguish known missing files from inaccessible or unknown resources. Remote and
protocol-relative URLs are skipped without network access.

### Absolute

check_absolute remains false by default and skips leading-slash nav destinations. When true, a
leading slash means a source path under docs_dir, never a host filesystem root or deployed website.
Do not infer mappings from site_url or use_directory_urls; do not reverse .html or generated
directory URLs to Markdown sources. Native absolute paths and file URLs stay blocked under the
resource contract.

### Omissions

check_omitted remains false by default. When enabled, require a complete authorized inventory of
eligible pages, applying MkDocs 1.6.1 build-time page eligibility and exclude_docs, draft_docs and
not_in_nav behavior, including built-in exclusions and recognized index/README conflicts. mdchk
target exclusions do not define the site inventory. Missing or null nav selects automatic navigation
and has no manual omissions; explicit [] is an empty manual navigation. Partial inventories or
unsupported page generation yield incomplete coverage rather than omission findings.

### Unsupported

Do not execute plugins, hooks, includes, inherited configuration, environment expansion or custom
YAML tags. INHERIT, custom tags and unresolved dynamic navigation produce incomplete affected
coverage. Initially only the built-in search plugin is known not to require navigation projection;
other active plugins/hooks make dependent absence/omission claims uncertain, while independently
verified facts may be retained. Plugins explicitly disabled by a literal enabled:false do not cause
dynamic-context uncertainty.

### Standalone

Supply the exact MkDocs YAML as a file entry in the existing resources map at config_path, plus docs
entries and authoritative inventory when needed. Interpret the same relative paths as project mode;
no host lookup or separate competing context representation.

### Reporting

Run once per operation as a project-level check. Attribute findings to original navigation entries
and omissions to the nav definition (config root when absent), identifying the omitted path.
Markdown inline directives cannot activate or suppress project-configuration findings; issue #118
and catalog directive applicability must state this exception. All findings are report-only; no
creation of files or navigation entries. Malformed/unavailable inputs report located problems and
affected incomplete coverage; exact error envelope is resolved in the results/errors area.

## Slide documents

### Configuration

#### Documents

##### Slides


### Declaration

Declare slide documents with documents.slides, a list of path patterns, default empty. Example:
documents.slides: ["slides/**", "**/*.slides.md"]. Use ["**"] to classify all documents in scope as
slides.

### Matching

Reuse the existing exclusion-pattern grammar, case-sensitive matching and selected-config-directory
base; standalone uses its virtual request root. Apply only the selected configuration, with no
nested overrides. Any matching pattern marks a selected file as slides; no match is ordinary
document context. Invalid patterns are configuration errors.

### Scope

Patterns annotate document context only. They do not select or exclude files, change Markdown
flavor, activate integrations/frontmatter, or enable rules. Do not infer slide context from
frontmatter fields, separators, or filename conventions unless the filename
matches an explicit configured pattern.

### Rule

Keep MD090 disabled by default and report-only. When enabled and allow_slides=true, matching slide
documents are exempt. When allow_slides=false, apply the ordinary thematic-break-before-heading
check to every selected document, including slides. Unmatched documents always use ordinary MD090
behavior. Intentional exemptions do not incur incomplete coverage.

### Parity

Use the same configuration and classification across CLI, inspect, engine and MCP. Inspect exposes
the effective context and matched pattern. No separate request field or in-document marker is
introduced.


### Delivered slide consumer

MD090 analysis is implemented for CommonMark and GFM. The immutable selected policy
uses direct bounded first-pattern matching for each selected input path, relative
to the selected configuration directory. It stays on the execution plan for original,
candidate and saved-view analyses; no per-plan global slide flag is shared by a batch.
Known matched slides with `allow_slides: true` make MD090 inapplicable before its
syntax prerequisite, without erasing independent parser or directive failures.
Inspection publishes the known annotation and first-match origin using the existing
version1 document-context section. Existing scoped integration document/view/Saved
ownership remains separate from the shared immutable matching policy. No resource
service, new parse, target selection, metadata interpretation or MCP transport is
introduced by annotation.

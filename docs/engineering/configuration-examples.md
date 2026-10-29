# Configuration examples and fixture cases

These cases specify inputs and expected outcomes for the approved
[configuration contract](../configuration.md). They are examples for loader and
integration fixtures. Strict YAML decoding, semantic validation, bounded
filesystem discovery, ancestor permissions, and compiled lint integration are
implemented. Use the complete known rule catalog in every case, independently
of the executable implementations. Read-only project configuration validation
and all four reporters are implemented. Inspect supports text/JSON; fix supports
previews and verified Darwin arm64+cgo/local APFS application of MD047, with
protected EOF deferrals and unsupported-adapter refusal. MCP remains planned.
Cases below involving unavailable rules, dialects, contexts or MCP specify the
accepted future contract.

Unless a case specifies otherwise, assume the selected flavor, directive
processor, and applicable rules are implemented, no ancestor restricts the
definition, and all configuration paths are inside the discovery boundary. These
conditions describe a test environment, not the current implementation status.

## Valid definitions

### Explicit project settings

```yaml
version: 1
flavor: gfm
exclude:
  - 'vendor/**'
  - 'docs/generated/**'
rules:
  MD013:
    enabled: true
    length: 100
  MD004:
    marker: dash
```

Expected: schema-valid, with GFM, a line-length limit of 100, and dash bullets.
MD004 keeps its default enabled state. Both exclusions are relative to this
definition's directory.

### Comments flow collections and aliases

```yaml
# Reuse a valid disabled-rule setting within this document.
version: 1
rules:
  MD001: &disabled {enabled: false}
  MD013: *disabled
```

Expected: both rules disabled. The alias does not create external file access or
merge keys. It is validated against MD013's option schema at its use site.

### Empty mappings preserve defaults

```yaml
version: 1
rules:
  MD001: {}
exclude: []
```

Expected: MD001 is explicitly configured and remains enabled with its catalog
options. If MD001 is unavailable, this same definition fails capability validation;
an empty entry does not count as an unmentioned rule.

By contrast, `MD013: {}` keeps that rule's default disabled state and is valid
even if MD013 is unavailable. Supplying only `length: 100` also leaves it disabled.

### Explicitly disabled unavailable rule

```yaml
version: 1
rules:
  MD013:
    enabled: false
    length: 100
```

Expected even when MD013 has no implementation: validate its option types and
range, retain the settings, and impose no coverage penalty for that disabled check.

### Usable early CommonMark settings

```yaml
version: 1
flavor: commonmark
```

This configuration works with the current production executable: CommonMark
0.31.2, GFM, directive processing, MD001/MD003/MD004/MD005/MD007/MD029/MD030/MD032/MD069/MD076/MD077/MD009/MD010/MD012/MD013/MD027/MD028/MD064/MD085/MD089/MD035/MD037/MD038/MD018/MD019/MD020/MD021/MD022/MD023/MD024/MD025/MD026/MD036/MD041/MD043/MD063/MD080/MD082/MD090/MD093 analysis and MD047 analysis/correction
are available. MD013, MD026, MD036, MD043, MD063, MD080, MD082, MD085, MD089, MD090 and MD093 remain disabled by default. MD001, MD025 and MD041 also support opt-in YAML/TOML title consumption with
independently enabled header recognition. Other ordinary rule implementations
remain unavailable. Lint parses
selected valid UTF-8 sources, reports applicable missing default-enabled checks
as `rule-unavailable`, sets `status: incomplete` and `complete: false`, and exits
15. The complete 87-rule catalog remains intact: MD001 is default-enabled and
executes along with MD004, MD005, MD007, MD029, MD030, MD032, MD069, MD076, MD077, MD009, MD010, MD012, MD027, MD028 and MD064, while unavailable default-enabled MD011 contributes
coverage. MD013 is available but default-disabled and does not incur a coverage penalty. Explicitly enabling
an unavailable rule instead fails configuration admission with exit 12 before
target processing. Text and JSON retain the same coverage and outcome.

For a hypothetical fixture with available ordinary checks except MD001, the same
configuration reports only MD001 as unavailable and preserves any available
findings. Test-only registrations exercise this behavior without changing
production defaults or shipping placeholder checks. Disabling all ordinary
checks can yield clean exit 0 for valid UTF-8; mandatory decoding still applies.

The implemented config validation scan does not execute MD001 and succeeds for
this definition with exit 0. Its independent project scan checks every eligible
definition rather than selecting the single definition used by lint. Delivered
inspect reports missing implementations as observations while still exiting 0.
For a current complete reduced selection, use the
[published MD047-only configuration](../examples/editor-ci/.mdchk.yaml); see the
[quick start](../index.md#get-started-with-this-build) for preview and saved results.
That selection is not full Markdown conformance.

## Invalid definitions

Each table row is an independent complete YAML input written as a flow mapping.
Expect a located configuration error before Markdown processing or writes. Types
must be checked from source nodes, without relying on struct-decoder coercion.

| Case | YAML input | Expected error |
| --- | --- | --- |
| Missing version | `{rules: {}}` | Required version missing |
| Unsupported version | `{version: 2}` | Unsupported schema version |
| Quoted version | `{version: '1'}` | String instead of integer |
| Fractional version | `{version: 1.9}` | Fractional number instead of integer; never truncate |
| Leading-zero integer | `{version: 01}` | Noncanonical integer notation |
| Alternate-base integer | `{version: 0x1}` | Nondecimal integer notation |
| Null setting | `{version: 1, exclude: null}` | Null where a sequence is required |
| Numeric string coercion | `{version: 1, flavor: 123}` | Number where a string is required |
| Wrong key case | `{Version: 1}` | Unknown key and missing required version |
| Unknown key | `{version: 1, flavour: gfm}` | Unknown key |
| Alias name | `{version: 1, rules: {final-newline: {}}}` | Unsupported descriptive rule alias |
| Lowercase ID | `{version: 1, rules: {md013: {}}}` | Invalid canonical ID spelling |
| Reserved ID | `{version: 1, rules: {MD002: {}}}` | Reserved rule ID |
| Unknown ID | `{version: 1, rules: {MD999: {}}}` | Unknown rule ID |
| Null rule | `{version: 1, rules: {MD013: null}}` | Expected an option mapping |
| Quoted integer option | `{version: 1, rules: {MD013: {length: '100'}}}` | String instead of integer |
| Out-of-range option | `{version: 1, rules: {MD013: {length: 0}}}` | Invalid length range |
| Quoted boolean | `{version: 1, rules: {MD013: {enabled: 'true'}}}` | String instead of boolean |
| Legacy boolean | `{version: 1, rules: {MD013: {enabled: on}}}` | Invalid boolean value |
| Invalid disabled rule | `{version: 1, rules: {MD013: {enabled: false, lenght: 100}}}` | Unknown option despite disabled rule |
| Escaping pattern base | `{version: 1, exclude: ['../**']}` | Parent-relative exclusion prohibited |
| Unsupported negation | `{version: 1, exclude: ['!README.md']}` | Negation unsupported |
| Embedded globstar | `{version: 1, exclude: ['docs/ab**cd']}` | Globstar must occupy a whole segment |
| Wrong frontmatter type | `{version: 1, frontmatter: true}` | Boolean instead of the required frontmatter mapping |

The validator need not aggregate every error in one invalid definition. When
multiple problems exist, at least one precise error must explain rejection;
duplicate-key diagnostics identify both occurrences.

### Duplicate keys

```yaml
version: 1
rules:
  MD013: {length: 100}
  MD013: {length: 100}
```

Expected: duplicate rule key at line 4, referencing line 3. Identical values do not
make a duplicate valid. Also cover duplicate top-level keys and duplicate option
keys within one rule mapping.

### Multiple documents

```yaml
version: 1
---
version: 1
```

Expected: extra document error. Successfully decoding only the first document is
insufficient. Empty files, null documents, and sequence roots are also invalid.

### Merge keys

```yaml
version: 1
rules:
  MD001: &base {enabled: false}
  MD013:
    <<: *base
```

Expected: unsupported merge key at line 5. A plain alias to a valid mapping is
supported; merging mappings is outside the configuration schema. Also reject
custom tags, alias cycles, unresolved aliases, and non-string mapping keys.

## Capability cases

Schema validity alone does not imply this build can honor a selected definition.
Production CommonMark lint now supplies the directive processor; the unavailable
processor rows describe capability-bound adapter fixtures.

| Definition or condition | Capability setup | Expected outcome |
| --- | --- | --- |
| `version: 1`, flavor omitted | Production GFM available | Real GFM analysis; missing default rules yield incomplete/15 |
| `version: 1`, flavor omitted | Build without GFM | Configuration error; no CommonMark fallback |
| Global directives effectively true | Processor unavailable | Configuration error |
| Child directives true beneath valid ancestor false | Processor unavailable | Effective false; no processor capability error |
| Explicit MD001 entry, enabled or default-enabled | MD001 unavailable | Configuration error |
| Explicit MD013 entry with `enabled: true` | MD013 unavailable | Configuration error |
| Explicit MD013 entry without `enabled` | MD013 unavailable | Remains default-disabled, without coverage penalty |
| No MD001 entry | MD001 unavailable and applicable | Lint/fix incomplete with exit 15 |
| Unmentioned default-disabled rule | Implementation unavailable | Inactive without coverage penalty |
| Unmentioned footnote rules in a dialect without footnotes | Extension inapplicable | No footnote coverage penalty |
| Explicit metadata-dependent option on an enabled rule | Required metadata interpretation unavailable | Configuration error |
| Enabled applicable resource-dependent check | Context unavailable | Analysis incomplete with `context-unavailable` |
| A known unavailable rule enabled by a permitted source directive | Directive processor available | Retain state transition and report `rule-unavailable` for required coverage |

Keep operational errors separate. A file read failure takes exit 13 ahead of
incomplete analysis; an internal failure takes 14 ahead of both. Coverage deficits
take 15 ahead of ordinary violations (10). Config validation does not use 15 for
missing implicit default implementations, and completed inspect does not use 15.

## Patterns discovery and policies

Use explicit fixture roots and case-independent names so host case behavior
does not change expectations. The compiled lint acceptance suite uses real
temporary project trees, including worktree markers and symlinks. Lower-level
provider tests inject filesystem views for deterministic access failures.

| Fixture | Expected outcome |
| --- | --- |
| `vendor` versus `vendor/a.md` and `pkg/vendor/a.md` | First excluded through its ancestor directory; second eligible |
| `**/vendor/` with root and nested vendor directories | Both subtrees excluded, including explicit targets |
| `docs/generated/**` versus the directory itself | Directory pruned, including an empty directory |
| `**/*.md` with root and nested lowercase extensions | Both match; zero-segment globstar works |
| Same pattern with `README.MD` | Pattern does not match, but directory discovery still selects the file |
| `.github/guide.md` without exclusions | Selected; hidden directories are included |
| Explicit regular file `notes.txt` | Eligible despite extension, subject to exclusions |
| Explicit target outside config directory | Config-relative patterns do not match it |
| Symlink target in lint/fix discovery | Skipped; no traversal through it |
| Explicit config outside enclosing repository | Configuration error; no widened boundary |
| Explicit config inside a nested separate repository | Configuration error for crossing repository scope |
| Config reached through a symlink | Configuration error |
| Relative `--config docs/custom.yaml` | Resolved from working directory; same YAML parser |
| Invalid nearest config, valid ancestor | Configuration error; no fallback |
| Invalid ancestor schema, valid selected child | Configuration error before trusting ancestor permissions |
| Schema-valid ancestor selecting an unavailable dialect, supported selected child | Ancestor policy still applies; unselected dialect is not executed or inherited |
| Parent exclusion permission false, child true and no exclusions | Valid; effective permission remains false |
| Same parent, child adds nonempty exclusions | Configuration error identifying the ancestor |
| Parent per-rule directive permission false, child true | Valid; effective permission remains false with provenance |
| Selected child changes enabled state or rule options | Settings come from child and built-ins, independently of ancestor rule settings |
| Worktree with a `.git` file | Discovery stops at the worktree boundary |
| Project MCP invocation | Discovery and ancestor reads stay inside startup root |
| Standalone request | No filesystem configuration or ancestor lookup |
| Validation scan starts below a parent config | Does not read that parent |
| Invalid config with exclusions during validation scan | Report it and continue with valid inherited policies; apply none of its exclusions |

For glob grammar, also cover classes, escaped metacharacters, trailing slashes,
malformed classes, repeated separators, drive/UNC paths, absolute patterns, and
backslashes as escapes rather than Windows separators. Compare Git ignore
fixtures separately against Git semantics; the mdchk matcher is not a Git ignore
implementation.

## Standalone parity

The same semantic validator accepts this object when its requested capabilities
are available:

```json
{
  "version": 1,
  "flavor": "commonmark",
  "directives": {"enabled": false},
  "rules": {"MD013": {"enabled": true, "length": 100}}
}
```

Reject duplicate JSON names before map construction, null values, unknown fields,
incorrect types, and invalid option values. Match YAML outcomes for equivalent
values. Errors identify property paths when byte/line locations are unavailable.
No submitted name or setting causes a server-local configuration read.

## Shared resource policy

This YAML and JSON pair select the same approved settings once their capabilities
are implemented. All other rule defaults remain unchanged.

```yaml
version: 1
flavor: gfm
resources:
  index_file: README.md
  anchor_style: explicit
rules:
  MD051:
    cross_file: true
```

```json
{
  "version": 1,
  "flavor": "gfm",
  "resources": {"index_file": "README.md", "anchor_style": "explicit"},
  "rules": {"MD051": {"cross_file": true}}
}
```

| Input or condition | Expected outcome |
| --- | --- |
| Omitted `resources`, or `resources: {}` | `index_file: index.md`, `anchor_style: github` |
| `resources: {anchor_style: explicit}` | Available for same-document MD080; MD051/MD073 and referenced Markdown consumption remain separate deliveries |
| `resources: {index_file: README.md}` | Current CLI rejects unavailable index handling; the future directory-lookup contract tries that one filename once |
| `resources: null`, a sequence, or an unknown resource field | Configuration error |
| `resources: {index_file: docs/index.md}` | Configuration error: filename only |
| Empty index filename, `.`/`..`, a glob, URL, query/fragment, drive prefix, backslash, or control character | Configuration error |
| `resources: {anchor_style: renderer}` | Configuration error: only `github` or `explicit` |
| `rules: {MD051: {anchor_style: github}}` | Configuration error: removed rule option |
| `rules: {MD080: {enabled: false, anchor_style: explicit}}` | Configuration error even though disabled |
| `rules: {MD057: {index_file: index.md}}` | Configuration error: removed rule option |
| Explicit shared resource setting with required implementation unavailable | Configuration error before analysis |
| Unmentioned default resource behavior unavailable for an applicable check | Incomplete coverage when required, not a false clean result |
| An ancestor sets different shared resource options | Selected definition and built-ins supply policy; no ancestor option merge |

Validate standalone JSON before ordinary decoding: reject duplicate names,
malformed raw UTF-8, and unpaired surrogate escapes. A valid literal U+FFFD is
accepted. Request-level `resources` is a separate map schema, not a configuration
option or permission to read host files. Its validation and parity cases are in
the [resource contract](resources.md#verification-and-handoff).

## Optional context schema cases

The [optional context settings](../configuration.md#optional-document-contexts)
are schema-1 fields. Use the complete default object there as a valid fixture.
Also accept `frontmatter: {enabled: true, formats: [yaml]}`,
and `documents: {slides: ["slides/**"]}` when required capabilities are available.

Reject string booleans, `frontmatter.formats: [json]`, empty/duplicate format
lists, unknown settings, malformed slide patterns, and null mappings.
Reject `rules.MD088.replacements` even when MD088 is disabled; its mapping is fixed.
Reject `limits` or HTTP authentication fields in project/supplied configuration.
Capability tests must distinguish accepted schema from an unavailable implementation.


MD082 is available, report-only and disabled by default. Its only rule-specific
option is a strict boolean:

```yaml
rules:
  MD082:
    enabled: true
    allow_final_section: false
```

`true` exempts only the last actual heading per document, including hidden and
container headings in the context. Null, strings, numbers and unknown options are
configuration errors even when MD082 is disabled. Anchor policy is unrelated.


MD090 is available, report-only and disabled by default. Slide annotation alone
never enables it:

```yaml
documents:
  slides: ["talks/**", "**/*.slides.md"]
rules:
  MD090:
    enabled: true
    allow_slides: true
```

Set `allow_slides: false` to check declared slides normally. Patterns use the
selected configuration directory and direct matching; no syntax infers slides.
Invalid later patterns, nulls and nonboolean options are errors even while the
rule is disabled and before target access. Inspection identifies the first match.

## Inline formatting in headings

MD093 is available, report-only and disabled by default. Each nested construct is
checked independently: this example permits links while still reporting strong
emphasis in a link label.

```yaml
version: 1
rules:
  MD093:
    enabled: true
    allowed_constructs: [link]
```

The exact choices are `emphasis`, `strong`, `code`, `link` and `image`; an empty list
permits none. Null, unknown and non-string values are rejected even when disabled.

## Bullet marker style

MD004 is available and enabled by default. Its `marker` option is `consistent`
(default), `asterisk`, `plus` or `dash`. The first actual bullet chooses one style
for the whole document, including nested and directive-suppressed items. Explicit
modes check every actual bullet. Findings select only the parsed marker byte.
Planned correction metadata does not grant edits: analysis remains manual.

## Sibling list alignment

MD005 is available and enabled by default, with no rule-specific options. Each
actual list group uses the first direct item's structural marker indentation as
its baseline, including hidden original items. Nested groups retain independent
baselines. Ordered width and padding changes do not shift aligned marker starts.
Findings select the entire original bullet or ordered marker and remain manual;
PlannedFix metadata grants no edits or previews.

MD007 is available and enabled by default. `indent: 2` is a positive integer
and `start_indented: false` is a strict boolean. Root bullet indentation must
equal zero, or exactly indent with start_indented. Bullet children use the
actual parent marker width and padding; numbered parents target the content
column without additional indent. Quotes retain ancestry after stripping their
prefix. MD007 findings are manual, and PlannedFix describes future work.

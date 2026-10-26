# Frontmatter and metadata contract

Status: accepted on 2026-10-15. Opaque recognition, shared protection and source
inspection and on-demand structured metadata interpretation are implemented;
MD001, MD025 and MD041 opt-in title consumption is implemented; broader production metadata-rule
integration and corrections remain pending. The
cross-operation recognition matrix is verified for both
supported flavors; see [operation parity fixtures](../../internal/testutil/frontmatter/README.md).
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

## Frontmatter recognition

Recognition defaults to disabled, independently of Markdown flavor. Configure:

```yaml
frontmatter:
  enabled: false
  formats: [yaml, toml]
```

JSON recognition is deferred. The configuration guide defines strict field types.

### Start

First line only, after an optional preserved UTF-8 BOM; no preceding blanks or comments.
Only one BOM may precede the opener. A second BOM, indentation, container prefix,
or any other preceding text prevents recognition.

### Delimiters

YAML opens --- and closes --- or ...; TOML opens/closes +++; unindented delimiter-only lines,
optional trailing spaces/tabs.
The marker must contain exactly three identical ASCII characters. Comments, extra
marker characters and non-ASCII whitespace invalidate a delimiter. The first
subsequent matching closer ends the header; another format's marker is content.
Recognition is lexical: malformed or quoted metadata does not change delimiter
matching. YAML `...` cannot open a header.

Physical lines use LF, CRLF or CR, including mixed endings. EOF can terminate a
delimiter line without a final newline. A recognized region includes the optional
BOM, opener line, payload and closer line through its original ending; the body
starts immediately afterward. Marker spans omit the BOM and line endings but
retain trailing spaces/tabs. Payload spans exclude both delimiter lines.

### Empty header

Recognized; original bytes preserved.

### Protection

Header remains opaque to ordinary Markdown rules and directives. Shared analysis
filters ordinary findings located within or crossing the header. Explicit
source-metadata rule access is opt-in and does not grant decoding or edits.
The entire header envelope, including BOM and delimiter endings, is protected.
A physical-file MD047 observation at a complete header-only EOF can remain
report-only; an insertion touching that opaque EOF is ineligible. Body fixes
retain the selected recognition settings through candidate and saved reparses
and independently preserve header bytes, format and delimiter binding.

### Unterminated

Protect from recognized opener to EOF, report incomplete analysis, and prohibit edits in that
region.
An opener alone at EOF is unterminated; a subsequent closer with no intervening
payload is an empty complete header. Invalid openers and unselected formats use
ordinary Markdown interpretation. Disabled recognition does the same for both
CommonMark and GFM. Selecting a flavor never enables recognition.

### Boundary verification

`internal/parser/testdata/frontmatter/boundaries.json` records original byte
ranges for absent, complete and unterminated lexical headers. It covers both
formats, format selection, disabled recognition, BOM/start restrictions, exact
markers, physical endings/EOF, empty/malformed payloads and deferred JSON.
Parser tests exercise the matrix alongside both supported flavors; configuration
tests verify independent defaults and selection across all acquisition paths.
`ParseWithOptions` now uses the boundary helper before body recognition, retaining
an immutable typed `Document.Frontmatter` record outside the ordinary body AST.
The full root/source and body coordinates retain the original snapshot. Enabled
entry-point tests reuse this matrix and check body/reference/comment isolation,
selected flavor, ownership, cancellation and limits. Unterminated records remain
available with explicit incomplete uncertainty, without a completed-parse handoff.
Shared CLI lint/fix/inspect preserve the same independently selected recognition
settings. Inspection exposes source records separately from the body AST, and
incidental edits cannot alter header bytes or recognition. On-demand structured
metadata interpretation is available through the separately bound decoder; the broader opaque operation matrix is verified by #169.

## Metadata values and corrections


The source-owned model, title/ordering policy and necessary preservation gates
are implemented in [internal/metadata](../../internal/metadata/README.md). Concrete
format decoding/source projection (#179) is implemented. MD001, MD025 and MD041 consume title
policy through narrow engine-owned access under the original
parse budget and operation. It retains body-only finding protection; semantic
title access does not grant UsesFrontmatter header-observation authority. Broader
production rule integration (#180) and metadata correction authorization (#181)
remain separate
work. Model records and
eligibility never bypass ordinary opaque-header protection.

### Parser strategy

Use YAML 1.2 Core interpretation through the existing exact pinned
`github.com/goccy/go-yaml v1.19.2` token/AST adapter. Reuse/factor the bounded lexical
guards and original-source mapping, without importing integration policy or the
closed mdchk configuration schema. Standard Core tags may establish their native
kind only when valid; merge keys, custom tags and cycles remain unsupported.
Aliases are bounded and acyclic, with diagnostic ranges at their authored use.

Use TOML 1.1 through `github.com/pelletier/go-toml/v2 v2.4.3`, behind a project-owned
source-preserving boundary. The module is admitted by the #179 producer through
a narrow metadata adapter boundary, with its checksum and MIT license retained
in the repository. Its unstable expression AST exposes
raw ranges and optional comments; copy validated records before advancing the
parser, and expose no upstream types to consumers. Add the exact module checksum,
license and narrow architecture boundary with implementation. The
[pinned parser](https://github.com/pelletier/go-toml/blob/v2.4.3/unstable/parser.go)
and [AST](https://github.com/pelletier/go-toml/blob/v2.4.3/unstable/ast.go) are the
adapter reference. This patched version is selected over the versions affected
by the [maintainer recursion advisory](https://github.com/pelletier/go-toml/security/advisories/GHSA-frc4-6h9q-39fq).

For both adapters, before recursive dependency entry reserve bytes, temporary
storage and work and enforce the tighter shared syntax-depth limit. The TOML
library's 10,000 nesting cap does not replace mdchk's limit. Charge subsequent
projection, copies, aliases, key comparisons and retained records to the original
operation and document counters. Apply caller/operation/elapsed-time checkpoints;
actual controls stay canonical errors rather than malformed metadata. No panic
recovery or after-the-fact depth check substitutes for bounded entry. Extend the
parser accounting/property fixtures when #179 introduces actual adapter work.

TOML table paths need semantic validation beyond token scanning: enforce exact
decoded-key duplicate/redefinition rules, scalar-versus-table conflicts, implicit
and explicit table ownership and array-of-table identity. Preserve native integer,
float, Boolean, offset-date-time, local-date-time, local-date and local-time kinds.
Do not normalize local values through a server timezone. Use the
[TOML 1.1 specification](https://toml.io/en/v1.1.0) for grammar fixtures. JSON
frontmatter remains deferred. Dependency upgrades require the same source-range,
native-value, bounded-entry and semantic-validation probes.

### Acquisition

Decode recognized YAML/TOML only when metadata-aware rules or inspection need structured values.
Permit arbitrary metadata field names; do not apply the mdchk configuration schema.

### Model

Root mapping with string keys, ordinary format-native values, nested mappings and sequences. Empty
headers count as empty mappings. Reject duplicate keys; do not coerce values to strings.

### Invalid or unsupported

Report a located problem, keep the header protected, mark affected metadata-dependent analysis
incomplete, and continue reliable Markdown-body analysis. No guessed missing-title or sorted-key
conclusion when required metadata is unavailable.

### Title

Only the exact top-level title key with a decoded nonblank string counts. Blank
means only Unicode White_Space (`unicode.IsSpace` in the pinned Go toolchain);
retain the decoded string without trimming. Missing, blank or
non-string title values do not count. The existing frontmatter_title options remain opt-in; MD001
uses a virtual H1, MD025 counts one title at its configured title level, and MD041 accepts the title
as satisfying its existing opening-heading requirement.

### Ordering

Retain current recursive=false and case_sensitive=true defaults. Compare decoded keys in Unicode
code-point order; case-insensitive mode uses Unicode 17.0.0 full default C/F case folding,
without whitespace/NUL transformation or normalization, retaining source order
for equal folded keys. Recursive mode checks nested mappings, including mappings inside sequences, without
reordering sequence elements.

### Yaml features

Allow bounded acyclic aliases. Treat merge keys, custom tags and cycles as unsupported for semantic
metadata analysis, with incomplete affected coverage. No includes or execution.

### Corrections

Reorder only complete independent entries, preserving exact value spelling, quoting, line endings
and inline comments. Initially leave affected blocks containing standalone comments or YAML
anchors/aliases report-only. Never move TOML table declarations, dotted-key assignments or
array-of-table elements. Reparse candidates to verify unchanged values and sequence order; reliable
ordering violations may still be reported where automatic correction is unavailable.

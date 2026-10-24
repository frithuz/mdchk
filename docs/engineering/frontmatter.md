# Frontmatter and metadata contract

Status: accepted on 2026-10-15. Opaque recognition, shared protection and source
inspection are implemented; structured metadata interpretation and corrections
remain pending. The cross-operation recognition matrix is verified for both
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
incidental edits cannot alter header bytes or recognition. Structured metadata
interpretation remains unavailable; the broader operation matrix is verified by #169.

## Metadata values and corrections

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

Only the exact top-level title key with a decoded nonblank string counts. Missing, blank or
non-string title values do not count. The existing frontmatter_title options remain opt-in; MD001
uses a virtual H1, MD025 counts one title at its configured title level, and MD041 accepts the title
as satisfying its existing opening-heading requirement.

### Ordering

Retain current recursive=false and case_sensitive=true defaults. Compare decoded keys in Unicode
code-point order; case-insensitive mode uses Unicode case folding, retaining source order for equal
folded keys. Recursive mode checks nested mappings, including mappings inside sequences, without
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

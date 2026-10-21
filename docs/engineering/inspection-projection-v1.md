# Inspection projection version 1

`internal/inspect.Analyze` consumes the immutable `Acquire` snapshot. It never
reopens the target or configuration. Its result and typed projection are owned
values; mutating nested properties, options, intervals or provenance does not
change the snapshot or another analysis. Concurrent snapshot readers may share
the operation meter. The same meter's remaining work, construction and elapsed
allowances cover acquisition, configuration, parsing, rule execution, optional
observation and projection. Analysis never creates a fresh execution budget.

The admitted configuration plan calls the shared engine's `Inspect` method.
Ordinary `Lint` retains its existing behavior. Inspection records invocation and
completion at the actual callback, independently of whether findings exist.
It materializes source projections and reserves bounded trace publication before
callbacks run, so cancellation or exhaustion preserves completed AST, rule-state
and diagnostic observations. Interruption before a section is constructed makes
that section unavailable, with reasons and links to canonical causes. A section
that is successfully constructed can remain available while dependent execution
is incomplete. Observation errors remain separate from active findings.

This foundation supplies full-document data. Line filtering, contextual ancestor
selection, selected-region eligibility/counts and command reports belong to
#140 and #141. Their sections are `not-requested` here. It implements neither
MD087 counterfactual conclusions nor additional rule implementations. Resource,
frontmatter, slide, GFM and Pandoc processing retain their own admission gates;
no CommonMark substitute or guessed document context is supplied.

## AST catalog

The current profile is `commonmark`, using the actual CommonMark 0.31.2 parse.
The document root covers every original byte, including empty input. Node IDs
are deterministic pre-order `node-N` identifiers; children retain parser source
order and all actual inline children. Coordinates use the shared one-based
Unicode-scalar mapping, with exclusive ends and original zero-based byte spans.
CRLF interiors are never fabricated as coordinates. Bounded coordinate lookup
reserves its binary searches and actual Unicode line-prefix bytes on the same
operation meter; it does not charge the complete document for each node endpoint. Full-document nodes have
`context_only: false`; future presentation filtering may mark retained ancestors.
No HTML renderer, reflection, private parser struct or address participates.

Every node has `properties.markers` and `properties.content`, including empty
arrays. Markers contain a documented parser role and original `byte_range`.
Content segments have a transformation (`exact`, `escape`, `entity`,
`normalized`, `partial-tab`, `synthetic` or `unavailable`), an optional original
byte range, and optional semantic text. Absence means unknown/inapplicable;
a present empty string or zero-width source span remains known empty. Values
used below contain `segments` and optional `semantic`, preserving this distinction.

| Node kind | Additional properties, when present |
| --- | --- |
| `document`, `paragraph`, `thematic-break`, `block-quote` | No additional kind properties; children and marker/content facts remain source-backed. |
| `heading` | `level`, `style` (`atx` or `setext`); actual inline children remain present. |
| `list`, `list-item` | `ordered`; optional `start`, `delimiter`, `bullet`, `tight`; item-only `marker_width`, `padding`, `content_indent`, `prefix` Value. |
| `code-block` | `style` (`fenced`/`indented`), optional `fence`, `width`, `info` Value and `literal`. |
| `html-block`, `inline-html` | Optional `block_form` only for block origins; raw content is retained. |
| `text` | Optional semantic `literal`, independently of original content. |
| `soft-break`, `hard-break` | `origin` (`line-ending`, `spaces`, `backslash`); original physical ending remains content. |
| `code-span`, `emphasis`, `strong` | `marker`, `width`; code spans may have normalized `literal`, emphasis/strong retain inline children. |
| `link`, `image` | `origin`, `label`, `destination`, `resolution`; optional `reference_label`, `normalized_label`, `title`, with source-backed visible children. |

`raw_attributes` is empty for the current CommonMark profile, which has no
attribute extension. It does not invent effective attributes from raw HTML.
The optional AST `references` collection retains every source definition in
source order, including duplicates and unused definitions: envelope, markers,
label/destination/title Values, normalized label and first-selection `effective`
flag. Resolved reference-use destinations remain explicitly synthetic Values;
the definition retains their original spelling.

A future profile must extend this catalog with documented source-backed kinds,
properties, attributes, original/effective distinctions and independent parser
and producer acceptance. The illustrative GFM AST fixture is a serialization
example; it neither enables GFM nor permits omission of real inline children.
Pandoc footnotes, attributes, cells/separators and HTML-origin distinctions remain
future profile obligations rather than invented nodes in this projection.

## Rule records and source coverage

Each known catalog entry retains implementation availability, configured enabled
state, effective directive permission/options, effective source intervals and
actual active/observational evaluation status. Availability, applicability and
execution are independent. `rule-unavailable`, `inapplicable`, `inactive`,
`parser-uncertain` and `context-unavailable` explain distinct skips. MD094 records
the unconditional encoding gate, including when configured attribution is off;
it is never registered or invoked as an ordinary callback.

The additive optional `source_coverage` field has `active` and `observational`
entries. Each contains `availability`, public `ranges` and original `byte_ranges`.
Only a callback that returned complete without failure establishes `complete`
full-snapshot coverage, including `[0,0)` for an empty file. A partial or failed
callback has `unknown` coverage with no proved subranges; findings or enabled
state intervals cannot establish checked source coverage. Skipped/unrequested
work has `not-requested` coverage. The shared validator checks these invariants.

`ObserveInactive` explicitly requests disabled-throughout rule observations on
the same original source, options, parser context and budget. It invokes eligible
callbacks once, records their separate status, and puts their findings solely in
`suppressed_diagnostics` with configuration/directive attribution. Disabled
observation invokes none. MD087 is never recursively observed. This mechanism
does not infer used/unused suppression counterfactuals.

## Directives and raw candidates

The shared processor alone recognizes comments, validates grammar, applies
permissions and establishes intervals. Directive IDs follow confirmed candidate
source order. Records retain raw spelling, original/public ranges, closing offset,
operation when known, disposition and per-rule effects/ranges. Optional
`named_tokens` preserve original tokens, including duplicates; `resolved_rules`
contains only known catalog IDs. Mixed allowed/blocked/inapplicable effects remain
separate. `permission_provenance` retains every denial's path, field and point,
marking selected versus inherited origins. Blocked requests never own intervals.

Intervals begin after the permitted comment closer. Repeated no-op instructions
retain their own last-owner identity, and the final interval owns the EOF anchor.
Suppressed findings link to the exact interval owner at the validated finding's
start, or to configured disablement. They stay separate from active diagnostics.

The optional `raw_candidates` section is a separate bounded byte inventory of
comment-looking mdchk spellings. It retains exact `raw_base64` and byte ranges,
including malformed UTF-8, with `confirmed`, `literal` or `unconfirmed` context.
Code and raw-HTML interior spellings cannot fabricate confirmed directive records
or transitions. Invalid configuration, unavailable parsing or bad UTF-8 leaves
raw context unconfirmed. UTF-8 failures retain the first original-byte observation
and withhold normal parsing, directives and callbacks; a literal U+FFFD is valid.
Invalid configuration supplies no defaults. A known unavailable flavor keeps its
validated configuration and flavor while dependent sections remain unavailable.

The schema remains result version 1 and projection version 1. New source coverage,
reference records, named/resolved tokens, plural permission provenance, section
`cause_ids`, raw candidates and `normally_excluded_known` are optional additions;
old fixtures remain valid. `normally_excluded_known: false` distinguishes an
unresolved exclusion decision from a known negative boolean. The same schema and
semantic validator validate actual package envelopes and ordinary lint results.

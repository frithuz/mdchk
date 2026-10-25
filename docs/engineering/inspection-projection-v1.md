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

Analyze supplies full-document data with counts `not-requested`. Filter adds
contextual selection and counts as described below. The inspect CLI renders this
projection in text/JSON and preserves its identities and causal links. It implements neither
MD087 counterfactual conclusions nor additional rule implementations. Resource,
semantic metadata and slide processing retain their own admission gates;
no CommonMark substitute or guessed document context is supplied.

## AST catalog

The profile is the actual admitted `commonmark` or `gfm` selection. GFM uses
CommonMark 0.31.2 plus all five frozen extensions; legacy Parse APIs stay core-only.
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
| `html-block`, `inline-html` | Optional `block_form` only for block origins; raw content is retained. GFM adds `filtered_tags`, including a known-empty array for an owner with no affected tags. |
| `text` | Optional semantic `literal`, independently of original content. |
| `soft-break`, `hard-break` | `origin` (`line-ending`, `spaces`, `backslash`); original physical ending remains content. |
| `code-span`, `emphasis`, `strong`, `strike` | `marker`, `width`; code spans may have normalized `literal`, emphasis/strong retain inline children. |
| `link`, `image` | `origin`, `label`, `destination`, `resolution`; optional `reference_label`, `normalized_label`, `title`, with source-backed visible children. |

GFM adds these source facts without changing the version 1 envelope:

| Owner | Properties |
| --- | --- |
| `table` | Positive `width` and ordered `alignments` (`none`, `left`, `center`, `right`). |
| `table-row` | `role` (`header`, `delimiter`, `body`), original `raw_cells`, spanless semantic `padding`, all original `separators`; optional `leading_pipe`/`trailing_pipe`. |
| `table-cell` | Original complete backslash-pipe spans in `escaped_pipes` (including known empty); optional delimiter `alignment`. Every original cell, including empty/excess cells, remains a child. Padding creates no source cell. |
| `list-item`, first direct `paragraph` | Identical `task` object with original `paragraph`, `marker`, `opening`, `check`, `closing`, `separator` byte ranges, ordered marker `segments` excluding container gaps, semantic `checked`, and exact `check_spelling`. Existing list/container facts retain their own owners. |
| `html-block`, `inline-html` | `filtered_tags` contains ordered `name`, `closing`, original `prefix` Value and one-byte `affected` angle. Optional `tag` Value exists only for a proved complete tag. The unchanged node is the protected owner. |

Separator, escaped-pipe and task ranges use original half-open byte coordinates;
the owning nodes additionally retain public scalar coordinates. Task marker
segments can be discontinuous. Absent optional values remain distinct from known
empty Values, zero-width original cells and spanless padding. `bare-gfm` retains
visible source in the ordinary link fields; only injected destination prefixes
are spanless synthetic segments. Selected views preserve these original facts
and canonical Go types, with no clipping or synthesized source authority.

`raw_attributes` is empty for the current CommonMark profile, which has no
attribute extension. It does not invent effective attributes from raw HTML.
The optional AST `references` collection retains every source definition in
source order, including duplicates and unused definitions: envelope, markers,
label/destination/title Values, normalized label and first-selection `effective`
flag. Resolved reference-use destinations remain explicitly synthetic Values;
the definition retains their original spelling.

A future profile must extend this catalog with documented source-backed kinds,
properties, attributes, original/effective distinctions and independent parser
and producer acceptance. The illustrative GFM AST fixture remains a serialization
example; real producer tests independently verify the catalog and inline children.

## Opaque frontmatter source record

When recognition is enabled, optional `ast.data.frontmatter` records the
recognized YAML/TOML header separately from the ordinary body-only root children.
It contains `format`, full original `range`/`byte_range`, `opener` and
`content` source regions, optional `closer`, `body_offset`, `context_only`
and optional `extends_region`. Each component region contains public scalar
`range` and original `byte_range`. No decoded metadata values are implied.

The envelope includes an optional BOM, delimiters and their original endings.
Marker regions omit BOM/endings and retain trailing ASCII spaces/tabs. Content
excludes delimiter lines; an empty payload is a present zero-width region.
`body_offset` equals the envelope's exclusive end. All root children and
reference definitions begin at or after it, while the root still spans the
entire original snapshot.

An absent record in an available AST proves no recognized header for the
effective settings. Disabled recognition and enabled recognition without a
matching header remain distinguishable through configuration values/provenance;
unavailable AST data does not prove absence. An absent closer means a recognized
unterminated header through EOF. Its AST section is partial, its reliable source
record remains visible, and a located `unterminated-frontmatter` parsing note
explains the incomplete result. This does not manufacture completed directive
or ordinary rule context. Structured metadata values and title/order inspection
remain unavailable in this projection. MD001 consumes title semantics separately
through its engine-owned opt-in analysis path.

Line selection retains the complete header as source context, marking it
`context_only` when it does not intersect selected lines and `extends_region`
when its original span extends beyond them. No header node, clipped region or
invented byte is inserted into the body tree. Raw directive-looking spellings
inside the opaque header can be displayed as literal context; they never own
directive transitions.

This is an optional additive result/projection version 1 field. The schema
bounds its component records; the semantic validator additionally checks envelope,
body offset, termination, body/reference exclusion and selected-context agreement.
Producer tests mutate actual emitted header records to verify rejection of
out-of-snapshot, inconsistent body-offset and contradictory closer states.

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

## Selected presentation and counts

`Filter(ctx, snapshot, analysis, lines)` consumes the privately retained full
`Analyze` authority. It never reopens a file, reparses a region, or reruns a
callback. Public projection/envelope mutations cannot change later filters.
An independently owned selected view can also be filtered again; every selection
starts from the original full analysis. The caller must supply the same snapshot.

`lines` is empty for the whole document or exactly positive decimal `N` or `N-M`,
without leading zeroes, signs, whitespace or overflow. Bounds are inclusive
physical content lines. Their byte selection contains each physical ending,
including CRLF, and excludes the next line's start. A final real line additionally
selects the EOF anchor, even when its public coordinate is after a terminal
newline. Empty input has no selectable line; its unfiltered `[0,0)` selects EOF
and retains configured enablement. `file.physical_line_count`, `selected_lines`
and `selected_region` document the distinction between physical bounds, original
byte positions and the EOF anchor.

The document root remains `[0,size)`. Intersecting nodes keep original IDs,
coordinates, properties, markers and segments. Other siblings disappear;
necessary structural ancestors are `context_only`, and `extends_region` marks
returned nodes and findings spanning beyond the selection. Zero-width findings
are positions; nonempty findings use exclusive ends. Suppressed findings are
selected by their complete finding span, while attribution remains the original
start's last permitted directive owner. Selection never reinterprets suppression.

Every catalog rule remains present. Optional `selected_enabled` means any
selected source position is enabled. Available counts count every known rule
once, independently of implementation, syntax applicability, findings or actual
execution. Unknown shared effective state makes counts unavailable, even when
configuration settings are known. Original full-source state intervals and
proved execution coverage remain unchanged: selected eligibility cannot claim
that a callback ran only on a region. Rule counts do not repair incompleteness.

All original directives, raw candidates and source references remain as context
facts, labeled `context_only` outside the selected span. Keeping this closure
preserves every original interval owner, multi-rule/last-owner effect, source
reference destination, literal distinction and cause even when outside selection.
It deliberately permits extra labeled context rather than clipping intervals or
renumbering facts. Whole-document parsing and processing notes remain visible;
optional `relevance` is `intersects`, `context`, or `whole-document` for a
location-free note or unfiltered view. Errors, coverage, omissions, interruptions,
file outcomes and their IDs are retained without reinterpretation.

Filter reserves JSON traversal/copy work by the retained authority's actual byte
size and construction by three times its object/array count plus fixed selection records,
covering decoding, typed restoration and replacement filtered collections, excluding quoted delimiters.
The authority contains the projection once; its own encoding and structural scan
are reserved before construction. Byte size is never charged as a count of
syntax nodes. Acquisition, full analysis and filtering use one residual operation
meter. On cancellation or exhaustion, Filter returns the original analysis and
typed cause without publishing a partial selected view or zero counts. Callers
must handle that error before reporting successful region selection. Filters can
read the same full analysis concurrently; returned mutable graphs are separate. Canonical Go option/list and AST source
property types survive filtering; restoration uses documented property types and
defensive views of the same immutable configuration, without provider callbacks.

These fields are optional additive version 1 extensions. Older fixtures remain
valid. Actual filtered envelopes use the existing shared JSON Schema and semantic
validator, including unchanged full-root, interval-owner and execution-coverage
invariants and new selected-bound/count checks. The package API is implemented;
inspection command reporting is implemented with compiled end-to-end acceptance.

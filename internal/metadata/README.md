# Metadata contracts

This package defines the model and consumer policy for recognized metadata.
`Decode` now produces source-mapped YAML and TOML values on demand. MD001, MD025 and MD041 consume title policy through the engine on opt-in. Broader production
metadata rules, inspection projection and edits remain downstream work.

`Bind` admits only the actual document-owned header under its original parser
operation budget. A complete header is distinct from an absent or unterminated
one; body uncertainty does not erase a reliable header boundary. Equal bytes in
a different snapshot or a different budget cannot reuse recognized authority.
`parser.Frontmatter.ReserveRecords` extends the original parse's document counter,
with its existing cumulative operation limit and the original or derived sticky
document limit. Original document exhaustion is retained without replacing its
first observed amount or stopping a different document in the same operation.
It does not reset the allowance or establish decoding. No worker or cache is added.

The immutable `Value` model retains ordered mappings, exact decoded string keys,
sequences and native scalar kinds. The scalar semantic string is a typed exact
representation, not conversion to the metadata String kind: integers use canonical
decimal text without a float64 intermediary; finite floats use an exact integer
coefficient followed by `e` and a decimal exponent, such as `125e0`. Trailing
coefficient zeros are removed; zero is `0e0`, with negative float zero `-0e0`.
Special floats are `nan`, `+inf`, `-inf`. Temporal values retain distinct offset-date-time,
local-date-time, local-date and local-time kinds with canonical civil/offset text;
local times have no assumed server timezone. `Span` separately retains exact
original spelling. Boolean and null retain their own kinds. Format adapters own
semantic validation/canonicalization; these private model builders do not parse
source text. No public list-plus-completeness constructor exists.

An entry's key, value and whole-entry ranges belong to the recognized payload.
A synthesized TOML parent/table path has absent authored ranges rather than a
fabricated contiguous envelope. An alias projection retains the use location;
its derived child records never gain independent moveable entry spans. Private
builders freeze collections, reject foreign or out-of-payload spans, detect exact
decoded-key duplicates, bound model depth and retain located duplicate problems.
Results with unknown values or problems cannot claim complete metadata. Immutable
known facts remain inspectable after a stop, while new analysis/collections share
caller, operation, document-node, total-node, work and elapsed-time controls.

Title consumption accepts only the exact top-level `title` key with a decoded
string containing a non-White_Space code point (`unicode.IsSpace` in the pinned Go
toolchain). It returns the original decoded string and authored location without
trimming. Nested keys, `Title`, blank strings and non-string values do not count.
Missing is authoritative only with complete metadata; exact duplicate title keys
make the title unknown. An independently established positive value can survive
unrelated uncertainty.

Ordering defaults to nonrecursive, case-sensitive Unicode code-point order.
`CaseInsensitive` selects Unicode 17.0.0 full default folding (C/F mappings,
excluding locale-sensitive T and simple S), reusing the parser's pinned table.
There is no Unicode normalization, whitespace collapse or NUL substitution.
Stable ties retain source order. Recursive mode visits mappings inside sequences;
sequence elements never move. Returned permutations do not mutate the source model.

Whole-entry eligibility is only a necessary preservation policy. Complete,
nonoverlapping independent physical-line entries may retain their exact quoting,
value spelling, line endings and inline comments. Standalone comments, YAML
anchors/aliases, partial-line/flow members and synthesized entries are report-only.
TOML tables, dotted assignments and array-of-table elements never move. Restriction
facts propagate to containing blocks. An ordering violation does not authorize a
move. Current ordinary fix protection still preserves the entire opaque header.
A future metadata-specific correction must independently reparse under the same
recognition/format policy and prove unchanged native values, sequence order,
delimiter binding and all untouched bytes before proposing or applying edits.

`Decode` accepts only a genuine bound header. Empty complete payloads produce
empty mappings; absent or unterminated headers produce unavailable results, with
a located problem for unterminated input. These unavailable outcomes reserve their
new root/problem records and work before construction, under the same binding. Malformed syntax and nonmapping roots
produce an unknown root with a located problem. Unsupported YAML subvalues retain
independently decoded sibling facts, while unknown/duplicate keys deny negative
authority. No decoder result alone enables a production capability or authorizes edits.
The CLI admits metadata interpretation for the actual MD001, MD025 and MD041 title consumers;
MD072 remains independently unavailable.

YAML uses the pinned goccy AST through `internal/yamlsource`, shared with MkDocs.
That neutral boundary owns normalization, tag/tab/escape reconciliation, guarded
lexing and original-byte mapping; metadata never imports MkDocs policy or the
configuration schema. Empty scalar tags are reconciled across lexical comments
and separation without consuming sibling keys or merge syntax; presentation
comments cannot create scalar content or native defaults. Core classification
uses source spellings instead of the
library's legacy numeric classifications. Standard tags are validated; custom
tags, merges, unresolved aliases and cycles are incomplete. Acyclic aliases bind
in authored order, including rebinding. Each original alias occurrence retains
its first binding (including an unresolved occurrence), so reusing a saved graph
cannot acquire later anchors. Every descendant uses the alias use
span with no independent whole-entry envelope.

TOML uses pinned pelletier v2.4.3 source expressions, copied before advancing its
arena, plus its public semantic decoder for duplicate/redefinition validation.
That validation discards generic decoded values; source records own order/ranges.
Synthetic table/path parents have absent authored spans. Integer values retain
exact arbitrary-size decimal semantics under the shared work and storage bounds. Temporal text preserves arbitrary fractional
precision, removes fractional trailing zeros, inserts omitted seconds and uses
`T`/`Z`; explicit nonzero offsets and negative zero offsets remain explicit.
Civil seconds `60` retain their leap-second marker without a host event-table
lookup or conversion to a native instant. Values above `60` are rejected.
Local values use no host timezone. Bare CR payloads remain invalid TOML even
though opaque recognition supports CR delimiter lines.

Before native entry, Decode reserves input/temporary storage and work against the
original document/operation. YAML guards maintained tokens before AST recursion;
TOML skips quoted/comment payloads and bounds value syntax before AST entry, then
checks full logical table/dotted/AOT depths before semantic validation. Exact
numeric conversions reserve a quadratic work bound before big integer routines.
Synthetic null positions reuse a charged index of reconciled lexical positions
from the monotonic offset pass; they never rescan each physical line prefix.
Projection, alias expansion, retained records and consumer analysis charge the
same original controls. Known immutable values remain readable after a stop.

Source-based decoder fixtures independently assert native kinds/text, original
key/value/entry slices, comments, aliases, temporal precision, ownership conflicts,
physical endings and pre-entry guard failures. The earlier private-builder
fixtures continue to test model/policy contracts. Actual correction preservation
and other production integrations remain unverified until their owning issues deliver.

# Metadata contracts

This package defines the model and consumer policy for recognized metadata.
It does not yet decode YAML/TOML, run metadata rules, expose an inspection section
or produce edits. Those remain the format-adapter and integration issues.

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
decimal text without a float64 intermediary; floats use canonical decimal/exponent
text or `nan`, `+inf`, `-inf`. Temporal values retain distinct offset-date-time,
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

The contract fixtures construct validated private model records with independent
expected values. They are not decoder/conformance fixtures. Concrete format
parsing, semantic validation and actual correction preservation remain unverified
until their owning issues deliver those producers and consumers.

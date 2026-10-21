# Shared directive foundation

`Process` consumes an actual completed `parser.ParseWithBudget` result, a validated
`config.Configuration`, and the same operation budget. It never scans the Markdown
source for comment openers. `Document.Comments` supplies owned, source-mapped facts
from recognized inline and block comments; its private parse handoff cannot be
created with the public model constructors. Original comment spans stop at the
closing marker even when a physical HTML-block envelope contains trailing text.
Mapped payloads omit quote/list prefixes while retaining original token anchors.
A private parser-owned inventory recognizes further HTML comments in the physical
closing-line tail using the same HTML lexer and mapped cursor. This applies to
all explicitly terminated HTML blocks: comments, raw script/pre/style/textarea,
processing instructions, declarations and CDATA. Literal payload before the
confirmed terminator stays opaque. Quoted attributes
remain opaque. Raw closing tags preserve literal-context transitions across inline
and block nodes.

A candidate begins with a case-insensitive `mdchk` introducer followed by a colon,
Unicode whitespace, or end of payload. Validation requires the exact case-sensitive
commands and canonical catalog IDs. Unicode White_Space separates tokens. Ordinary
comment text such as `mdchecker` is not a candidate. Invalid lists are atomic;
known unavailable rules remain valid targets. Globally disabled candidates retain
raw spelling and governing denials without syntax findings. Literal HTML interiors
and unclosed comments remain inactive with explanations.

Configuration enablement/options and permissions come from private immutable
snapshots, independently of mutable inspection views or executable registration.
Intervals begin after each permitted closing marker, including repeated no-op
instructions. Their owner is a candidate index; initial configured state has owner
-1. The final interval owns the EOF anchor, including an empty interval created by
a directive closing at EOF. `StateAt` and all collection accessors return owned
views. MD074 and MD094 have inapplicable document effects. These records provide
ownership and token anchors for later MD087 analysis; no counterfactual runs occur.

Any unresolved configuration, uncertain parse, cancellation or exhausted shared
budget withholds intervals and marks the result incomplete. Already collected
candidates and notes remain inspectable. Directive syntax diagnostics use the
shared diagnostic structure with identifier `directive`; operation adapters must
attach their path, source snapshot and final result IDs before transport.

The processor implementation belongs to the shared engine; this package retains
its recognition/state facade. Lint applies finding-start suppression and dynamic
scheduling/coverage before reporters run. Production CommonMark directive
capability is available. Exact owned attribution is retained in shared internal
analysis; lint wire notes and suppressed records stay within the existing schema
without dangling directive IDs. Corrections, inspection/MCP transport, observational
execution of disabled-throughout rules and future parser flavors remain downstream.

Permission provenance has cached immutable work estimates including origin record
counts, path/property byte sizes and optional positions. The processor reserves
these units before owned permission queries and duplicate blocked-note copies;
large inherited denial lists therefore consume the shared remaining work budget.

# Rule recognition and special behavior

Status: accepted on 2026-10-15; implementation and end-to-end verification remain pending.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

## Malformed syntax candidates

### Shared policy

Define bounded source-candidate recognizers alongside the selected dialect parser. Preserve parser
ownership of valid constructs and protected code/raw/control regions. Do not infer author intent.
Candidate recognition must not cross block/container boundaries; initially keep unparsed inline
candidates to one physical line. Intentionally excluded ambiguous candidates are outside the rule
scope and do not themselves cause incomplete coverage. Unreliable parsing or an unavailable required
prerequisite still causes affected incomplete coverage.

### Md069

MD069 analysis is implemented for CommonMark and GFM using a bounded parser-backed
projection over actual list items and original physical-line ASCII space/tab gaps.
Require direct item ancestry and reject different physical lines before gap scans.
Report each maximal identical-bullet subrun of length at least two, spanning its
first marker start through its last marker end, with original whitespace retained.
Mixed pairs, ordered markers and quote discontinuities end a run; later inner
subruns remain eligible. `- - + + Text` reports two runs; `- - - Text` reports one
full run. `- > - - Text` reports only the inner pair. Parser ownership protects
thematic/code/raw/metadata regions without erasing independently owned outer
markers: `- - * * *` reports the dash pair; pure `- - -` is thematic and excluded.
Empty EOF `- -` remains eligible. No unparsed heuristic or generic framework for
other candidate rules is implied. MD069 remains permanently report-only with NoFix.

### Md037

Analyze each maximal source-contiguous ordinary Text island under one actual
inline owner on one physical line. Adjacent Text allocations join; source gaps,
physical LF/CR/CRLF, code, HTML, breaks and actual inline constructs split islands.
Eligible prose includes headings, list/quote/task bodies, GFM cells and local
link labels/image alternatives. Parser-owned structural markers and protected raw
content remain excluded. Actual valid Emphasis/Strong syntax always wins.

Count maximal exact unescaped runs of both `*` and `_`. Any run longer than two
excludes the whole island before other tests. Width-one/two runs whose immediate
original neighbors are both Unicode Letter, Number, Mark or ASCII underscore are
neutral payload for either family. Exactly two remaining runs must have equal
marker and width. A third competitor, mixed family/width or apparently multiple
flat pairs is deliberately excluded with complete coverage when parsing is reliable.

Reject a pair when the opener's preceding or closer's following original scalar
within the island is Letter, Number, Mark or underscore. Boundaries use raw source,
before entity/escape decoding. Escapes and entities remain indivisible ordinary
payload but never supply marker bytes or offending whitespace. Normalized NUL,
partial-tab mappings and raw Cc controls except TAB/FF interrupt candidates.

The interior must have actual semantic nonblank Text. Whitespace means CommonMark
Unicode Zs plus TAB/LF/FF/CR; physical endings terminate islands, so edges contain
only literal Zs/TAB/FF. Entity-only `&nbsp;` is blank; `&amp;` is nonblank. VT/NEL
are barriers; LS/PS are ordinary nonblank scalars. No re-decoding is performed.

Report each maximal contiguous original whitespace run immediately inside an edge,
at most two findings, with `Whitespace within emphasis delimiters.`. Each finding
uses its own whitespace START for directive suppression, exclusive original-byte
ranges and one-based Unicode-scalar columns. For `A * text * example.` the ranges
are [3,4) and [8,9). No edits, previews or correction eligibility are produced.
Incomplete required syntax yields affected coverage rather than guessed candidates.

See the [MD037 rule page](../rules/formatting/md037.md) for examples and configuration.

### Md038

MD038 analyzes actual parsed CodeSpan nodes in eligible Markdown inline contexts.
Both original delimiter-adjacent edges must have ASCII SPACE runs. One-sided runs
of every length and all-SPACE normalized Literal values are meaningful and accepted.
For other paired runs, actual Literal edge SPACE or backtick requires one innermost
bounding byte at each edge; report only outer excess. Otherwise report both full
runs. TAB/FF/VT/Unicode spacing and physical line endings are never padding ranges.
Use retained original Content/marker ownership for geometry, including trimmed
empty-semantic segments, rather than semantic offsets or envelope reconstruction.
Protect actual raw HTML lifetimes and closing tails; image alternatives isolate
raw state. Each range uses its own start for suppression and the fixed message
`Whitespace within inline code delimiters.`. These are manual style findings,
not permission to delete bytes or proof of preserved code value. See
[MD038](../rules/formatting/md038.md) for the complete analysis policy.

### Md044

MD044 matches configured names only within Exact, touching-source, single-line
Text islands under one actual inline owner. Transformed barriers preserve semantic
outside-neighbor word facts. Unicode17 full C/F folding uses original scalar
endpoints, no normalization, and Letter/Number/Mark/ASCII-underscore boundaries.
Leftmost then longest original occurrences are consumed even when exact; fold-equal
configured spellings are accepted exact alternatives independent of list order.
Every differing winner reports the complete original occurrence with
`Configured name uses different capitalization.`. Authored labels and alt text
are eligible; actual URLs, raw lifetimes, comments and recognized headers remain
protected. Ordinary CommonMark bare URL-looking Text remains eligible. Code opt-in
admits literal actual CodeBlock Content only; CodeSpan stays protected. Names have
a narrow valid-UTF8 list domain, including disabled validation; no other lists
are narrowed. See [MD044](../rules/formatting/md044.md) for the full policy.
Pass 1 produces manual analysis only; MD047 remains the sole correction producer.

### Md049

MD049 consumes only actual Valid Emphasis facts of width one from the shared
parser delimiter inventory. Strong and malformed Whitespace facts never seed
or produce italic findings. Consistent style uses the minimum original opening
position in the whole document, including suppressed or inactive intervals
whenever the callback executes. Fixed styles select their configured marker.
No container reset or cross-document mutable baseline is permitted.

One manual finding identifies each mismatching original opening byte with
`Italic delimiter style does not match the expected style.` Shared suppression
uses that opening START independently of closing state. Actual nested/triple
emphasis, authored labels/alternatives and transformed children remain eligible;
actual code, metadata, raw lifetimes/tails and URL ownership stay protected.
The same shared facade admits MD037 empty options or exactly one valid style;
MD049 independently requires a resolved style. No MD049 correction is produced.

### Md050

MD050 consumes actual Valid Strong width-two facts from the same delimiter
projection and resolved-style getter. Emphasis and malformed Whitespace never
seed or report. Consistent selects the minimum original Strong Opening.Start
per immutable document before suppression or inspection slicing; fixed styles
select their marker. Nested/triple/longer consumed roles, transformed children,
authored labels and alternatives retain actual ownership and shared protections.
One manual finding per mismatch covers exactly the complete two-byte Opening
with `Bold delimiter style does not match the expected style.` Opening START
alone governs suppression. No correction authority or alternate parser is added.

### Md011

Recognize flat, immediately adjacent (label)[URL] candidates with nonblank label and a valid
absolute HTTP or HTTPS URL with a nonempty host, on one line. Exclude images, escaped outer
delimiters, nested/ambiguous delimiters, whitespace in the URL and relative destinations. Existing
valid parsed constructs take precedence. Preserve the documented conditional correction policy,
requiring reparse and proof of the intended link structure before any later fix.

### Md052

Recognize explicit full and collapsed reference syntax, including images: [text][id], [text][],
![text][id], ![text][]. Reuse the selected dialect reference-label grammar and normalization and
authoritative definition collection. Bare [text] is not guessed to be an unresolved shortcut;
shortcuts remain eligible only when positively identified as references by the parser. Exclude
escaped/malformed bracket pairs and candidates overlapping another valid parsed construct.

### Md062

Recognize flat, fully closed inline link/image candidates with whitespace inside a bare destination,
e.g. `[Guide](my guide.md)`, on one physical line. Exclude valid destination-plus-title syntax,
permitted angle-bracket destinations, escaped whitespace as specified by the rule, and nested or
otherwise ambiguous malformed delimiters. Keep valid parsed destination checks intact; the extra
candidate grammar handles forms which otherwise fall back to plain text. No percent-encoding fix is
implied.

### Md075

Require at least two consecutive pipe-row candidates in one container, or a candidate row
immediately adjacent to but outside a parsed table. A run has clear row structure when its rows have
matching cell counts of at least two, or all rows have both outer pipes, or it contains a
dialect-valid delimiter-shaped row adjacent to a pipe row. Single unconnected prose pipes are
excluded. Use dialect-owned structural pipes, escaping, protected inline spans and separator
classification. Rows already inside a parsed table belong to MD056 rather than MD075; do not
manufacture a table from malformed rows.

### Corrections

Retain the existing rule-specific correction policies. MD069, MD037, MD052, MD062 and MD075 stay
report-only. MD011 retains only its existing unambiguous conditional correction, with later shared
fix validation.

## Trailing whitespace and hard breaks

### Configuration

Keep MD009 enabled by default, hard_break_spaces an integer >=2 with default 2, strict=false, and
code_blocks=false. hard_break_spaces is an exact permitted/canonical count, not a minimum.

### Ordinary nonblank lines

#### No trailing whitespace

Allowed in both strict modes.

#### One space

Report in both strict modes.

#### Exactly n spaces

With strict=false, allow regardless of whether a parsed hard break results. With strict=true, allow
only when the selected dialect parser confirms a hard break.

#### Other nonzero space counts

Report in both modes, including two or three spaces when N=4 and three or four spaces when N=2.

#### Tabs or mixed tabs spaces

Report in both modes; tabs never satisfy the configured all-space allowance.

#### Eof

With exactly N trailing spaces, strict=false permits them and strict=true reports them because EOF
does not produce a hard break. Other counts follow the same table.

MD009 specializes the shared container-relative spacing default: its finding is
one whole maximal physical ASCII space/TAB suffix before LF/CRLF/CR or EOF.
Blankness removes only actual parser-owned list/quote prefixes; consumed suffix
whitespace still reports, while markers never enter the range. Task checkboxes
remain nonblank content. Strict allowance requires a spaces-origin hard-break
marker equal to the full all-space suffix. A terminal paragraph ending does not
create a break. Code opt-in includes multiline CodeSpan payload and all physical
fenced/indented payload contributions, including blank indentation; actual fenced
opening/retained closing lines follow the ordinary numeric policy. Inline closing
delimiters leave outside suffixes eligible. Full recognized metadata headers and
HTMLBlock tails remain protected, as do actual inline HTML/comment owner bytes
and semantic raw-tag payload between actual tags, including Text/HardBreak bytes.
Image-description tags cannot change enclosing raw state. Protection wins code
opt-in. For example, `> <SPACE>` reports, `>` passes, and `a<TAB><SPACE><SPACE>`
reports despite any smaller real break marker. These are manual analysis ranges;
MD009 supplies no correction producer in this pass.

### Blank lines

Report whitespace-only lines in both strict modes; there is no hard-break allowance for them.
Preserve any parser-owned structural indentation or container prefix when correcting.

### Corrections

For flagged whitespace that currently produces a real hard break, normalize to exactly N ASCII
spaces. For other flagged trailing whitespace, remove it only after reparsing confirms preserved
interpretation. Never introduce or remove a hard break or change container structure as a side
effect. Already permitted whitespace is left alone. Preserve source newline conventions.

### Literal code

By default, code is excluded. With code_blocks=true, report trailing spaces or tabs in literal code
without applying a Markdown hard-break allowance. Keep literal-code findings report-only;
code_blocks opts into analysis, not modification of code bytes.

### Protected regions

Frontmatter, raw payloads and other protected regions retain their established protections; parser
certainty remains required for automatic corrections.

### Whitespace domain

MD009 treats only ASCII spaces and tabs as trailing whitespace. Other Unicode spacing characters
remain content and are not stripped by this rule.

## ASCII punctuation

### Configuration

Keep MD088 disabled by default. style defaults to ascii and ascii is initially its only accepted
value. The replacement table is fixed behavior, not a configurable replacements field; reject
replacements as an unknown option under the shared strict schema.

### Mapping

#### U+2018

'

#### U+2019

'

#### U+201c

"

#### U+201d

"

#### U+2013

-

#### U+2014

--

### Scope

Replace only those six source characters in eligible prose. Keep other Unicode characters unchanged.
Preserve code, URLs/destinations, metadata, and existing protected syntax.

### Implemented analysis scope

MD088 includes actual Paragraph, Heading and GFM prose TableCell Exact Text, with
transparent Emphasis/Strong/Strike. Entire Links and Images are excluded for
findings. Genuine Link-descendant raw transitions remain global; Image children
and private tails cannot change outer raw protection. URL protection is actual
parser Link ownership: CommonMark bare URI Text remains eligible. No lexical URI
scanner is added. Private rawTag/rawClosing/htmlTail lifetime, code/info, comments,
attributes, opaque headers and retained definitions remain excluded. Entities,
escapes, normalized and partial-tab contributions create no source candidates.

Emit one original three-byte/one-code-point range per eligible scalar with exactly
`Use ASCII quotes and dashes in prose.` Em dash is one finding. Full context
precedes shared START suppression and inspect intersection. This pass is analysis
and configuration only: PlannedFix remains future metadata; MD047 is sole fixer;
MD088 creates no edits, previews or corrected content.

### Corrections

Use the existing source-span correction and preservation checks. Defer a correction if reparsing or
required reference-preservation checks cannot establish safety; preserve relevant findings. No
arbitrary replacement or broad transliteration feature is introduced.

## Table of contents

### Configuration

Keep MD073 disabled by default. Keep `<!-- toc -->` and `<!-- /toc -->` defaults. Custom markers must be
distinct, nonempty, single-line complete HTML comments. min_level and max_level are integers
satisfying 1 <= min_level <= max_level <= 6; retain defaults 1 and 6.

### Recognition

Recognize marker-only lines in parser-confirmed eligible HTML comment spans, after accounting for
container prefixes and surrounding spaces/tabs. Match configured comment text exactly and
case-sensitively. Ignore marker text in code, metadata, raw payloads or other protected contexts. No
markers means no TOC requirement.

### Pairing

Allow multiple sequential, nonoverlapping pairs. Each pair must remain in one container. Unmatched,
nested or cross-container markers produce a located problem and no inferred regeneration extent;
independently reliable regions may still be analyzed. Any affected analysis that requires uncertain
region or heading ownership remains explicitly incomplete.

### Heading selection

Every TOC covers the same whole-document selection, in document order, filtered to
min_level..max_level. Include parsed main-flow headings, including headings in ordinary parsed
Div/section wrappers, but exclude headings inside blockquotes, list items, code, metadata and all
marked TOC regions. Frontmatter virtual titles and arbitrary ID-bearing non-heading nodes are not
TOC entries. Do not infer local/section-only TOCs.

### Toc title

Allow one optional leading heading inside each marked region. Preserve that heading byte-for-byte
and exclude it from TOC entries. Do not guess TOC titles from text such as Contents. Headings
outside a marked region use the ordinary eligibility rules.

### Entries and anchors

Use parsed visible heading text as the link label, with safe Markdown escaping, and the settled
shared anchor service for destinations. Explicit anchor mode includes only eligible headings with
usable explicit IDs; no new generated IDs. Known policy exclusions are not incomplete coverage.
Actual parse/anchor uncertainty, including ambiguous target identities, blocks affected
comparison/correction.

### Comparison

Compare entry order, visible labels, resolved fragment destinations and logical outline nesting.
Equivalent bullet spelling, indentation or inline presentation alone is not an MD073 mismatch;
existing formatting rules own those details.

### Generation

Generate a deterministic unordered list. Choose parents from the preceding eligible heading stack:
pop headings of greater-or-equal level, attach beneath the remaining nearest shallower heading, and
never invent empty levels for heading jumps. Honor effective bullet marker, indentation, root-indent
and marker-spacing settings; use resolved document style for consistent-marker mode, or dash when no
marker exists. Preserve source newline style and container prefixes. Empty heading selection removes
entries while preserving markers and the optional title.

### Safe replacement

Replace only the list portion inside a valid marked region, preserving both markers, the optional
leading heading and all outside content. Automatic regeneration is allowed for an empty region or
one containing only that optional heading, whitespace and an ordinary TOC list. Unexpected prose,
comments, directives or other blocks make the region report-only. Reparse candidates and require
stable heading/anchor identities, intended list structure and the existing fix-safety guarantees.

## Nested Markdown examples

### Configuration

Keep MD070 disabled by default and without rule-specific options. Introduce fixed extent markers
`<!-- mdchk-example -->` and `<!-- /mdchk-example -->` for this rule only. Unmarked examples are outside
MD070 scope.

### Marker recognition

Require marker-only lines, an opening marker in parser-confirmed eligible comment context outside
protected content, and a unique nonnested closing marker in the same surrounding container. Use a
bounded source scan for the closing marker so a prematurely closed outer fence cannot decide the
intended extent. Do not activate a start marker displayed inside code, frontmatter or other
protected content. Multiple separate pairs are allowed; nested, missing or conflicting markers are
not guessed.

### Outer block

Between a valid pair, the first and last nonblank lines must be the intended outer opening and
closing fences, using the same character. The opener must identify Markdown through md or markdown
language metadata recognized by the selected dialect. The last line must be a delimiter-only closer
valid for the opener, not a guessed fence later in the document. Missing/invalid outer boundaries
produce a located problem and no automatic correction.

### Inner fences

Inspect only the declared example body, excluding the two outer delimiter lines. Count source-backed
fenced-block opening/closing runs recognized within that Markdown payload by the selected dialect;
ignore inline runs, escaped markers and non-fence code strings. Compare only runs using the outer
fence character. Emit MD070 when an eligible inner run is at least as long as the outer opening
fence.

### Correction

Set both intended outer delimiters to max(existing opener length, existing closer length, longest
eligible same-character inner run + 1). Never shorten either delimiter. Preserve all example-body
bytes, language/info/attributes, markers, line endings and container prefixes. Treat the pair as one
atomic correction.

### Validation

Require the corrected region to parse as exactly one outer Markdown code block containing the
declared body. Preserve content and reliable interpretation outside the region, and obey shared
directive, reference and edit-safety checks. If safety cannot be established, retain the finding
without applying a correction.

### Scope

Markers supply MD070 source extents only; they do not change normal Markdown parsing, activate other
rules, suppress other findings or grant a new directive mechanism. Any correction that would
unexpectedly alter effective directives or protected bindings remains deferred. Unmarked ambiguous
prematurely closed fences remain outside MD070 scope rather than being heuristically reconstructed.

## Decorative HTML images

### Configuration

Keep MD045 enabled by default, allow_decorative=false, and report-only.

### Base check

Check recognized Markdown images, reference images and raw HTML img elements. Missing HTML alt,
empty alt and Unicode-whitespace-only alternative text are findings unless the explicit HTML
decorative exemption applies. Decode HTML character references before classification; missing and
present-empty attributes remain distinct.

### Declaration

When allow_decorative=true, an explicitly present HTML alt attribute whose decoded value is exactly
empty declares the image decorative. No additional role is required. Empty HTML attribute syntax
equivalent to alt="" is treated by its decoded value; do not trim whitespace-only alt into an empty
decorative declaration.

### Conflicts

Do not grant the empty-alt exemption if the img has a nonblank title, any role other than an
absent/empty role or a single none/presentation token, any aria-* attribute except
aria-hidden="true", or a tabindex/usemap/ismap attribute. These are static, local disqualifiers for
this narrow exemption, not a full accessible-name or accessibility-tree evaluation. A nonempty alt
still satisfies the base MD045 check; unrelated ARIA conflicts are not new MD045 findings.

### Non declarations

role="none", role="presentation", aria-hidden="true", hidden, filenames and visual appearance alone
never replace the required explicit empty alt declaration. Missing or whitespace-only alt remains
reportable even with those attributes.

### Markdown

The exemption applies only to explicitly authored HTML img markup. Empty Markdown image syntax such
as `![](image.png)`, including reference forms, remains reportable even with allow_decorative=true.

### Uncertainty

Malformed or duplicate relevant attributes that prevent reliable interpretation produce a located
parsing problem and affected incomplete coverage rather than a guessed exemption or alternative
text. Preserve reliably established independent findings.

### Scope

Do not infer whether an image is truly decorative from pixels or surrounding prose, fetch images,
synthesize text, add attributes, or perform full page accessibility validation. Keep all MD045
findings report-only.

## Unicode hostnames

### Activation

Keep MD095 disabled by default and report-only. Use the shared parsed destination inventory and
protected-region rules; do not infer additional links from arbitrary prose.

### Host scope

Check DNS hostnames in hierarchical absolute URLs and protocol-relative destinations with an
authority. Separate user information and port before hostname processing. Paths, query strings,
fragments, display text, local paths, opaque URIs including mailto, and IPv4/IPv6 literals are
outside MD095. This is a character-lookalike heuristic, not brand reputation or proof of malicious
intent.

### Data

#### Version

17.0.0

#### Url

https://www.unicode.org/Public/17.0.0/security/confusables.txt

#### Sha256

091c7f82fc39ef208faf8f94d29c244de99254675e09de163160c810d13ef22a

#### Bytes

745683

### Idna

Pin Unicode 17.0.0 hostname processing and normalization alongside the confusable table. Use UTS #46
nontransitional processing with STD3 ASCII rules, label/hyphen/joiner validation, bidi validation
and DNS length validation; never silently remove leading dots or empty labels. Validate A-labels and
decode them to the normalized Unicode form, checking round trips. Raw Unicode and equivalent xn--
labels receive identical findings. Normalize one final root dot for comparison. IDNA compatibility
forms that normalize to ordinary ASCII do not remain separate lookalike-domain findings.

### Recognition

After IDNA normalization, inspect each remaining non-ASCII code point. Report when its direct
mapping in the pinned confusables.txt is a nonempty sequence solely of ASCII letters A-Z/a-z. Do not
strip accents/combining marks, apply additional transliteration or recursively transform mappings.
This is an explicitly scoped table-based rule, not a claim of full UTS #39 security-profile
implementation. ASCII-only lookalikes and mappings to digits or punctuation are outside this rule;
mixed scripts alone are not a violation.

### Allowlist

allowed_hosts entries are bare DNS hostnames, Unicode or A-label form. Apply the same validation and
convert to lowercase canonical ASCII IDNA form, removing one final root dot. Match exact whole hosts
only, independent of URL port; no implicit subdomain matching, wildcards, URL syntax, whitespace
trimming or confusable-skeleton comparison. Invalid entries are configuration errors. Allowlisting
the ASCII lookalike does not allow the distinct Unicode host.

### Unreliable inputs

Use shared syntax decoding once, with source mapping, including valid percent-encoded UTF-8 in a
hostname; do not double-decode or reinterpret authority delimiters. Invalid URL authority, invalid
UTF-8, invalid IDNA/Punycode or characters unsupported by the pinned profile produce a located
problem and affected incomplete MD095 coverage, rather than guessed validity or a clean result.
Continue reliable independent checks.

### Reporting

Report once per destination hostname occurrence, identifying the source hostname and flagged code
points/mappings. For punycode or escaped source use the whole original hostname span if there is no
precise character span. Shared reference-definition ownership avoids repeating the same source
destination. Exact result encoding follows the accepted shared result schema.

### Execution

Embed reviewed versioned data with its SHA-256 and license; run entirely offline, with no DNS, HTTP,
reputation lookup or runtime table update. Upgrades require explicit table/profile version changes
and fixture review. Never rewrite a hostname.

## Unused suppressions

### Activation

Keep MD087 disabled by default, with no rule-specific options, and report-only. Apply the ordinary
MD087 state at each prospective finding anchor; later enabling does not retroactively activate
earlier comments. Never remove directives automatically.

### Candidates

Evaluate each deduplicated target rule in a valid, allowed mdchk:disable directive independently.
Ignore enable directives, invalid directives, globally inactive directives, protected literal
comments, permission-blocked targets and rules for which document directives do not apply (including
project-scoped MD074). Preserve their ordinary inspection/diagnostic treatment.

### Ownership

Use the existing actual per-rule state intervals: each allowed instruction owns its interval from
immediately after its closing marker until the next allowed instruction for that rule or EOF. A
repeated disable takes ownership even when state remains false. Credit a hidden finding only to the
disable owning its diagnostic start/EOF anchor. This is interval attribution, not independently
deleting each comment and replaying all remaining disables; repeated comments must not all be
credited for one observation.

### Counterfactual

Evaluate target-rule observations using the same immutable source, full-document parser context,
flavor, options, integrations, resource context and limits as ordinary analysis, without generating
edits or emitting hidden target-rule findings as active violations. Use a separate would-be-enabled
state initialized from effective configuration and honoring permitted enable directives while
ignoring disable directives. An observation is otherwise reportable only where that state is
enabled. A target configured off and never explicitly enabled therefore cannot make a disable useful
merely because forced evaluation could find violations.

### Demand

When MD087 is active at a candidate target token, obtain enough target-rule observations to decide
whether its interval contains an otherwise reportable finding, even if ordinary lint would skip that
target because it is disabled everywhere. Reuse analysis where possible; keep observational
execution separate from active/inactive counts. No required counterfactual evaluation when MD087 is
disabled. Do not expand resource authorization or execute code; this work consumes shared operation
budgets.

### Decision

A proved otherwise-reportable finding owned by the target disable makes that target used, even if
other parts of its interval are incomplete. Report unused only when every relevant would-be-enabled
part of the owned interval has complete reliable evaluation with no such finding, or the interval is
proved incapable of hiding one (for example configured-off with no enabling instruction). Proven
dialect inapplicability can establish no findings; missing required integration/context cannot.

### Uncertainty

If no hidden finding has been proved and required observations are unavailable because of an
unimplemented rule, uncertain parsing, missing resources/context, cancellation or exhausted limits,
withhold that unused claim and report affected MD087 coverage as incomplete. Reliable conclusions
for other targets remain available. Do not synthesize a target-rule violation or treat absence of
execution as a clean check. Existing configuration validation and operational-error precedence still
apply.

### Self reference

Exclude MD087 itself from suppression usefulness analysis; do not recursively run MD087 or count its
own findings as evidence that a suppression is useful. For a mixed disable naming MD087 and other
rules, analyze the other eligible targets normally. The MD087 exclusion is intentional scope, not
incomplete coverage.

### Diagnostics

Emit one MD087 finding per proved-unused target rule, anchored to its first source ID token in the
comment. Repeated names inside one directive remain deduplicated. The message identifies the target
rule; shared directive attribution links its effective interval. A mixed directive can therefore
contain both used and unused targets without labeling the entire comment unused. Keep
blocked/uncertain/used outcomes distinct in inspection.

### Boundaries

Use the shared diagnostic start-position rule, including file-wide and EOF anchors, rather than edit
spans or overlap. Preserve all existing permission, configured-option and protected-source behavior.
Recompute after any separately authorized safe fix against the candidate source.

## UTF-8 validation

### Mandatory gate

Validate the original document snapshot as strict UTF-8 before Markdown parsing, metadata
recognition or directive interpretation, independent of MD094 enablement and of whether any ordinary
rule is enabled. The gate is mandatory core input handling. On failure, retain the original bytes,
skip normal parsing and all content edits for that file, and continue independent selected files
under existing partial-failure behavior. Never guess an encoding, transcode or substitute
replacement characters.

### Configuration

Keep MD094 enabled by default with no rule-specific options. Accept shared enabled and
allow_directives booleans under ordinary type validation. enabled controls MD094 attribution only:
an invalid target document emits a single io error attributed to MD094 when enabled; when disabled,
emit the same encoding failure as a generic io error without a rule attribution. Disabling
attribution does not make malformed bytes acceptable or make the operation complete. With unresolved
configuration in inspection, use the generic encoding error rather than guessing enablement.

### Directives

Document directives cannot control the pre-parse UTF-8 gate or MD094 attribution. In a valid parsed
document, an otherwise valid directive naming MD094 is an inapplicable effect explained in
processing notes, without changing its effective state; other permitted targets in the same comment
retain their normal effects. When global directive interpretation is disabled retain existing
inactive-candidate behavior. Invalid documents have no interpreted directives, including comments
before the first bad byte. MD087 does not assess MD094 suppressions because directives do not apply
to it.

### Error location

Emit one encoding error per failed target snapshot, identifying the first byte where decoding can no
longer consume a valid UTF-8 scalar. For a malformed or truncated multibyte sequence, anchor at its
leading byte. Preserve a zero-based original byte offset as the authoritative location. A one-based
line/code-point-column point may be derived from the valid prefix using the shared line index; do
not invent a Unicode width for an invalid byte or transcode to compute it. If a range is needed it
is zero-width at that point; an original byte span may identify the first byte separately.

### Results

Store this as one operational io error, not also as an ordinary rule violation. Lint/fix/preview
become incomplete with error status and exit 13 unless an existing higher-precedence internal
failure applies. Decoding-dependent coverage remains unavailable with the encoding cause attached.
Ordinary Markdown findings cannot be produced from a partially decoded file. Exact error and
availability fields follow the [shared result schema](results.md#shared-result-schema).

### Inspection

If the raw file snapshot can be acquired and the report delivered, inspect retains status inspected
and exit 0, includes original byte size/digests and the encoding error, and marks complete false.
AST, parsed directives and decoding-dependent analysis are explicitly unavailable. Do not invent
active/inactive state where unavailable directive interpretation could change it; independently
resolved configuration data remains visible. Existing argument, target-acquisition, delivery and
internal error exits still apply. A byte-based newline index can validate requested line bounds
without treating invalid bytes as decoded characters.

### Valid inputs

Valid UTF-8 encoding of literal U+FFFD is not an encoding error. UTF-8 BOM bytes are also valid;
retain their independent metadata/style treatment. Validate byte encoding only, without
heuristically declaring a well-formed byte sequence to be another encoding based on apparent
content. Empty files are valid UTF-8.

### Resource and transport boundaries

Filesystem resources needed only for read-only context keep the existing generic resource io
decode-error and affected source-coverage contract; do not recursively run MD094 as a resource rule.
Standalone MCP accepts exact Unicode strings, with malformed raw UTF-8 or unpaired surrogate escapes
rejected at the existing request boundary before document analysis. Do not add binary/base64
payloads to create invalid-byte standalone fixtures. Project MCP uses the same target-file decoding
gate as CLI.

### Catalog and execution

Keep MD094 in the known catalog and preserve configured active/inactive accounting where state is
resolvable. Inspection identifies mandatory input validation separately from configurable rule
attribution, so a disabled MD094 entry does not imply that byte validation was skipped. Core
validation must exist in every build that accepts document bytes; it is not an optional unavailable
parser capability.

## List item spacing

MD076 compares exact source-owned direct sibling separator counts per actual
list. Tight=0, loose=1 and consistent=immutable exact first count, including >1.
Internal looseness and final List.Tight do not decide source spacing. Every
mismatch remains manual; rendering preservation only limits future correction.


MD077 compares all eligible actual paragraph contributions, nonliteral block
openings, real fence boundaries, definition first lines and actual GFM source
rows against the cumulative final item chain. Actual quote columns are neutral
only when consumed; descendant quotes select their first item-facing marker.
Postquote body indent, raw/comment regions and literal payload stay protected.
It is analysis-only and returns manual original-source evidence.

## Original TAB bytes (MD010)

MD010 emits one detached one-byte, one-scalar finding per eligible original TAB.
The constant message is `Tab character.`. Admitted positive spaces_per_tab is
analysis-neutral; parser structural tabs retain physical four-column phase.
CodeBlock actual Content plus genuine intrinsic indentation protects atomic
shared TABs by default; code_blocks admits only those code contributions.
CodeSpan remains protected in both modes. Actual literal contributions and
owned delimiters/tails protect HTML and CodeSpan without coarse prefix gaps.
Confirmed semantic raw bounds recover trimmed body TABs through actual closer
or EOF, subtracting only completely consumed actual container prefixes. Final
confirmed scalar prefix replay preserves residual TAB columns and physical phase;
empty first-item Prefix segments describe actual consumption beyond effective
padding. Image-alt raw tags cannot affect outer state. Whole recognized headers
remain opaque. See [MD010](../rules/whitespace/md010.md) for exact boundaries.
All findings are manual; MD047 alone can produce corrections.

MD012 specializes blank body runs: ASCII space/TAB after each physical line's
actual final prefix bytes, globally consecutive across container changes, one
first-excess remaining-body finding per run. Actual protected code/raw/header
lines break runs, including empty payload lines with retained endings. Count real
BOF/EOF blanks without a virtual line. Present zero-width ranges are manual source
evidence, never edit authority. See [MD012](../rules/whitespace/md012.md).

MD013 specializes original physical-source scalar width, including prefixes and syntax, with four-column TAB stops. Protected bytes occupy width but cannot anchor; the first eligible overflowing scalar is the sole manual finding per physical line. Code/literal contribution gaps remain ordinary; table/heading false options deliberately exempt whole physical lines. See [MD013](../rules/whitespace/md013.md) for exact raw, destination and option boundaries.


MD027 specializes final ordinary Paragraph contributions and actual prose-facing quote markers. Only wholly ASCII SPACE separators participate; any TAB exempts the whole separator. One manual range covers only excess spaces after retaining the first. Newly opening inline code and ordinary markup can qualify, HTML/comment-only contributions cannot, and already-open multiline literal/raw continuation stays protected through closer+tail. See [MD027](../rules/whitespace/md027.md).


MD028 compares consecutive final quote siblings with the same direct parent. Only an entire ordinary ASCII blank-body gap qualifies; actual literal/raw/header ownership and removed definitions intervene. Each physical line reports its remaining original body, including a present empty span after an atomic prefix-owned TAB. See [MD028](../rules/whitespace/md028.md).

### Horizontal-rule body spacing

MD065 compares actual unprotected thematic breaks only within their immediate
Document, BlockQuote or ListItem body. Retained reference definitions remain
source participants; recognized headers are outside root body edges. Only actual
owner/ancestor prefixes may be removed when checking the immediately adjacent
physical ASCII SPACE/TAB blank. Deeper syntax and opaque payload blanks remain
content. Emit each deficient side separately, including both sides of a shared
gap, at present zero-width original points: before own indentation after actual
prefixes, and after trailing content before the whole physical ending. Full raw
and body context precedes START suppression and inspection selection. This rule
is analysis-only and grants no insertion/correction authority.


## Paragraph emphasis population (MD081)

MD081 is default disabled, RequiresSyntax and permanently NoFix for CommonMark
and GFM. Each actual Paragraph independently counts non-CommonMark-whitespace,
non-Cc semantic Text scalars; actual Emphasis/Strong ancestry is a boolean union.
Escapes, breaks, code, images, headings/tables, raw/comment payload and recognized
headers are excluded from both populations. All actual link-label origins count
once. Entity/Normalized/PartialTab values are consumed semantically without a new
decode; Cf, LS/PS, combining and astral scalars retain individual contributions.

D>0 and inclusive total D>=minimum precede strict binary64 E/D>ratio, without an
epsilon or multiplication substitution. One manual finding owns the full original
trimmed Paragraph Content hull, including markup and interior container gaps.
The fixed message is `Paragraph contains excessive bold or italic text.` Full
original counts precede START suppression and inspection filtering. See the
[MD081 rule page](../rules/formatting/md081.md) for options and exact exclusions.

## Embedded HTML (MD033)

The [MD033 source policy](../rules/html-and-comments/md033.md) reports each
complete named parser-owned tag separately with full original geometry and the
fixed message `Embedded HTML tag is not allowed.` Opaque unnamed tokens, actual
four-name raw payload, code/metadata and whole Images remain protected; actual
Link-label HTML is eligible. Form 1 uses any literal parser raw closer; other
owned tokens preserve source order/global lifetimes without entering quoted or
opaque payload. Failed owned named context withholds the entire inventory with
coverage. The existing permissive allowed_tags string list uses whole-name ASCII
folding only, applied after inventory/raw context. Default OFF and NoFix remain;
MD047 alone produces corrections.

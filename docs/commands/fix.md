# mdchk fix

Current implementation: `fix` applies validated MD047, MD009, MD010, MD012, MD005, MD019, MD021, MD003, MD022, MD023, MD032, MD029, MD030, MD063, MD007, MD077, MD076, MD004, MD027, MD035, MD064, MD085 and MD089 corrections on Linux,
macOS and Windows, from a build without cgo. Other operating systems refuse writes
with I/O status 13. `fix --dry-run` supports all four formats and writes nothing. Text/JSON
include proposed diffs; editor formats report only remaining candidate findings.
MD047 supplies the first production correction: minimal terminal-newline changes
with independently checked preservation and shared directive permissions. Unsafe
protected EOF contexts remain manual, with explicit deferral reasons. In GFM,
adding an ending to literal task spelling such as `- [x]` at EOF would turn
visible text into task syntax. MD047 leaves that case manual with false
`fix_available`; choose the intended text or task meaning before editing.
MD009 removes trailing whitespace, or normalizes a real hard break to exactly
`hard_break_spaces` spaces. Literal code stays manual, and suffixes whose correction
could change meaning or grow the file too far are deferred with reason
`meaning-not-proven` or `expansion-limit`; see
[its correction policy](../rules/whitespace/md009.md#correction-policy). All fixers
run in one `fix` and converge across rounds. When an MD009 removal at physical EOF
touches MD047's insertion, the removal applies first and MD047 adds the final
newline in the next round, so `foo<SPACE><EOF>` becomes `foo<NEWLINE>`. When two
corrections conflict, one wins by catalog rule order, with MD047 always last, and the
other waits with reason `conflict-waiting` until the next round proposes it again. A manual MD047 finding doesn't block other corrections in the file. A document
opening with an unrecognized block has no such conflict, because MD009 proposes no edit
there: MD047 adds its line ending and the suffix stays. See [Unrecognized leading blocks](#unrecognized-leading-blocks).


Apply safe corrections to Markdown files and report problems that still need attention. Preview changes with `--dry-run` before saving them.

```text
mdchk fix [options] [paths...]
```

The examples below assume the [current reduced setup](../index.md#get-started-with-this-build):
a CommonMark+MD047 configuration and readable fixture paths. Without it,
default GFM and CommonMark both run every default check with complete coverage. Apply additionally requires the verified native adapter above.

[MD088 ASCII-punctuation analysis](../rules/formatting/md088.md) is available for CommonMark and GFM,
disabled by default with sole string option `style: ascii`. Invalid styles and
unknown `replacements` reject even when disabled. Findings are manual in Pass 1;
PlannedFix retains future correction scope. No MD088 edits or previews are emitted.

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, fix processes the working directory. It uses the same selection and exclusion behavior as [lint](lint.md).

```sh
mdchk fix --dry-run README.md docs/
```

## Options

Options may appear before or after path arguments; `--` ends option parsing.
Valid help/version requests bypass target requirements, while invalid option
syntax or scope still returns usage `11`. See the linked global options policy.

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../engineering/limits.md); repeat for multiple limits. A saved run applies it to [each half](../engineering/limits.md#saved-fix-runs) |
| `--config PATH` | Select a configuration inside the operation's discovery boundary |
| `--format FORMAT` | Choose `text` (default), `json`, `vscode` or `github` |
| `--dry-run` | Compute corrections and report the predicted result without writing files |

The [global options](../index.md#global-options) also apply. See [configuration](../configuration.md) and [result formats](../output-formats.md) for details.

## Preview and apply corrections

```sh
mdchk fix docs/ --dry-run
mdchk fix docs/
mdchk fix docs/ --dry-run --format json
```

Text and JSON previews include proposed diffs when a registered correction
passes shared candidate validation. MD047 final-newline, MD009 trailing-whitespace, MD010 tab-character, MD012 blank-line-run, MD005 sibling-list alignment, MD019 and MD021 heading-spacing, MD003 heading-style, MD022 heading blank-line, MD023 heading-indentation, MD032 list blank-line, MD029 numbered-list prefix, MD030 list-marker-spacing, MD063 heading-capitalization, MD007 bullet-indentation, MD077 list continuation indentation, MD076 list-item-spacing, MD004 bullet-marker, and MD027 blockquote-spacing, MD035 horizontal-rule-style, MD064 repeated-prose-space, MD085 paragraph-continuation, and MD089 CJK/Latin-spacing corrections are implemented. MD004 corrects a list only when its structure stays the same: a bullet list directly beside another bullet list, separated only by blank lines or nothing, would merge, so its findings stay reported with `fix_available: false`. MD001, MD069, MD013, MD028, MD037, MD038, MD081, MD088, MD018, MD020, MD024, MD025, MD026, MD036, MD041, MD043, MD080, MD082 and MD093 remain report-only, with no edits or previews for their findings.

[MD019](../rules/headings/md019.md) and [MD021](../rules/headings/md021.md)
corrections remove excess ASCII spaces from a heading delimiter gap: the second
through last space are deleted and one stays, so `##  Title  ##` becomes
`## Title ##`. A gap containing any tab stays a manual finding with
`fix_available: false`. Each gap is judged alone, so `#  x<TAB>#` still gets its
opening gap corrected. When both rules report the same opening excess, one edit is
applied and the change credits both rules. Enablement and directives apply to each
rule separately, so if one ID is disabled the other can still correct a shared gap.
A document's eligible corrections, including MD047's, are proposed
together. If MD047 cannot be applied, the heading corrections still are, and MD047
stays manual.

[MD003](../rules/headings/md003.md) corrections change only heading delimiters
to the expected style and keep the level and inline content: `# A` becomes
`# A #` under `atx_closed`, `## A ##` becomes `## A` under `atx`, `# A` becomes
`A` over a `===` underline under `setext`, and a single-line Setext heading
becomes `## B`. Headings the policy can't rewrite safely, such as H3–H6 under
`setext` or multiline Setext text, stay manual with `fix_available: false`.
MD003 edits share heading lines with the other heading and whitespace
corrections, so they wait for a round in which no other correction is proposed;
one `fix` run still applies all of them.
[MD022](../rules/headings/md022.md) corrections insert the missing blank lines
around a heading, so `Text<NEWLINE>## Install<NEWLINE>Text` becomes
`Text<NEWLINE><NEWLINE>## Install<NEWLINE><NEWLINE>Text`. Inserted lines copy the
heading line's blockquote prefix without trailing whitespace and use the nearest
original line ending. Headings in list items, lazy or mismatched neighbours, and
neighbours that are or touch code, raw HTML, comments or front matter stay manual
with `fix_available: false`.
[MD023](../rules/headings/md023.md) corrections delete the leaf indentation on
every physical heading line, after the line's container prefixes and their
padding, so ` ## Install` becomes `## Install` and `>  # H` becomes `> # H`. A
Setext heading's text, continuation and underline lines are corrected together or
not at all. A heading with a partially consumed tab or a lazy line missing its
container prefix stays a manual finding with `fix_available: false`, and so does
one whose deletion lies in protected or directive-suppressed context.
[MD032](../rules/lists/md032.md) corrections insert one blank body line before
or after a list, repeating the enclosing quote markers and indentation without
trailing whitespace, so `> Text` followed by `> - One` gets a `>` line between
them. The line uses the previous line's ending. A finding whose blank line would
make a tight list loose, whose container prefix holds a tab, or that follows an
HTML block stays manual with `fix_available: false`. A reparse must show the
same lists, items, contents and tightness, or the correction is deferred.
[MD063](../rules/headings/md063.md) corrections uppercase every required lowercase
initial of a heading together, so `## installation guide` becomes
`## Installation guide` under sentence style. A heading with any initial that is
written as an entity or escape, has no single titlecase form, lies in protected
context or autolink text, or could be part of a configured `proper_names` phrase stays a manual
finding with `fix_available: false`. Nothing is ever lowercased.
[MD005](../rules/lists/md005.md) corrections re-indent a misaligned sibling list
marker to match the first item of its group, so `- One` followed by ` - Two`
becomes `- One` followed by `- Two`. Only the spaces before the marker change.
Tabs, items with continuation, lazy or child lines, and items that would need
spaces inserted stay manual with `fix_available: false`. MD005 corrections wait
for a round with no MD009, MD022, MD023 or MD032 edit and apply later in the
same run.
[MD012](../rules/whitespace/md012.md) corrections remove the excess lines of a
blank run, each whole with its own ending, so exactly `maximum` remain:
`One\n\n\n\nTwo\n` becomes `One\n\nTwo\n` at `maximum: 1`. A run whose excess
lines carry different container prefixes, whose removal would change the parse
or create an MD022, MD031, MD032 or MD065 finding stays manual with
`fix_available: false`. MD012 corrections wait for a round with no MD076,
MD004, MD019, MD021, MD022, MD032, MD023, MD029, MD030, MD009, MD005, MD007 or
MD077 edit, and apply later in the same run; MD063, MD010, MD003 and MD027 wait for MD012.
[MD035](../rules/formatting/md035.md) corrections replace only a break's
first-to-last marker envelope with `---`, `***` or `___`, or with the first
break's exact pattern under `consistent`, so `---\n\n***\n` becomes
`---\n\n---\n`. A break whose replacement would change the parse, such as
`---` directly under a paragraph line becoming a Setext underline, stays manual
with `fix_available: false`. MD035 corrections wait for a round with no other
edit, after MD010, MD003, MD027 and MD089, and apply later in the same run; MD064 waits
for them in turn.
[MD029](../rules/lists/md029.md) corrections replace a flagged marker's digits
with the expected value in plain decimal, so `1. a` then `3. b` becomes `1. a`
then `2. b`. A wider or narrower number takes its width from, or gives it to,
the all-space padding so the content column stays put. Tab padding, too little
padding, a value over nine digits, protected content, directives, front matter
and any parse change keep the finding manual with `fix_available: false`.
MD029 corrections wait for a round with no MD022, MD023 or MD032 edit, and
MD009, MD005 and MD063 corrections wait for MD029; all apply in the same run.
[MD007](../rules/lists/md007.md) corrections move a misindented bullet item, with
its continuation lines and nested content, to the expected column by adding or
removing leading spaces; blank lines stay as they are. Items whose moved lines hold a
tab, a blockquote prefix, a lazy continuation or protected content stay manual with
`fix_available: false`. A shift whose reparse would change list structure is reported
under `deferred_fixes` with reason `meaning-not-preserved`. MD007 shifts wait for a
round with no MD009, MD022, MD023, MD029, MD032 or MD005
edit, MD063 waits for them, and a nested finding is regenerated after its ancestor
moves. A single `fix` run still reaches the fully corrected result across its
rounds. A shift that would add more than 16 columns stays manual, so a large
`indent` can't multiply a file's size.
[MD077](../rules/lists/md077.md) corrections fix list continuation indentation
made only of ASCII spaces: a surplus deletes the reported extra spaces and a
deficit inserts the missing ones, so `- One\n\n   Text\n` becomes
`- One\n\n  Text\n` and `- - One\nText\n` becomes `- - One\n    Text\n`.
Indentation with a tab, a line with a quote prefix (present or lazily absent),
protected content, a line a directive governs and any document opening with a
front matter block stay manual with `fix_available: false`. An edit whose reparse
would move the line to another item or block is deferred and the finding stays
reported; one that conflicts with another proposal waits for a later round. MD077 corrections wait for a
round with no MD009, MD022, MD023, MD029, MD032, MD005 or MD007 edit, and MD063
waits for them; all apply in the same run.
[MD030](../rules/lists/md030.md) corrections replace the separator after a list
marker with exactly `spaces` ASCII spaces, so `-  One` becomes `- One`. Only an
item whose content is all on the marker line, with a spaces-only separator, no
code-indentation fallback and `spaces` from 1 to 4, is corrected. Continuation
lines, children, same-line nested items under the outer marker, tabs and any
change that would pull a following line into the item stay manual with
`fix_available: false`.
MD030 corrections wait for a round with no MD022, MD023, MD029 or MD032 edit,
and MD005, MD007, MD077, MD063 and MD003 corrections wait for MD030; all apply
in the same run.
[MD076](../rules/lists/md076.md) corrections make the blank lines between sibling
list items match the configured `style`. A missing blank line is inserted at the
start of the next item's marker line, carrying the same ancestor prefixes without
trailing spaces, so `> - A` followed by `> - B` gains a `>` line between them. An
extra blank line is deleted whole, with its prefix and line ending. A gap whose
prefixes differ between lines, hold a tab or follow a lazy continuation stays
manual with `fix_available: false`, as does a gap after an unclosed fence or in
protected content, front matter or a disabled interval. MD076 runs first: every
other correction except an already eligible MD047 edit waits for a round with no
MD076 edit, and all apply in the same run. MD004 runs next and shares its round
only with MD009, MD019, MD021 and MD047; the other corrections follow in later
rounds of the same run.
[MD010](../rules/whitespace/md010.md) corrections replace a TAB in paragraph,
heading or table-cell text with exactly `spaces_per_tab` spaces, so
`Text<TAB>continued` becomes `Text<SPACE><SPACE><SPACE><SPACE>continued`. Each
TAB is its own one-byte edit. Leading indentation, container prefixes, code,
code spans, HTML, raw-tag bodies, front matter, link destinations and titles,
reference definitions, table delimiter rows, cell trimming, image descriptions
and line-end whitespace stay manual with `fix_available: false`. A
`spaces_per_tab` above 8 defers every TAB with reason `expansion-limit`. MD010
corrections wait for a round with no other edit, MD009 and MD047 included, and
every other correction waits for them, so `a<TAB>b<SPACE><NEWLINE>c<TAB>d<EOF>`
becomes `a<SPACE><SPACE><SPACE><SPACE>b<NEWLINE>c<SPACE><SPACE><SPACE><SPACE>d<NEWLINE>`
across the rounds of one run.

[MD027](../rules/whitespace/md027.md) corrections delete the excess spaces after
the prose-facing blockquote marker, so `>   Text` becomes `> Text` and one SPACE
stays. Separators holding a TAB, lazy lines, code and HTML continuations, and
documents opening with an unrecognized `---` or `+++` block stay manual with
`fix_available: false`. MD027 runs last: it waits for a round in which no other
correction is proposed, and a withheld MD047 edit waits for it; all apply in the
same run.

[MD064](../rules/whitespace/md064.md) corrections delete exactly each reported
excess span of spaces between prose words, keeping the first `maximum` spaces, so
`One  two` becomes `One two`. A finding under a disable directive or in protected
content, or in heading text, whose anchor a deletion would change, stays manual
with `fix_available: false`, and its bytes are left alone. MD064 waits even for
MD027 and MD010: its edits wait for a round in which no other rule proposes an
edit, and only an eligible MD047 edit shares that round, so
`One  two<SPACE><SPACE><SPACE><NEWLINE>end` converges in one run, with MD009 and
MD047 first, and spaces a TAB replacement adds are collapsed in a later round of
the same run.

[MD085](../rules/whitespace/md085.md) corrections delete the reported surplus
SPACE/TAB indentation of a paragraph continuation line, so
`a<NEWLINE><SPACE><SPACE>b<NEWLINE>` becomes `a<NEWLINE>b<NEWLINE>`. A line that
would start a block, or become a setext underline or table delimiter row, stays
manual with `fix_available: false`, as does a line in a disabled interval or a
document opening with an unrecognized block. MD085 shares its round with MD009
and MD047, so `a<SPACE><NEWLINE><SPACE><SPACE>b<EOF>` becomes
`a<NEWLINE>b<NEWLINE>` in one run; MD012, MD010, MD027 and MD064 wait for it.

[MD089](../rules/whitespace/md089.md) corrections insert one ASCII SPACE at each
point between adjacent CJK and Latin text in prose, so `版本2` becomes `版本 2`.
Code, raw HTML, autolinks, bare GFM links, link destinations and titles, front
matter and disabled intervals keep their bytes. Heading text, whose anchor
fragments may reference, collapsed and shortcut reference labels, and documents
opening with an unrecognized `---` or `+++` block stay manual with
`fix_available: false`. A group its checker can't prove is reported under
`deferred_fixes`. MD089 runs after MD027, in a round in which no other
correction is proposed, and a withheld MD047 edit waits for it; MD064 waits for
MD089 edits. All apply in the same run.

[MD081 paragraph-emphasis analysis](../rules/formatting/md081.md) is available for CommonMark and
GFM, disabled by default and permanently report-only, with no edits or previews.

Other corrections remain planned. VS Code and GitHub describe findings remaining
in proposed content for dry runs, without diffs or raw corrected Markdown. Use
[lint](lint.md) when highlighted locations must match files currently on disk.

Corrections preserve meaning and unrelated content. For example, fix can add a missing final newline while preserving intentional hard breaks. It leaves code examples and ambiguous changes alone, and front matter is kept as described in [Unrecognized leading blocks](#unrecognized-leading-blocks). A skipped heading level or missing code language needs a decision rather than a guessed correction.

After corrections, mdchk checks the resulting content and reports remaining findings at their new locations. Apply reports observed saved snapshots and confirmed written changes; dry-run reports candidate snapshots and proposed changes. Refused or unavailable saved observations are never replaced by predicted clean content.

### Unrecognized leading blocks

Recognized front matter stays byte-identical, and the body after it is still corrected. Recognition is off by default, though, and an unrecognized header would otherwise read as Markdown, so a correction could rewrite a value inside it.

A document that opens with a complete `---` or `+++` block that isn't recognized, because `frontmatter.enabled` is false or the block's format isn't in `frontmatter.formats`, gets no MD003, MD004, MD005, MD009, MD010, MD012, MD019, MD021, MD022, MD023, MD029, MD030, MD032, MD035, MD063, MD007, MD077, MD076 or MD064 corrections anywhere. That covers the body as well as the block. Their findings are still reported, with `fix_available: false` and no `deferred_fixes` record.

mdchk can't tell a real header from Markdown of the same shape, so a leading thematic break paired with a later one also triggers this. An opener with no closer doesn't. Set `frontmatter.enabled: true` to restore body corrections for documents with real headers.

MD047 still applies: it adds a missing final line ending and removes excess ones. A trailing space at physical EOF therefore stays while the line ending is added, so `body<SPACE><EOF>` becomes `body<SPACE><NEWLINE>`.

Production cross-file rule/resource consumers remain planned. Their accepted
post-fix contract uses one candidate view containing all proposed target changes,
with referenced resources read-only. Current CLI publication conservatively
defers coupled target changes rather than claiming such checks ran. After project writes, results use the actual saved view: successful writes plus confirmed unchanged snapshots. If writes fail or a dependency changes, mdchk reports the error and affected incomplete coverage instead of treating every proposal as saved. See the [resource snapshot contract](../engineering/resources.md#snapshots-caching-fixes-and-limits).

## Understand the result

Exit code `0` means analysis completed and no violations remain after corrections. Code `10` means a complete check found remaining problems, including those that cannot be safely fixed. Code `15` means post-fix analysis is incomplete; available safe changes and findings remain in the report, and uncertain regions remain protected. For a dry run, those codes describe the predicted content; even a `0` can accompany changes that still need to be applied.

Errors can occur after other files have changed. Inspect both successful changes and failures before retrying. If a file changes during fixing, mdchk refuses stale edits rather than overwriting the newer contents. See the [exit code guide](../exit-codes.md) for usage, configuration, I/O, and internal error codes.

An invalid original UTF-8 target retains its encoding I/O errors and any retention
omission, and receives no content correction. Independent valid targets continue analysis and eligible
MD047 correction; proof-dependent components remain unchanged and explicitly
unavailable. Preview reports only independently approved proposals; supported
apply reports actual saved snapshots. Operating systems outside Linux, macOS and
Windows retain an explicit write refusal. Encoding-local incompleteness does not waive runtime or proof checks.

MD033 embedded-HTML analysis is available for CommonMark and GFM, disabled by
default and permanently NoFix. Full original tag ranges remain manual, including
candidate and saved analysis after an independent MD047 newline correction.
MD033 produces no edits, eligibility, previews or corrected contents. Apply on an unsupported operating system returns an early
complete I/O refusal 13 with empty analysis/effects and unchanged files; a build
without cgo applies normally.

## MD086 manual findings

MD086 unmatched-comment analysis is enabled by default for CommonMark and GFM,
with no rule-specific options and permanent NoFix. It emits no edits, eligibility,
previews or corrected content. An unmatched opener in an ended quote owner remains a manual MD086 finding when
MD047 adds an eligible final LF to later ordinary text. An unclosed comment at
physical EOF protects that EOF, so MD047 remains deferred and MD086 remains manual.
Candidate and saved diagnostics retain their exact source identities.

## MD087 suppression audit

MD087 analysis is available for CommonMark and GFM, disabled by default, with
no options and permanent report-only support. Inspection's optional per-effect
reason distinguishes used, unused, uncertain, self-excluded and inactive-audit
targets while retaining ordinary state, permission, effect and provenance.
Join each effect's rule to its first matching named token and the directive ID
to owned rule intervals; final intervals own EOF. Full-document conclusions
precede focus, retaining preceding owners as context_only.

The audit produces no edit or preview. A separately authorized MD047 correction
recomputes the audit against candidate source with the original residual budget and
against actual saved source under the [saved-run allowance](../engineering/limits.md#saved-fix-runs). Hidden observations bypass correction eligibility; ordinary
active MD047 corrections retain their established behavior.

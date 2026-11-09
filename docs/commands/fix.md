# mdchk fix

Current implementation: `fix` applies validated MD047, MD009, MD005, MD019, MD021, MD003, MD022, MD023, MD032 and MD063 corrections on Linux,
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
touches MD047's insertion, both are deferred with reason `conflict`. The MD009 finding
then has `fix_available: false`, but the MD047 finding can still show
`fix_available: true` and be deferred. Both stay unchanged, and you decide which edit
to make. A manual MD047 finding doesn't block other corrections in the file. A document
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
passes shared candidate validation. MD047 final-newline, MD009 trailing-whitespace, MD005 sibling-list alignment, MD019 and MD021 heading-spacing, MD003 heading-style, MD022 heading blank-line, MD023 heading-indentation, MD032 list blank-line, and MD063 heading-capitalization corrections are implemented. MD001, MD004, MD007, MD029, MD030, MD069, MD076, MD077, MD010, MD012, MD013, MD027, MD028, MD064, MD085, MD089, MD035, MD037, MD038, MD081, MD088, MD018, MD020, MD024, MD025, MD026, MD036, MD041, MD043, MD080, MD082 and MD093 remain report-only, with no edits or previews for their findings.

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
for a round with no MD009 edit and apply later in the same run.

[MD081 paragraph-emphasis analysis](../rules/formatting/md081.md) is available for CommonMark and
GFM, disabled by default and permanently report-only, with no edits or previews.

Other corrections remain planned. VS Code and GitHub describe findings remaining
in proposed content for dry runs, without diffs or raw corrected Markdown. Use
[lint](lint.md) when highlighted locations must match files currently on disk.

Corrections preserve meaning and unrelated content. For example, fix can add a missing final newline while preserving intentional hard breaks. It leaves code examples and ambiguous changes alone, and front matter is kept as described in [Unrecognized leading blocks](#unrecognized-leading-blocks). A skipped heading level or missing code language needs a decision rather than a guessed correction.

After corrections, mdchk checks the resulting content and reports remaining findings at their new locations. Apply reports observed saved snapshots and confirmed written changes; dry-run reports candidate snapshots and proposed changes. Refused or unavailable saved observations are never replaced by predicted clean content.

### Unrecognized leading blocks

Recognized front matter stays byte-identical, and the body after it is still corrected. Recognition is off by default, though, and an unrecognized header would otherwise read as Markdown, so a correction could rewrite a value inside it.

A document that opens with a complete `---` or `+++` block that isn't recognized, because `frontmatter.enabled` is false or the block's format isn't in `frontmatter.formats`, gets no MD003, MD005, MD009, MD019, MD021, MD022, MD023, MD032 or MD063 corrections anywhere. That covers the body as well as the block. Their findings are still reported, with `fix_available: false` and no `deferred_fixes` record.

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

# Explicit integration contexts

Issue #174 implements the shared context and policy boundary for the accepted
[integration contract](../../docs/engineering/integrations.md). It does not
advertise concrete Quarto/MkDocs recognition to CLI, inspect or MCP. The existing
configuration capability checks remain in force.

`NewScope` borrows one existing resource Operation, its exact immutable View and
the adapter's explicit selected settings. `Operation.OwnsView` is an identity
check, and `View.TargetCount` permits charging an identity lookup without copying
targets. Neither creates resource, target, inventory or write authority. A
selected `Document` has private source/view proof; equal bytes from another Source
or another operation/view cannot borrow it. Unavailable Saved snapshots do not
fall back to originals. Scope creates no reader, worker, cache or new allowance.
Owner checks compare constant-size Source tokens rather than whole source byte
strings. Tokens remain identity only; they grant no parse or content admission.

Every working policy call shares the operation's work/node/time budget and caller
context. Independently observed caller cancellation, operation deadline and the
first actual quota remain inspectable together. A stopped context is unavailable,
never a clean missing label/nav/page result. Callers retain the returned actual
error alongside the typed status for the existing result/cause producer. This
package adds no result wire fields, diagnostic constructor or error exit class.

## Quarto policy versus parser facts

`QuartoHeader` evaluates an explicitly supplied, exact-source **candidate info
span**. It does not discover fences, validate parser ownership, decide container
protection or grant a complete cell collection. Its `SupportedCandidate` result
is a policy decision. The future parser-backed producer must extract the actual
fenced nodes and initial option-comment block before it can produce `Cell` facts.
There is no public `Cell` or available `QuartoContext` constructor from raw nodes,
caller-supplied enums, headers or decoded-label records.

Activation is only `QuartoEnabled`, independently of flavor/frontmatter/rules or
file extension. With recognition disabled, the candidate policy is ordinary and
the context is inactive. With recognition enabled but no concrete producer yet,
`Scope.Quarto` returns unavailable. That truthful provider state is intentional;
the actual parser-owned collection is delivered by #175.

The executable policy fixtures pin these initial forms:

| Input | Candidate policy |
| --- | --- |
| `{r}`, `{python}`, `{julia}` | Supported language candidate |
| `{r name}`, `{r, name}` | Literal R positional label |
| `{r label="a b"}`, `{r, label = 'é-図'}` | Quoted literal R label |
| `{r name, echo=FALSE, eval=FALSE}` | Static options, still a label candidate |
| Ordinary `python` info | Ordinary language example |
| `{{python}}` or a producer-proved enclosing code example | Escaped/protected example |
| Unsupported language, malformed braces, dynamic options or duplicate declarations | Unsupported/uncertain executable-looking context |

Names are exact and case-sensitive. R options accept literal Boolean/null,
numbers and simple quoted strings; known quoted escapes are backslash, quote,
newline, carriage return and tab. Unsupported static/engine-specific forms remain
explicitly uncertain rather than executing or inferring them. Optional whitespace
around an assignment is supported. No option evaluation, code execution or label
renaming occurs. The future provider uses these decisions together with actual
parser facts; the table is not a claim of complete upstream Quarto compatibility.

`Label` is a decoded policy input with an original `Location`, not proof of an
authored option. It distinguishes absent, string (including empty), non-string,
ambiguous and unknown. `ResolveLabels` preserves header and body declarations and
applies body precedence including an explicitly blank string. An invalid or
unknown body does not recover a header label. Strings are neither trimmed nor
coerced. Effective-label availability and `DeclarationsComplete` are separate: a
known body override does not erase uncertainty in the overridden header. Header
candidate decisions retain each reliably decoded duplicate declaration for later
independent whitespace checks. Complete provider-owned Cells can likewise retain
all declared labels and exact mapped spans without forcing them into one value.

## MkDocs policy versus acquired context

`MkDocsPolicy.Enabled` is the effective MD074 enablement, not a second integration
switch. The pinned static reference is MkDocs 1.6.1. `ConfigPath` is a canonical
resource-root-relative key, default `mkdocs.yml`. `PlanDocsRoot` plans a literal
relative `docs_dir` beside that config, default `docs` when absent. Explicit empty
and absent values remain distinct. The result keeps the existing Root and grants
no directory existence or native access. Escapes and native/URL roots are rejected.

`LeadingSlash` applies only the extra integration rule: skip a single leading
slash by default; with `CheckAbsolute`, interpret it as a source path below the
planned docs root. Protocol-relative URLs remain untouched for shared remote
classification. This is not a second URL decoder. The #176 producer must then use
the shared resource normalizer for once decoding, query/fragment-independent
existence, blocked native/file paths, exact names, no-follow reads and the one
configured index. `WithinDocsRoot` enforces the lexical docs-subroot after that
normalization. It does not infer `site_url`, `use_directory_urls`, generated
directory URLs or `.html`-to-Markdown rewrites.

`Navigation` validates and snapshots a semantic policy tree with original config
definition/entry spans, not a Markdown Link or a claimed YAML parse. Absent/null
nav is automatic; a literal list, including `[]`, is manual; unsupported syntax
is unknown. Path strings, title/path records and nested titled sections preserve
source order. Copies charge actual shared work/nodes and reject foreign Source or
out-of-definition spans; cycles/depth cannot evade limits. Mutable inputs and
returned slices cannot modify the retained tree.

The concrete safe-YAML/config acquisition provider is #176. It must permit
unrelated ordinary settings and bounded acyclic aliases, retain exact spans, and
report duplicate keys/cycles/tags/includes/INHERIT/environment/dynamic contexts as
affected uncertainty without executing them. `StaticNavigation` describes plugin
policy: built-in search and literal `enabled:false` plugins do not introduce
dynamic navigation; other active plugins/hooks make dependent negative claims
uncertain. These pure policy inputs do not themselves prove YAML interpretation.

`EligibleInventory` has no public constructor or completeness setter. Complete
resource names, selected lint targets and queried-key caches cannot create it.
The #176 trusted producer establishes complete authorized **build-eligible** page
facts for the exact config Source/scope: included, excluded, draft, not-in-nav and
README/index conflicts under the pinned defaults and Gitignore-like MkDocs
patterns. The mdchk config pattern grammar is not MkDocs eligibility syntax.
`CanCheckOmissions` requires manual navigation, enabled omission policy, static
context and that exact complete inventory; partial or foreign inventories fail
closed. Automatic navigation has no manual omissions.

Until the real provider exists, enabled `Scope.MkDocs` is unavailable and disabled
is inactive. Standalone acquisition will use the same exact YAML file entry at
`resources[config_path]` plus docs/inventory in the existing resource namespace;
no competing context request, host fallback, network or new transport is added.

## Explicit slides

`Slides` annotates an exact selected Document using the existing configuration
pattern grammar, case-sensitive direct `MatchFirstBounded` semantics and the
selected-config-directory base (virtual root for standalone). The trusted adapter
supplies the normalized logical name and established containment; this mapping
grants no filesystem authority. A directory-only pattern does not annotate all
descendants through exclusion-ancestor semantics. Matching is one bounded scan
over the complete pattern list, including actual visited memo-row allocations.

The deterministic first matching pattern and copied configuration Origin are
retained. Empty/no-match/outside-base is an ordinary selected document; invalid
patterns remain configuration errors; unavailable Saved data or actual stops are
unavailable, not ordinary. Filename conventions, frontmatter, Quarto outputs and
separators do not implicitly classify slides. Annotation changes no selection,
flavor, frontmatter, integration or rule setting. MD090 and actual inspect/CLI/MCP
presentation remain later consumers.

No production MD074/MD078/MD079/MD090 callbacks, findings, fixes, TOC, transport or
writer changes are implemented here. #175/#176 and later #177 must prove the real
providers and complete consumer journeys under these shared contracts.

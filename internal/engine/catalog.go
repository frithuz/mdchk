package engine

// Catalog returns an independent snapshot of all 89 accepted rule contracts.
// Planned corrections are metadata only: no executable callbacks or correction
// implementations are installed. Defaults never depend on a build's providers.
func Catalog() []Metadata {
	return []Metadata{
		{ID: "MD001", Name: "Heading increments", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"frontmatter_title": boolean(false)}},
		{ID: "MD003", Name: "Heading syntax style", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "atx", "atx_closed", "setext")}},
		{ID: "MD004", Name: "Bullet marker style", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"marker": choice("consistent", "asterisk", "plus", "dash")}},
		{ID: "MD005", Name: "Alignment of sibling list items", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD007", Name: "Bullet list nesting indentation", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"indent": integer(2, 1), "start_indented": boolean(false)}},
		{ID: "MD009", Name: "Whitespace at line ends", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"hard_break_spaces": integer(2, 2), "strict": boolean(false), "code_blocks": boolean(false)}},
		{ID: "MD010", Name: "Tab characters", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"code_blocks": boolean(false), "spaces_per_tab": integer(4, 1)}},
		{ID: "MD011", Name: "Swapped link delimiters", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD012", Name: "Runs of empty lines", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"maximum": integer(1, 0)}},
		{ID: "MD013", Name: "Maximum line width", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"length": integer(80, 1), "code_blocks": boolean(false), "tables": boolean(false), "headings": boolean(true)}},
		{ID: "MD014", Name: "Shell prompts without command output", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"languages": list("sh", "shell", "bash", "zsh", "console")}},
		{ID: "MD018", Name: "Missing space after opening hashes", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD019", Name: "Extra spaces after opening hashes", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD020", Name: "Missing spaces within closed headings", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD021", Name: "Extra spaces within closed headings", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD022", Name: "Blank lines around headings", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"before": integer(1, 0), "after": integer(1, 0)}},
		{ID: "MD023", Name: "Indented headings", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD024", Name: "Repeated heading text", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"siblings_only": boolean(false)}},
		{ID: "MD025", Name: "Multiple document titles", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"level": headingLevel(1), "frontmatter_title": boolean(false)}},
		{ID: "MD026", Name: "Punctuation at heading ends", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"punctuation": text(".,:;!?")}},
		{ID: "MD027", Name: "Whitespace after blockquote markers", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD028", Name: "Empty lines separating blockquotes", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD029", Name: "Numbered list prefixes", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("ordered", "one")}},
		{ID: "MD030", Name: "Whitespace after list markers", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"spaces": integer(1, 1)}},
		{ID: "MD031", Name: "Blank lines around fenced blocks", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD032", Name: "Blank lines around lists", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD033", Name: "Embedded HTML", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allowed_tags": list()}},
		{ID: "MD034", Name: "URLs without link markup", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD035", Name: "Horizontal rule marker style", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "dash", "asterisk", "underscore")}},
		{ID: "MD036", Name: "Emphasis used as a heading", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD037", Name: "Whitespace within emphasis delimiters", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD038", Name: "Whitespace within inline code delimiters", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD039", Name: "Whitespace surrounding link labels", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD040", Name: "Missing fenced block language", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allowed_languages": list()}},
		{ID: "MD041", Name: "Opening document heading", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"level": headingLevel(1), "frontmatter_title": boolean(false)}},
		{ID: "MD042", Name: "Links without destinations", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD043", Name: "Required heading outline", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"headings": list()}},
		{ID: "MD044", Name: "Capitalization of configured names", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"names": list(), "code_blocks": boolean(false)}},
		{ID: "MD045", Name: "Images without alternative text", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allow_decorative": boolean(false)}},
		{ID: "MD046", Name: "Indented versus fenced code", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "fenced", "indented")}},
		{ID: "MD047", Name: "Exactly one terminating newline", DefaultEnabled: true, Fix: ImplementedFix, RequiresSyntax: false, Options: OptionSchema{}},
		{ID: "MD048", Name: "Backtick versus tilde fences", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "backtick", "tilde")}},
		{ID: "MD049", Name: "Italic delimiter style", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "asterisk", "underscore")}},
		{ID: "MD050", Name: "Bold delimiter style", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "asterisk", "underscore")}},
		{ID: "MD051", Name: "Unresolved fragment targets", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"cross_file": boolean(false)}},
		{ID: "MD052", Name: "References without definitions", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD053", Name: "Unneeded link and image definitions", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"ignored_labels": list()}},
		{ID: "MD054", Name: "Link and image representation style", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"allowed_styles": {Kind: Strings, Default: []string{"inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink"}, Choices: []string{"inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink"}, MinItems: 1}}},
		{ID: "MD055", Name: "Outer table pipe convention", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, AppliesTo: pipeTablesApply, Options: OptionSchema{"style": choice("consistent", "leading_and_trailing", "none")}},
		{ID: "MD056", Name: "Mismatched cell counts", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, AppliesTo: pipeTablesApply, Options: OptionSchema{}},
		{ID: "MD057", Name: "Missing local link targets", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"check_images": boolean(true)}},
		{ID: "MD058", Name: "Blank lines around tables", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, AppliesTo: pipeTablesApply, Options: OptionSchema{}},
		{ID: "MD059", Name: "Uninformative link labels", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"phrases": list("click here", "here", "more", "read more")}},
		{ID: "MD060", Name: "Table column layout", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, AppliesTo: pipeTablesApply, Options: OptionSchema{"style": choice("aligned", "compact", "tight")}},
		{ID: "MD061", Name: "Configured prohibited vocabulary", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"terms": list(), "case_sensitive": boolean(false)}},
		{ID: "MD062", Name: "Whitespace in destination URLs", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD063", Name: "Heading capitalization", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("sentence", "title"), "proper_names": list()}},
		{ID: "MD064", Name: "Repeated spaces in prose", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"maximum": integer(1, 1)}},
		{ID: "MD065", Name: "Blank lines around horizontal rules", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD066", Name: "Unmatched footnote references and definitions", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, AppliesTo: footnotesApply, Options: OptionSchema{}},
		{ID: "MD067", Name: "Definition order versus reference order", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, AppliesTo: footnotesApply, Options: OptionSchema{}},
		{ID: "MD068", Name: "Footnotes without content", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, AppliesTo: footnotesApply, Options: OptionSchema{}},
		{ID: "MD069", Name: "Repeated list markers", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD070", Name: "Outer fence length for nested examples", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD071", Name: "Blank line following metadata", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD072", Name: "Metadata key ordering", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"recursive": boolean(false), "case_sensitive": boolean(true)}},
		{ID: "MD073", Name: "Table of contents versus headings", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"start_marker": {Kind: String, Default: "<!-- toc -->", Domain: HTMLComment}, "end_marker": {Kind: String, Default: "<!-- /toc -->", Domain: HTMLComment}, "min_level": headingLevel(1), "max_level": headingLevel(6)}},
		{ID: "MD074", Name: "MkDocs navigation file targets", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"config_path": text("mkdocs.yml"), "check_omitted": boolean(false), "check_absolute": boolean(false)}},
		{ID: "MD075", Name: "Pipe rows without a table header", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, AppliesTo: pipeTablesApply, Options: OptionSchema{}},
		{ID: "MD076", Name: "Consistent spacing between list items", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("consistent", "tight", "loose")}},
		{ID: "MD077", Name: "Indentation of list continuation content", DefaultEnabled: true, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD078", Name: "Missing executable Quarto chunk label", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"require_unique": boolean(true)}},
		{ID: "MD079", Name: "Whitespace in Quarto chunk labels", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD080", Name: "Colliding heading anchors", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD081", Name: "Overuse of bold and italic text", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"maximum_ratio": {Kind: Number, Default: 0.5, Minimum: bound(0), Maximum: bound(1)}, "minimum_characters": integer(20, 1)}},
		{ID: "MD082", Name: "Sections without content", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allow_final_section": boolean(false)}},
		{ID: "MD083", Name: "Text corrupted by encoding mismatch", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"sequences": list("Ã", "Â", "â€")}},
		{ID: "MD084", Name: "Hidden or discouraged Unicode characters", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allowed_codepoints": {Kind: Strings, Default: []string{}, Domain: UnicodeScalarHex}}},
		{ID: "MD085", Name: "Indented paragraph continuation lines", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD086", Name: "Comments lacking a closing delimiter", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD087", Name: "Suppressions that hide no findings", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD088", Name: "Quote and dash normalization", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{"style": choice("ascii")}},
		{ID: "MD089", Name: "Spacing between CJK and Latin text", DefaultEnabled: false, Fix: PlannedFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD090", Name: "Horizontal rules before headings", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allow_slides": boolean(true)}},
		{ID: "MD091", Name: "Markdown syntax inside HTML blocks", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD092", Name: "Unresolved version-control conflict markers", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{}},
		{ID: "MD093", Name: "Inline formatting in headings", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allowed_constructs": {Kind: Strings, Default: []string{}, Choices: []string{"emphasis", "strong", "code", "link", "image"}}}},
		{ID: "MD094", Name: "Invalid UTF-8 bytes", DefaultEnabled: true, Fix: NoFix, RequiresSyntax: false, Options: OptionSchema{}},
		{ID: "MD095", Name: "Lookalike Unicode characters in destinations", DefaultEnabled: false, Fix: NoFix, RequiresSyntax: true, Options: OptionSchema{"allowed_hosts": {Kind: Strings, Default: []string{}, Domain: DNSHostname}}},
	}
}

// NewCatalogRegistry combines the complete catalog with only supplied analyses.
func NewCatalogRegistry(implementations []Registration) (*Registry, error) {
	return NewRegistry(Catalog(), implementations)
}
func bound(n int) *int          { return &n }
func boolean(v bool) OptionSpec { return OptionSpec{Kind: Boolean, Default: v} }
func text(v string) OptionSpec  { return OptionSpec{Kind: String, Default: v} }
func integer(v, min int) OptionSpec {
	return OptionSpec{Kind: Integer, Default: v, Minimum: bound(min)}
}
func headingLevel(v int) OptionSpec {
	return OptionSpec{Kind: Integer, Default: v, Minimum: bound(1), Maximum: bound(6)}
}
func choice(first string, rest ...string) OptionSpec {
	return OptionSpec{Kind: String, Default: first, Choices: append([]string{first}, rest...)}
}
func list(values ...string) OptionSpec {
	return OptionSpec{Kind: Strings, Default: append([]string{}, values...)}
}

// Applicability excludes only formats whose accepted profile proves that the
// syntax is absent. Missing or unrecognized context is not proof of absence;
// execution admission and parser coverage handle unavailable dialect support.
func pipeTablesApply(dc DocumentContext) bool {
	return dc.Flavor != "commonmark"
}

func footnotesApply(dc DocumentContext) bool {
	return dc.Flavor != "commonmark" && dc.Flavor != "gfm"
}

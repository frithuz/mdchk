package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
)

// ValidationError is a semantic or capability error at a supplied field. Err
// remains inspectable, including errors from semantic providers.
type ValidationError struct {
	Origin Origin
	Err    error
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %v", formatOrigin(e.Origin), e.Err) }
func (e *ValidationError) Unwrap() error { return e.Err }

// Capabilities describes implemented behavior, independently of configuration
// defaults. Pandoc3121FullProfile asserts the complete pinned fixed profile;
// generic dialect support or partial extensions cannot assert this capability.
// RepresentIndexFile is a pure active-adapter representability predicate, not IO.
type Capabilities struct {
	CommonMark, GFM, Pandoc3121FullProfile                   bool
	DirectiveProcessing                                      bool
	FrontmatterYAML, FrontmatterTOML, MetadataInterpretation bool
	QuartoRecognition, SlideAnnotation                       bool
	ResourceIndex, GitHubAnchors, ExplicitAnchors            bool
	RepresentIndexFile                                       func(string) error
}

// Permissions carries inherited denials only. Its zero value permits everything.
// Origins identify all governing false values; no rule options or flavor are
// inherited. Callers retain ownership; validation snapshots all collections.
type Permissions struct {
	Directives     []Origin
	RuleDirectives map[string][]Origin
	NestedExcludes []Origin
}

func clonePermissions(p Permissions) Permissions {
	q := Permissions{RuleDirectives: map[string][]Origin{}}
	copyOrigins := func(xs []Origin) []Origin {
		ys := make([]Origin, len(xs))
		for i, o := range xs {
			ys[i] = cloneOrigin(o)
		}
		return ys
	}
	q.Directives = copyOrigins(p.Directives)
	q.NestedExcludes = copyOrigins(p.NestedExcludes)
	for k, v := range p.RuleDirectives {
		q.RuleDirectives[k] = copyOrigins(v)
	}
	return q
}

// Validated is an immutable schema-valid definition. It grants no execution
// availability. Its private owned snapshot is independent of passive fields.
type Validated struct {
	selection       *engine.RuleSelection
	definition      *Definition
	rules           map[string]engine.ResolvedRule
	settings        Builtins
	exclude, slides *Patterns
}

// Resolution is a fresh owned selected-operation snapshot. Own permissions and
// effective inherited restrictions remain distinct. Origins contains only
// explicit values; absence means a catalog/built-in default. No missing default
// rule implementation becomes a validation coverage deficit.
type Resolution struct {
	plan                                 *ExecutionPlan
	Settings                             Builtins
	Rules                                map[string]engine.ResolvedRule
	Origins                              map[string]Origin
	OwnPermissions, EffectivePermissions Permissions
	Exclude, Slides                      *Patterns
}

// ValidateDefinition performs complete schema and semantic validation without
// admitting execution. Supply a registry built from the complete engine.Catalog,
// with the build's actual executable registrations. Provider errors are schema
// failures even for disabled rules and policy-only definitions.
func ValidateDefinition(input *Definition, registry *engine.Registry, providers engine.ValidationProviders) (*Validated, error) {
	if input == nil || input.Syntax().Kind() == "" {
		return nil, &ValidationError{Err: errors.New("definition requires an owned decoded syntax snapshot")}
	}
	d, err := definition(input.Syntax())
	if err != nil {
		return nil, err
	}
	at := func(path string, err error) error { return located(d, path, err) }
	if registry == nil {
		return nil, at("", errors.New("complete rule registry is required"))
	}
	// A subset of implementations is supported; a subset of catalog metadata is not.
	metadata := registry.Metadata()
	catalog := engine.Catalog()
	if len(metadata) != len(catalog) {
		return nil, at("", errors.New("registry must contain the complete rule catalog"))
	}
	for i, m := range metadata {
		if m.ID != catalog[i].ID || !reflect.DeepEqual(m.Options, catalog[i].Options) || m.DefaultEnabled != catalog[i].DefaultEnabled {
			return nil, at("", errors.New("registry must contain the complete rule catalog"))
		}
	}
	b := DefaultSettings()
	if d.Flavor != nil {
		b.Flavor = *d.Flavor
	}
	if !slices.Contains([]string{"commonmark", "gfm", "pandoc"}, b.Flavor) {
		return nil, at("flavor", errors.New("expected commonmark, gfm, or pandoc"))
	}
	if d.Resources != nil {
		if p := d.Resources.IndexFile; p != nil {
			b.IndexFile = *p
			if !validIndexFile(*p) {
				return nil, at("resources.index_file", errors.New("expected a nonempty literal basename"))
			}
		}
		if p := d.Resources.AnchorStyle; p != nil {
			b.AnchorStyle = *p
			if !slices.Contains([]string{"github", "explicit"}, *p) {
				return nil, at("resources.anchor_style", errors.New("expected github or explicit"))
			}
		}
	}
	if d.Frontmatter != nil {
		if p := d.Frontmatter.Enabled; p != nil {
			b.FrontmatterEnabled = *p
		}
		if p := d.Frontmatter.Formats; p != nil {
			b.FrontmatterFormats = slices.Clone(*p)
		}
		if len(b.FrontmatterFormats) == 0 {
			return nil, at("frontmatter.formats", errors.New("expected a nonempty unique YAML/TOML list"))
		}
		seen := map[string]bool{}
		for i, f := range b.FrontmatterFormats {
			if !slices.Contains([]string{"yaml", "toml"}, f) || seen[f] {
				return nil, at(fmt.Sprintf("frontmatter.formats[%d]", i), errors.New("expected unique yaml or toml formats"))
			}
			seen[f] = true
		}
	}
	if d.Integrations != nil && d.Integrations.Quarto != nil && d.Integrations.Quarto.Enabled != nil {
		b.QuartoEnabled = *d.Integrations.Quarto.Enabled
	}
	if d.Documents != nil && d.Documents.Slides != nil {
		b.Slides = slices.Clone(*d.Documents.Slides)
	}
	if d.Exclude != nil {
		b.Exclude = slices.Clone(*d.Exclude)
	}
	if d.Directives != nil && d.Directives.Enabled != nil {
		b.DirectivesEnabled = *d.Directives.Enabled
	}
	if d.AllowNestedExcludes != nil {
		b.AllowNestedExcludes = *d.AllowNestedExcludes
	}
	compile := func(path string, values []string) (*Patterns, error) {
		p, e := CompilePatterns(values)
		if e != nil {
			var pe *PatternError
			if errors.As(e, &pe) {
				path = fmt.Sprintf("%s[%d]", path, pe.Index)
			}
			return nil, at(path, e)
		}
		return p, nil
	}
	ex, err := compile("exclude", b.Exclude)
	if err != nil {
		return nil, err
	}
	slides, err := compile("documents.slides", b.Slides)
	if err != nil {
		return nil, err
	}
	for _, rule := range child(d.Syntax(), "rules").Entries() {
		for _, option := range rule.Value.Entries() {
			if option.Key == "enabled" || option.Key == "allow_directives" {
				continue
			}
			if _, unsupported := optionValue(option.Value).(LocatedValue); unsupported {
				bad := option.Value
				if bad.Kind() == SequenceValue {
					for _, e := range bad.Elements() {
						if e.Kind() != StringValue {
							bad = e
							break
						}
					}
				}
				return nil, &ValidationError{Origin: bad.Origin(), Err: errors.New("unsupported or unrepresentable rule option value")}
			}
		}
	}
	selection, err := registry.SelectRules(d.Rules, providers)
	if err != nil {
		var se *engine.SettingError
		path := "rules"
		if errors.As(err, &se) {
			path += "." + se.RuleID
			if se.Option != "" {
				path += "." + se.Option
			}
			if se.Element != nil {
				path += fmt.Sprintf("[%d]", *se.Element)
			}
		}
		return nil, at(path, err)
	}
	return &Validated{selection: selection, definition: d, rules: selection.Rules(), settings: b, exclude: ex, slides: slides}, nil
}
func validIndexFile(s string) bool {
	return s != "" && s != "." && s != ".." && utf8.ValidString(s) && !strings.ContainsAny(s, "/\\*?[]#:") && strings.IndexFunc(s, unicode.IsControl) < 0
}
func located(d *Definition, path string, err error) error {
	o, ok := d.Provenance[path]
	if !ok {
		o = d.Syntax().Origin()
		o.PropertyPath = path
	}
	return &ValidationError{cloneOrigin(o), err}
}

// Permissions returns the inherited denials combined with this definition's
// own restrictions. It neither merges settings nor admits execution. Discovery
// boundaries and rejecting additions under NestedExcludes belong to the caller.
func (v *Validated) Permissions(inherited Permissions) Permissions {
	p := clonePermissions(inherited)
	d := v.definition
	if !v.settings.DirectivesEnabled {
		p.Directives = append(p.Directives, cloneOrigin(d.Provenance["directives.enabled"]))
	}
	if !v.settings.AllowNestedExcludes {
		p.NestedExcludes = append(p.NestedExcludes, cloneOrigin(d.Provenance["allow_nested_excludes"]))
	}
	for id, s := range d.Rules {
		if s.AllowDirectives != nil && !*s.AllowDirectives {
			p.RuleDirectives[id] = append(p.RuleDirectives[id], cloneOrigin(d.Provenance["rules."+id+".allow_directives"]))
		}
	}
	return p
}

// Admit validates selected-operation availability, without parsing Markdown,
// invoking checks, acquiring resources, or returning partial usable settings.
func (v *Validated) Admit(c Capabilities, inherited Permissions) (*Resolution, error) {
	if v == nil {
		return nil, &ValidationError{Err: errors.New("nil validated definition")}
	}
	d := v.definition
	b := v.settings
	p := v.Permissions(inherited)
	fail := func(path, message string) (*Resolution, error) { return nil, located(d, path, errors.New(message)) }
	supported := b.Flavor == "commonmark" && c.CommonMark || b.Flavor == "gfm" && c.GFM || b.Flavor == "pandoc" && c.Pandoc3121FullProfile
	if !supported {
		return fail("flavor", "selected Markdown profile is unavailable")
	}
	if len(p.Directives) == 0 && !c.DirectiveProcessing {
		return fail("directives.enabled", "global directive processing is unavailable; effective permission must be false")
	}
	if b.FrontmatterEnabled {
		for _, f := range b.FrontmatterFormats {
			if f == "yaml" && !c.FrontmatterYAML || f == "toml" && !c.FrontmatterTOML {
				return fail("frontmatter.enabled", "selected frontmatter recognition is unavailable")
			}
		}
	}
	if b.QuartoEnabled && !c.QuartoRecognition {
		return fail("integrations.quarto.enabled", "Quarto recognition is unavailable")
	}
	if len(b.Slides) > 0 && !c.SlideAnnotation {
		return fail("documents.slides", "slide annotation is unavailable")
	}
	if d.Resources != nil {
		if d.Resources.IndexFile != nil {
			if !c.ResourceIndex {
				return fail("resources.index_file", "resource index handling is unavailable")
			}
			if c.RepresentIndexFile == nil {
				return fail("resources.index_file", "active adapter basename validation is unavailable")
			}
			if err := c.RepresentIndexFile(b.IndexFile); err != nil {
				return nil, located(d, "resources.index_file", err)
			}
		}
		if d.Resources.AnchorStyle != nil && (b.AnchorStyle == "github" && !c.GitHubAnchors || b.AnchorStyle == "explicit" && !c.ExplicitAnchors) {
			return fail("resources.anchor_style", "selected anchor style is unavailable")
		}
	}
	rules := make(map[string]engine.ResolvedRule, len(v.rules))
	// Catalog order gives deterministic errors, independent of map iteration.
	for _, m := range engine.Catalog() {
		id := m.ID
		s := v.rules[id]
		if s.Explicit && s.Enabled && !s.Available {
			return fail("rules."+id, "explicitly enabled rule is unavailable")
		}
		if s.Enabled && s.Available {
			if !ruleOptionCapability("frontmatter_title", s.Options["frontmatter_title"], c) {
				return fail("rules."+id+".frontmatter_title", "frontmatter metadata interpretation is unavailable")
			}
		}
		if len(p.Directives) > 0 || len(p.RuleDirectives[id]) > 0 {
			s.AllowDirectives = false
		}
		s.Options = cloneOptions(s.Options)
		rules[id] = s
	}
	b.FrontmatterFormats = slices.Clone(b.FrontmatterFormats)
	b.Slides = slices.Clone(b.Slides)
	b.Exclude = slices.Clone(b.Exclude)
	b.DirectivesEnabled = len(p.Directives) == 0
	b.AllowNestedExcludes = len(p.NestedExcludes) == 0
	origins := make(map[string]Origin, len(d.Provenance))
	for k, o := range d.Provenance {
		origins[k] = cloneOrigin(o)
	}
	denied := make(map[string]bool)
	for id, rule := range rules {
		if !rule.AllowDirectives {
			denied[id] = true
		}
	}
	plan, err := v.selection.Plan(engine.DocumentContext{Flavor: b.Flavor}, denied)
	if err != nil {
		return nil, located(d, "rules", err)
	}
	plan = plan.WithDirectivePolicy(v.configuration(c, inherited))
	resolution := &Resolution{Settings: b, Rules: rules, Origins: origins, OwnPermissions: v.Permissions(Permissions{}), EffectivePermissions: p, Exclude: ownedPatterns(v.exclude), Slides: ownedPatterns(v.slides)}
	resolution.plan = &ExecutionPlan{engine: plan, configuration: cloneResolution(resolution)}
	return resolution, nil
}
func cloneOptions(o engine.Options) engine.Options {
	p := make(engine.Options, len(o))
	for k, v := range o {
		if xs, ok := v.([]string); ok {
			v = slices.Clone(xs)
		}
		p[k] = v
	}
	return p
}

func child(v LocatedValue, key string) LocatedValue {
	for _, e := range v.Entries() {
		if e.Key == key {
			return e.Value
		}
	}
	return LocatedValue{}
}

// ExecutionPlan returns the immutable admitted execution bridge. Mutating this
// Resolution's passive snapshots cannot alter it. A manually built Resolution
// has no execution authority and returns nil.
func (r *Resolution) ExecutionPlan() *ExecutionPlan {
	if r == nil {
		return nil
	}
	return r.plan
}

// ruleOptionCapability describes support for the selected option value, apart
// from executable rule registration and enablement. Share this predicate with
// pre-admission inspection so inactive settings retain truthful availability.
func ruleOptionCapability(name string, value any, c Capabilities) bool {
	if name == "frontmatter_title" {
		title, _ := value.(bool)
		return !title || c.MetadataInterpretation
	}
	return true
}

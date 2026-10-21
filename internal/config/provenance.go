package config

import (
	"slices"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

// SettingProvenance distinguishes explicit leaf values from catalog/built-in
// defaults. Availability describes build support independently of enablement,
// dialect applicability, inherited restrictions and execution admission.
type SettingProvenance struct {
	Explicit  bool
	Origin    Origin
	Available bool
}

// Configuration is a fresh owned pre-admission view of one selected definition.
// Ancestor settings are never merged. Selected identifies its diagnostic source;
// supplied objects have no invented filename or position. Rules retains explicit
// entry membership independently of each leaf's provenance (an empty rule entry
// is explicit while its enabled/options leaves remain catalog defaults).
type Configuration struct {
	Selected   Origin
	Settings   Builtins
	Rules      map[string]engine.ResolvedRule
	Provenance map[string]SettingProvenance
}

// Configuration exposes fully validated settings even when Admit would reject
// unavailable requested behavior. It performs no IO or capability admission.
func (v *Validated) Configuration(c Capabilities) *Configuration {
	if v == nil {
		return nil
	}
	b := v.settings
	b.FrontmatterFormats = slices.Clone(b.FrontmatterFormats)
	b.Exclude = slices.Clone(b.Exclude)
	b.Slides = slices.Clone(b.Slides)
	view := &Configuration{Selected: v.definition.Syntax().Origin(), Settings: b, Rules: map[string]engine.ResolvedRule{}, Provenance: map[string]SettingProvenance{}}
	add := func(path string, available bool) {
		o, explicit := v.definition.Provenance[path]
		if !explicit {
			o = Origin{PropertyPath: path}
		}
		view.Provenance[path] = SettingProvenance{Explicit: explicit, Origin: cloneOrigin(o), Available: available}
	}
	add("version", true)
	add("flavor", b.Flavor == "commonmark" && c.CommonMark || b.Flavor == "gfm" && c.GFM || b.Flavor == "pandoc" && c.Pandoc3121FullProfile)
	add("resources.index_file", c.ResourceIndex && c.RepresentIndexFile != nil)
	add("resources.anchor_style", b.AnchorStyle == "github" && c.GitHubAnchors || b.AnchorStyle == "explicit" && c.ExplicitAnchors)
	formats := true
	for _, f := range b.FrontmatterFormats {
		formats = formats && (f == "yaml" && c.FrontmatterYAML || f == "toml" && c.FrontmatterTOML)
	}
	add("frontmatter.enabled", formats)
	add("frontmatter.formats", formats)
	add("integrations.quarto.enabled", c.QuartoRecognition)
	add("documents.slides", c.SlideAnnotation)
	add("exclude", true)
	add("directives.enabled", c.DirectiveProcessing)
	add("allow_nested_excludes", true)
	for _, m := range engine.Catalog() {
		rule := v.rules[m.ID]
		rule.Options = cloneOptions(rule.Options)
		view.Rules[m.ID] = rule
		prefix := "rules." + m.ID
		add(prefix, rule.Available)
		add(prefix+".enabled", rule.Available)
		add(prefix+".allow_directives", c.DirectiveProcessing)
		for key, value := range rule.Options {
			add(prefix+"."+key, rule.Available && ruleOptionCapability(key, value, c))
		}
	}
	// Retain supplied array-element locations, including alias use-site points.
	for path, o := range v.definition.Provenance {
		if _, ok := view.Provenance[path]; !ok && strings.Contains(path, "[") {
			parent := path[:strings.Index(path, "[")]
			p, ok := view.Provenance[parent]
			if ok {
				view.Provenance[path] = SettingProvenance{true, cloneOrigin(o), p.Available}
			}
		}
	}
	return view
}

// Shared returns a fresh schema-v1 Configuration projection. Effective follows
// configuration field names, with rule availability and explicit membership as
// additional inspection metadata. The availability map records build support
// by JSON Pointer for all effective settings. Provenance is sorted by JSON Pointer. Origin
// points are zero-width ranges, never fabricated lexical ranges. No source hash
// is emitted without acquired source evidence.
func (c *Configuration) Shared() result.ConfigurationData {
	if c == nil {
		return result.ConfigurationData{Effective: map[string]any{}, Sources: []result.ConfigurationSource{}, Provenance: []result.Provenance{}}
	}
	b := c.Settings
	rules := map[string]any{}
	for id, r := range c.Rules {
		rule := map[string]any{"enabled": r.Enabled, "allow_directives": r.AllowDirectives, "available": r.Available, "explicit": r.Explicit}
		for key, value := range cloneOptions(r.Options) {
			rule[key] = value
		}
		rules[id] = rule
	}
	availability := map[string]bool{}
	for path, p := range c.Provenance {
		availability[propertyPointer(path)] = p.Available
	}
	effective := map[string]any{
		"availability": availability,
		"version":      Version, "flavor": b.Flavor,
		"resources":    map[string]any{"index_file": b.IndexFile, "anchor_style": b.AnchorStyle},
		"frontmatter":  map[string]any{"enabled": b.FrontmatterEnabled, "formats": slices.Clone(b.FrontmatterFormats)},
		"integrations": map[string]any{"quarto": map[string]any{"enabled": b.QuartoEnabled}},
		"documents":    map[string]any{"slides": slices.Clone(b.Slides)},
		"exclude":      slices.Clone(b.Exclude), "directives": map[string]any{"enabled": b.DirectivesEnabled}, "allow_nested_excludes": b.AllowNestedExcludes, "rules": rules,
	}
	sourceKind := "supplied-config"
	if c.Selected.Path != "" {
		sourceKind = "selected-config"
	}
	out := result.ConfigurationData{Effective: effective, Sources: []result.ConfigurationSource{{Kind: "default"}, {Kind: sourceKind, Path: c.Selected.Path}}, Provenance: []result.Provenance{}}
	for path, p := range c.Provenance {
		kind := "default"
		if p.Explicit {
			kind = sourceKind
		}
		record := result.Provenance{Pointer: propertyPointer(path), SourceKind: kind, Path: p.Origin.Path}
		if p.Origin.Position != nil {
			record.Range = &result.Range{Start: *p.Origin.Position, End: *p.Origin.Position}
		}
		out.Provenance = append(out.Provenance, record)
	}
	slices.SortFunc(out.Provenance, func(a, b result.Provenance) int { return strings.Compare(a.Pointer, b.Pointer) })
	return out
}

// Property paths here have passed the fixed configuration schema: dots delimit
// known field/catalog/option names and brackets delimit decimal list indices.
// Escape each segment separately rather than treating dotted paths as pointers.
func propertyPointer(path string) string {
	var out strings.Builder
	for _, field := range strings.Split(path, ".") {
		parts := strings.Split(field, "[")
		for i, part := range parts {
			if i > 0 {
				part = strings.TrimSuffix(part, "]")
				if _, err := strconv.Atoi(part); err != nil {
					continue
				}
			}
			out.WriteByte('/')
			out.WriteString(strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1"))
		}
	}
	return out.String()
}

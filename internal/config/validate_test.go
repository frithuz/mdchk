package config

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func registryFor(t *testing.T, ids ...string) *engine.Registry {
	t.Helper()
	var rs []engine.Registration
	for _, id := range ids {
		rs = append(rs, engine.Registration{ID: id, Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
			t.Fatal("validation called check")
			return engine.RuleOutcome{}
		}})
	}
	r, e := engine.NewCatalogRegistry(rs)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func yamlFor(t *testing.T, s string) *Definition {
	t.Helper()
	d, e := DecodeYAML("fixture.yaml", []byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func schemaFor(t *testing.T, s string, r *engine.Registry) *Validated {
	t.Helper()
	v, e := ValidateDefinition(yamlFor(t, s), r, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func baseCaps() Capabilities {
	return Capabilities{CommonMark: true, GFM: true, DirectiveProcessing: true}
}
func assertPath(t *testing.T, err error, path string) {
	t.Helper()
	var e *ValidationError
	if !errors.As(err, &e) || e.Origin.PropertyPath != path {
		t.Fatalf("error %v: expected path %s", err, path)
	}
}
func TestValidationDefaultsAcrossBuilds(t *testing.T) {
	all := []string{}
	for _, m := range engine.Catalog() {
		if m.ID != "MD094" {
			all = append(all, m.ID)
		}
	}
	for _, ids := range [][]string{nil, {"MD001"}, all} {
		r := registryFor(t, ids...)
		v := schemaFor(t, "version: 1\n", r)
		got, e := v.Admit(baseCaps(), Permissions{})
		if e != nil {
			t.Fatal(e)
		}
		yes, no := 0, 0
		for _, m := range engine.Catalog() {
			s := got.Rules[m.ID]
			if s.Enabled != m.DefaultEnabled || s.Explicit {
				t.Fatalf("changed %s", m.ID)
			}
			if s.Enabled {
				yes++
			} else {
				no++
			}
		}
		if yes != 59 || no != 30 || len(got.Rules) != 89 {
			t.Fatal(yes, no)
		}
		if !reflect.DeepEqual(got.Settings, DefaultSettings()) {
			t.Fatalf("defaults changed: %#v", got.Settings)
		}
	}
}
func TestExplicitRules(t *testing.T) {
	for _, tt := range []struct {
		setting          string
		enabled, wantErr bool
	}{{"MD001: {}", true, true}, {"MD001: {enabled: false}", false, false}, {"MD013: {}", false, false}, {"MD013: {length: 100}", false, false}, {"MD013: {allow_directives: false}", false, false}, {"MD094: {}", true, false}} {
		t.Run(tt.setting, func(t *testing.T) {
			v := schemaFor(t, "version: 1\nrules: {"+tt.setting+"}\n", registryFor(t))
			got, e := v.Admit(baseCaps(), Permissions{})
			if (e != nil) != tt.wantErr {
				t.Fatal(e)
			}
			if e == nil {
				id := strings.Split(tt.setting, ":")[0]
				if !got.Rules[id].Explicit || got.Rules[id].Enabled != tt.enabled {
					t.Fatal(got.Rules[id])
				}
			}
		})
	}
}
func TestSchemaDomainsEvenDisabled(t *testing.T) {
	for _, tt := range []struct{ body, path string }{{"rules: {md001: {}}", "rules.md001"}, {"rules: {MD002: {}}", "rules.MD002"}, {"rules: {MD051: {enabled: false, anchor_style: github}}", "rules.MD051.anchor_style"}, {"rules: {MD080: {enabled: false, anchor_style: explicit}}", "rules.MD080.anchor_style"}, {"rules: {MD057: {enabled: false, index_file: index.md}}", "rules.MD057.index_file"}, {"rules: {MD013: {enabled: false, length: -1}}", "rules.MD013.length"}, {"rules: {MD013: {length: 99999999999999999999999999999}}", "rules.MD013.length"}, {"rules: {MD093: {allowed_constructs: [emphasis, wrong]}}", "rules.MD093.allowed_constructs[1]"}, {"rules: {MD093: {allowed_constructs: [emphasis, 1]}}", "rules.MD093.allowed_constructs[1]"}, {"frontmatter: {enabled: false, formats: []}", "frontmatter.formats"}, {"frontmatter: {formats: [yaml, yaml]}", "frontmatter.formats[1]"}, {"exclude: [good, '[']", "exclude[1]"}, {"documents: {slides: [good, '[']}", "documents.slides[1]"}, {"resources: {anchor_style: unknown}", "resources.anchor_style"}, {"flavor: unsupported+extension", "flavor"}} {
		t.Run(tt.body, func(t *testing.T) {
			_, e := ValidateDefinition(yamlFor(t, "version: 1\n"+tt.body+"\n"), registryFor(t), engine.ValidationProviders{})
			assertPath(t, e, tt.path)
		})
	}
}
func TestSelectedCapabilitiesAndPolicy(t *testing.T) {
	r := registryFor(t, "MD001")
	ancestor := schemaFor(t, "version: 1\nflavor: gfm\nfrontmatter: {enabled: true}\nrules: {MD003: {}, MD001: {allow_directives: false}}\ndirectives: {enabled: false}\nresources: {index_file: README.md, anchor_style: explicit}\n", r)
	if _, e := ancestor.Admit(baseCaps(), Permissions{}); e == nil {
		t.Fatal("ancestor unavailable selected profile accepted")
	}
	child := schemaFor(t, "version: 1\nflavor: commonmark\ndirectives: {enabled: true}\nrules: {MD001: {allow_directives: true}}\n", r)
	c := Capabilities{CommonMark: true}
	got, e := child.Admit(c, ancestor.Permissions(Permissions{}))
	if e != nil {
		t.Fatal(e)
	}
	if got.Settings.DirectivesEnabled || got.Rules["MD001"].AllowDirectives || got.Settings.IndexFile != "index.md" || got.Settings.AnchorStyle != "github" || got.Rules["MD003"].Explicit {
		t.Fatal("ancestor settings merged or reopened")
	}
	if len(got.OwnPermissions.Directives) != 0 || len(got.EffectivePermissions.Directives) != 1 {
		t.Fatal("own/effective lost")
	}
	if _, e = child.Admit(c, Permissions{}); e == nil {
		t.Fatal("perrule denial substituted global")
	}
	v := schemaFor(t, "version: 1\nrules: {MD001: {allow_directives: false}}\n", r)
	_, e = v.Admit(Capabilities{GFM: true}, Permissions{})
	assertPath(t, e, "directives.enabled")
}
func TestOptionalCapabilities(t *testing.T) {
	for _, tt := range []struct {
		body, path string
		caps       Capabilities
	}{{"", "flavor", Capabilities{CommonMark: true, DirectiveProcessing: true}}, {"frontmatter: {enabled: true, formats: [yaml]}", "frontmatter.enabled", baseCaps()}, {"frontmatter: {enabled: true}", "frontmatter.enabled", func() Capabilities { c := baseCaps(); c.FrontmatterYAML = true; return c }()}, {"integrations: {quarto: {enabled: true}}", "integrations.quarto.enabled", baseCaps()}, {"documents: {slides: ['slides/**']}", "documents.slides", baseCaps()}, {"rules: {MD001: {frontmatter_title: true}}", "rules.MD001.frontmatter_title", baseCaps()}, {"resources: {index_file: index.md}", "resources.index_file", baseCaps()}, {"resources: {anchor_style: github}", "resources.anchor_style", baseCaps()}} {
		t.Run(tt.body, func(t *testing.T) {
			v := schemaFor(t, "version: 1\n"+tt.body+"\n", registryFor(t, "MD001"))
			_, e := v.Admit(tt.caps, Permissions{})
			assertPath(t, e, tt.path)
		})
	}
	c := baseCaps()
	c.FrontmatterYAML = true
	c.MetadataInterpretation = true
	c.QuartoRecognition = true
	c.SlideAnnotation = true
	c.ResourceIndex = true
	c.ExplicitAnchors = true
	c.RepresentIndexFile = func(s string) error {
		if s != "README.md" {
			t.Fatal(s)
		}
		return nil
	}
	v := schemaFor(t, "version: 1\nflavor: gfm\nfrontmatter: {enabled: true, formats: [yaml]}\nintegrations: {quarto: {enabled: true}}\ndocuments: {slides: ['s/**']}\nresources: {index_file: README.md, anchor_style: explicit}\nrules: {MD001: {frontmatter_title: true}}\n", registryFor(t, "MD001"))
	if _, e := v.Admit(c, Permissions{}); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"resources: {}", "frontmatter: {enabled: false}", "documents: {slides: []}", "rules: {MD001: {enabled: false, frontmatter_title: true}}"} {
		v := schemaFor(t, "version: 1\n"+s+"\n", registryFor(t))
		if _, e := v.Admit(baseCaps(), Permissions{}); e != nil {
			t.Fatal(e)
		}
	}
}
func TestResourcesDomain(t *testing.T) {
	for _, s := range []string{"", ".", "..", "a/b", "a\\b", "a?x", "a#x", "a*", "[a]", "C:x", "https:x", "a\x01"} {
		obj := map[string]any{"version": 1, "resources": map[string]any{"index_file": s}}
		d, e := DecodeObject(obj)
		if e != nil {
			t.Fatal(e)
		}
		_, e = ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		assertPath(t, e, "resources.index_file")
	}
}
func TestProviderSchemaIdentityAndLocation(t *testing.T) {
	d := yamlFor(t, "version: 1\nrules:\n  MD095:\n    enabled: false\n    allowed_hosts: [example.com]\n")
	_, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if !errors.Is(e, engine.ErrHostnameValidationUnavailable) {
		t.Fatal(e)
	}
	assertPath(t, e, "rules.MD095.allowed_hosts[0]")
	sentinel := errors.New("provider")
	_, e = ValidateDefinition(d, registryFor(t), engine.ValidationProviders{ValidateHostname: func(string) error { return sentinel }})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
}
func TestValidationSnapshotAndOrigins(t *testing.T) {
	d := yamlFor(t, "version: 1\r\n# 雪\r\nrules:\r\n  MD013: {length: -1}\r\n")
	_, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	var ve *ValidationError
	if !errors.As(e, &ve) || ve.Origin.Position.Line != 4 || ve.Origin.Position.Column != 19 {
		t.Fatalf("%#v %v", ve, e)
	}
	d = yamlFor(t, "version: 1\nresources: {}\nrules: {MD013: {length: 100}}\n")
	d.Rules["MD013"] = RuleSetting{}
	d.Provenance["flavor"] = Origin{Path: "fake"}
	*d.Version = 9
	v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	a, e := v.Admit(baseCaps(), Permissions{})
	if e != nil {
		t.Fatal(e)
	}
	a.Rules["MD013"].Options["length"] = 999
	a.Settings.FrontmatterFormats[0] = "bad"
	a.Origins["resources"] = Origin{}
	b, e := v.Admit(baseCaps(), Permissions{})
	if e != nil || b.Rules["MD013"].Options["length"] != 100 || b.Settings.FrontmatterFormats[0] != "yaml" {
		t.Fatal(e, b)
	}
}
func TestSemanticYAMLObjectParity(t *testing.T) {
	cases := []struct {
		yaml   string
		object map[string]any
		path   string
	}{
		{"version: 1\nrules: {MD013: {length: -1}}", map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"length": -1}}}, "rules.MD013.length"},
		{"version: 1\nresources: {anchor_style: unknown}", map[string]any{"version": 1, "resources": map[string]any{"anchor_style": "unknown"}}, "resources.anchor_style"},
		{"version: 1\nexclude: [good, '[']", map[string]any{"version": 1, "exclude": []any{"good", "["}}, "exclude[1]"},
		{"version: 1\nfrontmatter: {formats: [yaml, json]}", map[string]any{"version": 1, "frontmatter": map[string]any{"formats": []string{"yaml", "json"}}}, "frontmatter.formats[1]"},
		{"version: 1\nrules: {MD013: {length: 100}}\nresources: {}", map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"length": 100}}, "resources": map[string]any{}}, ""},
	}
	for _, tt := range cases {
		t.Run(tt.yaml, func(t *testing.T) {
			od, e := DecodeObject(tt.object)
			if e != nil {
				t.Fatal(e)
			}
			yd := yamlFor(t, tt.yaml)
			var resolutions []*Resolution
			for _, d := range []*Definition{yd, od} {
				v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
				if tt.path != "" {
					assertPath(t, e, tt.path)
					var ve *ValidationError
					errors.As(e, &ve)
					if d == od && ve.Origin.Position != nil {
						t.Fatal("invented object position")
					}
					continue
				}
				if e != nil {
					t.Fatal(e)
				}
				r, e := v.Admit(baseCaps(), Permissions{})
				if e != nil {
					t.Fatal(e)
				}
				resolutions = append(resolutions, r)
			}
			if len(resolutions) > 0 && (!reflect.DeepEqual(resolutions[0].Settings, resolutions[1].Settings) || !reflect.DeepEqual(resolutions[0].Rules, resolutions[1].Rules)) {
				t.Fatal("semantic adapters diverged")
			}
		})
	}
}
func TestRegistryMustPreserveCompleteCatalog(t *testing.T) {
	for _, catalog := range [][]engine.Metadata{engine.Catalog()[:1], func() []engine.Metadata { c := engine.Catalog(); c[0].DefaultEnabled = false; return c }(), func() []engine.Metadata { c := engine.Catalog(); c[0].Options = engine.OptionSchema{}; return c }()} {
		r, e := engine.NewRegistry(catalog, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ValidateDefinition(yamlFor(t, "version: 1"), r, engine.ValidationProviders{}); e == nil {
			t.Fatal("incomplete or changed catalog accepted")
		}
	}
}
func TestAliasSemanticOrigin(t *testing.T) {
	d := yamlFor(t, "version: 1\nrules:\n  MD013: &rule {enabled: false, length: -1}\n  MD013x: *rule\n")
	_, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	assertPath(t, e, "rules.MD013x")
	d = yamlFor(t, "version: 1\nrules:\n  MD093: {enabled: false, allowed_constructs: &list [emphasis, invalid]}\n  MD013: {enabled: false, length: *list}\n")
	_, e = ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	assertPath(t, e, "rules.MD013.length")
	var ve *ValidationError
	errors.As(e, &ve)
	if ve.Origin.Position.Line != 4 {
		t.Fatal(e)
	}
}
func TestAdapterRepresentabilityIdentity(t *testing.T) {
	v := schemaFor(t, "version: 1\nresources: {index_file: README.md}\n", registryFor(t))
	c := baseCaps()
	c.ResourceIndex = true
	_, e := v.Admit(c, Permissions{})
	assertPath(t, e, "resources.index_file")
	sentinel := errors.New("unrepresentable")
	c.RepresentIndexFile = func(string) error { return sentinel }
	_, e = v.Admit(c, Permissions{})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
}

func TestCrossFieldErrorsLocateSuppliedOption(t *testing.T) {
	for _, tt := range []struct{ options, path string }{
		{"max_level: 1", "max_level"},
		{"min_level: 6, max_level: 1", "min_level"},
		{"start_marker: '<!-- /toc -->'", "start_marker"},
		{"start_marker: '<!-- same -->', end_marker: '<!-- same -->'", "end_marker"},
	} {
		d := yamlFor(t, "version: 1\nrules: {MD073: {enabled: false, "+tt.options+"}}\n")
		_, err := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		// max_level=1 is compatible with the default min_level=1.
		if tt.options == "max_level: 1" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		assertPath(t, err, "rules.MD073."+tt.path)
		var e *ValidationError
		errors.As(err, &e)
		if e.Origin.Position.Line != 2 {
			t.Fatalf("lost option origin: %v", err)
		}
	}
}

func TestMultipleAncestorPermissionSnapshots(t *testing.T) {
	r := registryFor(t, "MD001")
	a := schemaFor(t, "version: 1\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD001: {allow_directives: false}}\n", r)
	b := schemaFor(t, "version: 1\ndirectives: {enabled: true}\nallow_nested_excludes: true\nrules: {MD001: {allow_directives: true}}\n", r)
	c := schemaFor(t, "version: 1\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD001: {allow_directives: false}}\n", r)
	p := a.Permissions(Permissions{})
	p = b.Permissions(p)
	p = c.Permissions(p)
	got, err := b.Admit(Capabilities{GFM: true}, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EffectivePermissions.Directives) != 2 || len(got.EffectivePermissions.RuleDirectives["MD001"]) != 2 || len(got.EffectivePermissions.NestedExcludes) != 2 {
		t.Fatal(got.EffectivePermissions)
	}
	p.Directives[0].Position.Line = 999
	p.RuleDirectives["MD001"][0].Position.Column = 999
	if got.EffectivePermissions.Directives[0].Position.Line == 999 || got.EffectivePermissions.RuleDirectives["MD001"][0].Position.Column == 999 {
		t.Fatal("inherited origin was borrowed")
	}
	got.EffectivePermissions.Directives[0].Position.Line = 999
	again := a.Permissions(Permissions{})
	if again.Directives[0].Position.Line == 999 {
		t.Fatal("validated snapshot was mutated")
	}
}

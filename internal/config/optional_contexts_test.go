package config

import (
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

// Optional contexts annotate a selected profile independently. Admission must
// preserve their settings and catalog defaults rather than activate related
// contexts or rules merely because a dialect or annotation is supported.
func TestOptionalContextIndependence(t *testing.T) {
	pandoc := DefaultSettings()
	pandoc.Flavor = "pandoc"
	quarto := DefaultSettings()
	quarto.QuartoEnabled = true
	slides := DefaultSettings()
	slides.Flavor = "commonmark"
	slides.FrontmatterEnabled = true
	slides.FrontmatterFormats = []string{"yaml"}
	slides.QuartoEnabled = true
	slides.Slides = []string{"talks/**"}
	slides.Exclude = []string{"generated/**"}
	for _, tc := range []struct {
		name     string
		body     string
		caps     Capabilities
		want     Builtins
		explicit map[string]bool
	}{
		{
			name:     "Pandoc does not enable frontmatter or Quarto",
			body:     "flavor: pandoc\n",
			caps:     Capabilities{Pandoc3121FullProfile: true, DirectiveProcessing: true},
			want:     pandoc,
			explicit: map[string]bool{"flavor": true},
		},
		{
			name:     "Quarto does not enable frontmatter or integration rules",
			body:     "integrations: {quarto: {enabled: true}}\n",
			caps:     Capabilities{GFM: true, QuartoRecognition: true, DirectiveProcessing: true},
			want:     quarto,
			explicit: map[string]bool{"integrations.quarto.enabled": true},
		},
		{
			name:     "slides preserve profile exclusions and independent contexts",
			body:     "flavor: commonmark\nfrontmatter: {enabled: true, formats: [yaml]}\nintegrations: {quarto: {enabled: true}}\ndocuments: {slides: ['talks/**']}\nexclude: ['generated/**']\n",
			caps:     Capabilities{CommonMark: true, FrontmatterYAML: true, QuartoRecognition: true, SlideAnnotation: true, DirectiveProcessing: true},
			want:     slides,
			explicit: map[string]bool{"flavor": true, "frontmatter.enabled": true, "frontmatter.formats": true, "integrations.quarto.enabled": true, "documents.slides": true, "exclude": true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := yamlFor(t, "version: 1\n"+tc.body)
			v, err := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Admit(tc.caps, Permissions{})
			if err != nil {
				t.Fatal(err)
			}
			view := v.Configuration(tc.caps)
			if !reflect.DeepEqual(got.Settings, tc.want) || !reflect.DeepEqual(view.Settings, tc.want) {
				t.Fatalf("context settings changed: admitted=%#v inspected=%#v want=%#v", got.Settings, view.Settings, tc.want)
			}
			for _, m := range engine.Catalog() {
				for _, rule := range []engine.ResolvedRule{got.Rules[m.ID], view.Rules[m.ID]} {
					if rule.Enabled != m.DefaultEnabled || rule.Explicit {
						t.Errorf("context changed catalog default for %s: %#v", m.ID, rule)
					}
				}
			}
			for _, path := range []string{"flavor", "frontmatter.enabled", "frontmatter.formats", "integrations.quarto.enabled", "documents.slides", "exclude", "rules.MD078.enabled", "rules.MD079.enabled", "rules.MD090.enabled"} {
				p, ok := view.Provenance[path]
				if !ok || p.Explicit != tc.explicit[path] {
					t.Fatalf("%s provenance=%#v, want explicit=%v", path, p, tc.explicit[path])
				}
				o, present := got.Origins[path]
				if present != tc.explicit[path] {
					t.Errorf("%s admitted origin presence=%v", path, present)
				}
				if p.Explicit {
					if !reflect.DeepEqual(p.Origin, d.Provenance[path]) || !reflect.DeepEqual(o, p.Origin) || p.Origin.Position == nil {
						t.Errorf("%s lost selected source point: %#v", path, p.Origin)
					}
				} else if p.Origin.Path != "" || p.Origin.Position != nil || p.Origin.PropertyPath != path {
					t.Errorf("%s invented default source: %#v", path, p.Origin)
				}
			}
		})
	}
}

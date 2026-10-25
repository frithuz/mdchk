// Package config defines the approved version-1 configuration boundary.
// Its schema types are passive data, not proof of validation or availability.
package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

const (
	Filename = ".mdchk.yaml"
	Version  = 1
)

// Definition retains only supplied settings. Nil pointers mean omission;
// pointers to zero values mean explicit settings. A non-nil empty mapping or
// sequence remains distinguishable from an omitted field. Do not decode directly
// into this struct: syntax-node validation must precede its construction.
// Ancestor definitions supply restrictions, never inherited rule/options/flavor
// settings. No policy resolution is performed by these types.
type Definition struct {
	syntax              LocatedValue
	Version             *int
	Flavor              *string
	Resources           *Resources
	Frontmatter         *Frontmatter
	Documents           *Documents
	Rules               map[string]RuleSetting
	Exclude             *[]string
	Directives          *Directives
	AllowNestedExcludes *bool
	Provenance          map[string]Origin
}

type Resources struct {
	IndexFile   *string
	AnchorStyle *string
}

type Frontmatter struct {
	Enabled *bool
	Formats *[]string
}

type Documents struct{ Slides *[]string }
type Directives struct{ Enabled *bool }

// RuleSetting separates permissions from engine rule options. Map membership in
// Definition.Rules records an explicit entry, including an empty rule mapping.
// Enabled nil retains catalog enablement; it never means enabled or disabled by
// itself. Only canonical case-sensitive catalog IDs are accepted by semantic
// validation; aliases, reserved/unknown IDs and duplicate entries are errors.
type RuleSetting = engine.RuleSetting

// Origin identifies a field's source for diagnostics and restriction inspection.
// Provenance keys and PropertyPath use dotted schema paths (rules.MD001.enabled,
// for example). Path identifies a definition file, or is empty for a supplied
// object. Position is nil when no textual location is available; otherwise it
// uses one-based line and Unicode code-point columns. Alias values retain their
// use-site origin. Decoders diagnose duplicates before map creation.
type Origin = result.ConfigurationOrigin

// Builtins describes non-rule defaults independently of field presence and
// implementation availability. Rule defaults belong to the complete catalog.
// These values do not validate or resolve a Definition, grant resource access,
// or combine ancestor permissions.
type Builtins struct {
	Flavor              string
	IndexFile           string
	AnchorStyle         string
	FrontmatterEnabled  bool
	FrontmatterFormats  []string
	Slides              []string
	Exclude             []string
	DirectivesEnabled   bool
	AllowDirectives     bool
	AllowNestedExcludes bool
}

// DefaultSettings returns fresh default collections. GFM is always the default,
// even in a build whose parser cannot implement it; availability checks must
// reject an unsupported effective flavor rather than substitute CommonMark.
func DefaultSettings() Builtins {
	return Builtins{
		Flavor: "gfm", IndexFile: "index.md", AnchorStyle: "github",
		FrontmatterFormats: []string{"yaml", "toml"},
		Slides:             []string{}, Exclude: []string{},
		DirectivesEnabled: true, AllowDirectives: true, AllowNestedExcludes: true,
	}
}

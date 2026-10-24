package parser

import "errors"

// FrontmatterOptions is an immutable recognition selection. Its zero value
// disables recognition. Construct enabled selections with NewFrontmatterOptions;
// no supplied format slice is retained.
type FrontmatterOptions struct {
	enabled    bool
	yaml, toml bool
}

// NewFrontmatterOptions validates the same independent enabled/formats selection
// as the configuration boundary. Nil formats selects the YAML/TOML defaults;
// an explicitly empty list, duplicates and other formats are rejected even when
// recognition is disabled. This does not decode metadata or enable consumers.
func NewFrontmatterOptions(enabled bool, formats []string) (FrontmatterOptions, error) {
	if formats == nil {
		formats = []string{"yaml", "toml"}
	}
	if len(formats) == 0 || len(formats) > 2 {
		return FrontmatterOptions{}, errors.New("frontmatter requires a nonempty unique YAML/TOML selection")
	}
	options := FrontmatterOptions{enabled: enabled}
	for _, format := range formats {
		switch format {
		case "yaml":
			if options.yaml {
				return FrontmatterOptions{}, errors.New("duplicate YAML frontmatter format")
			}
			options.yaml = true
		case "toml":
			if options.toml {
				return FrontmatterOptions{}, errors.New("duplicate TOML frontmatter format")
			}
			options.toml = true
		default:
			return FrontmatterOptions{}, errors.New("unsupported frontmatter format")
		}
	}
	return options, nil
}

// ParseOptions selects the complete GFM profile and independent frontmatter
// recognition. Its zero value has the same CommonMark/disabled behavior as Parse.
// It contains only immutable values and is safe to reuse across operations.
type ParseOptions struct {
	GFM         bool
	Frontmatter FrontmatterOptions
}

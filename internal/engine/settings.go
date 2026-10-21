package engine

import "fmt"

// ResolvedRule exposes catalog defaults, explicit entry presence, executable
// availability and permission independently. Dialect applicability requires
// document context and is deliberately not inferred from these values.
type ResolvedRule struct {
	Enabled         bool
	AllowDirectives bool
	Explicit        bool
	Available       bool
	Options         Options
}

// ResolveSettings validates all supplied settings, including disabled rules,
// and returns owned defaults/options. It does not perform operation capability
// admission: consumers can distinguish explicit requests from omitted defaults.
func (r *Registry) ResolveSettings(settings Settings, providers ValidationProviders) (map[string]ResolvedRule, error) {
	if r == nil {
		return nil, fmt.Errorf("nil registry")
	}
	for _, id := range sortedKeys(settings) {
		if _, ok := r.metadata[id]; !ok {
			return nil, fmt.Errorf("unknown or reserved rule %q", id)
		}
	}
	states := make(map[string]ResolvedRule, len(r.ids))
	for _, id := range r.ids {
		m := r.metadata[id]
		s, explicit := settings[id]
		enabled, directives := m.DefaultEnabled, true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		if s.AllowDirectives != nil {
			directives = *s.AllowDirectives
		}
		resolved, err := m.Options.resolve(s.Options, providers)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if id == "MD073" {
			// Synthetic metadata used by engine consumers need not declare TOC options.
			min, hasMin := resolved["min_level"].(int)
			max, hasMax := resolved["max_level"].(int)
			if hasMin && hasMax && min > max {
				return nil, fmt.Errorf("MD073: min_level exceeds max_level")
			}
			start, hasStart := resolved["start_marker"].(string)
			end, hasEnd := resolved["end_marker"].(string)
			if hasStart && hasEnd && start == end {
				return nil, fmt.Errorf("MD073: markers must be distinct")
			}
		}
		states[id] = ResolvedRule{Enabled: enabled, AllowDirectives: directives, Explicit: explicit, Available: r.Available(id), Options: resolved}
	}
	return states, nil
}

// SettingFromMapping is a filesystem- and syntax-independent rule mapping
// boundary. Syntax adapters must reject duplicate keys before constructing it.
// A nil mapping represents null and is invalid; a nonnil empty mapping retains
// defaults. Shared booleans are kept separate from analysis-specific options.
func SettingFromMapping(mapping map[string]any) (RuleSetting, error) {
	if mapping == nil {
		return RuleSetting{}, fmt.Errorf("rule setting must be a mapping, not null")
	}
	setting := RuleSetting{Options: Options{}}
	for _, key := range sortedKeys(mapping) {
		value := mapping[key]
		if key == "enabled" || key == "allow_directives" {
			b, ok := value.(bool)
			if !ok {
				return RuleSetting{}, fmt.Errorf("%s must be boolean", key)
			}
			if key == "enabled" {
				setting.Enabled = &b
			} else {
				setting.AllowDirectives = &b
			}
			continue
		}
		setting.Options[key] = cloneValue(value)
	}
	return setting, nil
}

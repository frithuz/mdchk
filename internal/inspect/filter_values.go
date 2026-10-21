package inspect

import (
	"encoding/json"
)

// The owned JSON authority has the same stable wire representation as the
// public projection. Restore its documented Go value types at the package API
// boundary: JSON's generic numbers/lists must not weaken engine.Options or
// replace source Marker/Segment/Value records with arbitrary maps.
func (b *builder) restoreProjectionValues(p *Projection, s *Snapshot) error {
	decode := func(v any, out any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	// The same immutable acquisition authority supplies canonical Go option
	// values. These defensive accessors do not validate, admit, parse or call a
	// provider again. Source/hash provenance remains the analyzed wire evidence.
	if err := b.charge(262144); err != nil {
		return err
	}
	configuration := s.Configuration()
	if configuration != nil {
		if p.Configuration.Data != nil {
			p.Configuration.Data.Effective = configuration.Shared().Effective
		}
		if p.Rules.Data != nil {
			for i := range *p.Rules.Data {
				r := &(*p.Rules.Data)[i]
				r.Options = configuration.Rules[r.Rule].Options
			}
		}
	}
	if p.AST.Data != nil {
		var visit func(*Node) error
		visit = func(n *Node) error {
			if err := b.charge(1); err != nil {
				return err
			}
			for key, value := range n.Properties {
				switch key {
				case "markers":
					var v []Marker
					if err := decode(value, &v); err != nil {
						return err
					}
					n.Properties[key] = v
				case "content":
					var v []Segment
					if err := decode(value, &v); err != nil {
						return err
					}
					n.Properties[key] = v
				case "prefix", "info", "label", "destination", "reference_label", "title":
					var v Value
					if err := decode(value, &v); err != nil {
						return err
					}
					n.Properties[key] = v
				case "level", "start", "marker_width", "padding", "content_indent", "width", "block_form":
					var v int
					if err := decode(value, &v); err != nil {
						return err
					}
					n.Properties[key] = v
				}
			}
			for i := range n.Children {
				if err := visit(&n.Children[i]); err != nil {
					return err
				}
			}
			return nil
		}
		return visit(&p.AST.Data.Root)
	}
	return nil
}

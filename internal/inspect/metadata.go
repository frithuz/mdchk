package inspect

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/metadata"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// MetadataRecord reports what the recognized header decodes to, separately from
// the opaque source record and the body AST. Every field beyond State depends on
// it, so absence is meaningful. Ordering holds observations: they are not findings
// and they grant no rule state, completeness or correction authority.
type MetadataRecord struct {
	State         string             `json:"state"`
	Format        string             `json:"format,omitempty"`
	Range         *result.Range      `json:"range,omitempty"`
	ByteRange     *result.ByteRange  `json:"byte_range,omitempty"`
	ContextOnly   *bool              `json:"context_only,omitempty"`
	ExtendsRegion bool               `json:"extends_region,omitempty"`
	Problems      *[]MetadataProblem `json:"problems,omitempty"`
	Title         *MetadataTitle     `json:"title,omitempty"`
	Ordering      *MetadataOrdering  `json:"ordering,omitempty"`
}

// MetadataProblem is one decoder problem at its original location.
type MetadataProblem struct {
	Code      string           `json:"code"`
	Range     result.Range     `json:"range"`
	ByteRange result.ByteRange `json:"byte_range"`
}

// MetadataTitle follows the exact top-level title policy. Only a found title
// carries text and a location.
type MetadataTitle struct {
	Status    string            `json:"status"`
	Text      string            `json:"text,omitempty"`
	Range     *result.Range     `json:"range,omitempty"`
	ByteRange *result.ByteRange `json:"byte_range,omitempty"`
}

// MetadataOrdering applies MD072's effective options whether or not it is enabled.
type MetadataOrdering struct {
	Kind          string            `json:"kind"`
	Recursive     bool              `json:"recursive"`
	CaseSensitive bool              `json:"case_sensitive"`
	Mappings      []MetadataMapping `json:"mappings"`
}

// MetadataMapping is one compared mapping. Synthesized TOML parents have no
// authored location, so their ranges are absent rather than invented.
type MetadataMapping struct {
	Range         *result.Range     `json:"range,omitempty"`
	ByteRange     *result.ByteRange `json:"byte_range,omitempty"`
	Violation     bool              `json:"violation"`
	OffendingKeys []MetadataKey     `json:"offending_keys"`
}

// MetadataKey is a key MD072 would flag. Its ranges are absent when the key has
// no authored location.
type MetadataKey struct {
	Key       string            `json:"key"`
	Range     *result.Range     `json:"range,omitempty"`
	ByteRange *result.ByteRange `json:"byte_range,omitempty"`
}

// metadata decodes on behalf of inspection only, on the shared operation controls
// and after rule execution. Disabled recognition binds nothing; absent and
// unterminated headers never reach the decoder. The caller publishes the record
// only when this returns without a stop, so a stop leaves nothing partial.
func (b *builder) metadata(document *parser.Document, enabled bool, rule engine.ResolvedRule) (MetadataRecord, error) {
	if err := b.charge(16); err != nil {
		return MetadataRecord{}, err
	}
	if err := b.budget.ChargeNodes(4); err != nil {
		return MetadataRecord{}, err
	}
	if !enabled {
		return MetadataRecord{State: "disabled"}, nil
	}
	recursive, okRecursive := rule.Options["recursive"].(bool)
	sensitive, okSensitive := rule.Options["case_sensitive"].(bool)
	if !okRecursive || !okSensitive {
		return MetadataRecord{}, metadata.ErrContract
	}
	binding, err := metadata.Bind(b.ctx, b.ctx, b.budget, document)
	if err != nil {
		return MetadataRecord{}, err
	}
	if binding.State() == metadata.Absent {
		return MetadataRecord{State: "absent"}, nil
	}
	header := binding.Header()
	whole, byteRange, err := b.located(header.Span())
	if err != nil {
		return MetadataRecord{}, err
	}
	contextOnly := false
	out := MetadataRecord{State: "unterminated", Format: header.Format(), Range: whole, ByteRange: byteRange, ContextOnly: &contextOnly}
	if binding.State() == metadata.Unterminated {
		return out, nil
	}
	decoded, err := metadata.Decode(b.ctx, binding)
	if err != nil {
		return MetadataRecord{}, err
	}
	out.State = "incomplete"
	if decoded.Complete() {
		out.State = "complete"
	}
	if out.Problems, err = b.metadataProblems(decoded, binding); err != nil {
		return MetadataRecord{}, err
	}
	if out.Title, err = b.metadataTitle(decoded, binding); err != nil {
		return MetadataRecord{}, err
	}
	orders, err := decoded.Ordering(b.ctx, binding, metadata.OrderOptions{Recursive: recursive, CaseInsensitive: !sensitive})
	if err != nil {
		return MetadataRecord{}, err
	}
	out.Ordering = &MetadataOrdering{Kind: "observation", Recursive: recursive, CaseSensitive: sensitive, Mappings: make([]MetadataMapping, 0, len(orders))}
	for _, order := range orders {
		mapping, err := b.metadataMapping(order)
		if err != nil {
			return MetadataRecord{}, err
		}
		out.Ordering.Mappings = append(out.Ordering.Mappings, mapping)
	}
	return out, nil
}

// located resolves a span's public coordinates, or nothing for an absent span.
func (b *builder) located(span parser.Span) (*result.Range, *result.ByteRange, error) {
	if !span.Present() {
		return nil, nil, nil
	}
	r, err := b.rangeOf(span.Start(), span.End())
	if err != nil {
		return nil, nil, err
	}
	return &r, optionalSpan(span), nil
}

func (b *builder) metadataProblems(decoded metadata.Result, binding metadata.Binding) (*[]MetadataProblem, error) {
	problems, err := decoded.SortedProblems(b.ctx, binding)
	if err != nil {
		return nil, err
	}
	if err := b.charge(16 * int64(len(problems))); err != nil {
		return nil, err
	}
	if err := b.budget.ChargeNodes(4 * int64(len(problems))); err != nil {
		return nil, err
	}
	out := make([]MetadataProblem, 0, len(problems))
	for _, problem := range problems {
		r, err := b.rangeOf(problem.Location.Start(), problem.Location.End())
		if err != nil {
			return nil, err
		}
		out = append(out, MetadataProblem{Code: string(problem.Code), Range: r, ByteRange: bytesOf(problem.Location)})
	}
	return &out, nil
}

func (b *builder) metadataTitle(decoded metadata.Result, binding metadata.Binding) (*MetadataTitle, error) {
	title, err := decoded.Title(b.ctx, binding)
	if err != nil {
		return nil, err
	}
	if !title.Known {
		return &MetadataTitle{Status: "unknown"}, nil
	}
	if !title.Present {
		return &MetadataTitle{Status: "missing"}, nil
	}
	if err := b.chargeCopy(title.Text); err != nil {
		return nil, err
	}
	r, byteRange, err := b.located(title.Location)
	if err != nil {
		return nil, err
	}
	return &MetadataTitle{Status: "found", Text: title.Text, Range: r, ByteRange: byteRange}, nil
}

func (b *builder) metadataMapping(order metadata.Order) (MetadataMapping, error) {
	if err := b.charge(int64(order.Count()) + 16); err != nil {
		return MetadataMapping{}, err
	}
	if err := b.budget.ChargeNodes(int64(order.Count()) + 4); err != nil {
		return MetadataMapping{}, err
	}
	r, byteRange, err := b.located(order.Mapping().Span())
	if err != nil {
		return MetadataMapping{}, err
	}
	out := MetadataMapping{Range: r, ByteRange: byteRange, Violation: order.Violation(), OffendingKeys: []MetadataKey{}}
	for _, offending := range order.OffendingKeys() {
		key := offending.Entry
		if err := b.chargeCopy(key.Key()); err != nil {
			return MetadataMapping{}, err
		}
		r, byteRange, err := b.located(key.KeySpan())
		if err != nil {
			return MetadataMapping{}, err
		}
		out.OffendingKeys = append(out.OffendingKeys, MetadataKey{Key: key.Key(), Range: r, ByteRange: byteRange})
	}
	return out, nil
}

// chargeCopy reserves one decoded string record before it joins the projection,
// which the report and the retained authority image copy again.
func (b *builder) chargeCopy(text string) error {
	if err := b.charge(8*int64(len(text)) + 16); err != nil {
		return err
	}
	return b.budget.ChargeNodes(int64(len(text)) + 4)
}

package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestReferenceLinkDocumentBindings(t *testing.T) {
	source := NewSource([]byte("[visible][key]\n\n[key]: /uri \"\"\n[key]: /other\n"))
	r := Parse(context.Background(), source)
	if r.Status != Complete {
		t.Fatal(r)
	}
	original := r.Document.root.spec.Children[0].spec.Children[0]
	rebuild := func(p *LinkProperties) (*Node, error) {
		linkSpec := original.spec
		linkSpec.Properties = Properties{Link: p}
		link, err := NewNode(linkSpec)
		if err != nil {
			return nil, err
		}
		paragraph := r.Document.root.spec.Children[0].spec
		paragraph.Children = []*Node{link}
		block, err := NewNode(paragraph)
		if err != nil {
			return nil, err
		}
		root := r.Document.root.spec
		root.Children = []*Node{block}
		return NewNode(root)
	}
	for _, mutate := range []func(*LinkProperties){
		func(p *LinkProperties) { p.NormalizedLabel = Some("missing") },
		func(p *LinkProperties) { p.Destination = projectReference(Value{Semantic: Some("/wrong")}) },
		func(p *LinkProperties) { p.Title = nil },
		func(p *LinkProperties) { v := projectReference(Value{Semantic: Some("wrong")}); p.Title = &v },
	} {
		p := original.Properties().Link
		mutate(p)
		root, err := rebuild(p)
		if err != nil {
			t.Fatal("valid local shape rejected", err)
		}
		if _, err := NewDocument(root, r.Document.References()); !errors.Is(err, ErrModel) {
			t.Fatal("accepted unbound/mismatched resolved reference", p, err)
		}
	}
	for _, mutate := range []func(*ReferenceCollection){
		func(r *ReferenceCollection) { r.LookupAvailability = Unavailable; r.Lookup = nil },
		func(r *ReferenceCollection) { r.LookupAvailability = Partial; delete(r.Lookup, "key") },
		func(r *ReferenceCollection) { r.Lookup["key"] = 1 },
		func(r *ReferenceCollection) {
			other := Parse(context.Background(), NewSource(source.Bytes()))
			r.Definitions = other.Document.References().Definitions
		},
	} {
		refs := r.Document.References()
		mutate(&refs)
		if _, err := NewDocument(r.Document.Root(), refs); !errors.Is(err, ErrModel) {
			t.Fatal("accepted unavailable/foreign/later binding", err)
		}
	}
	for _, mutate := range []func(*LinkProperties){
		func(p *LinkProperties) { p.ReferenceLabel = nil },
		func(p *LinkProperties) { v := projectReference(Value{Semantic: Some("key")}); p.ReferenceLabel = &v },
		func(p *LinkProperties) { p.Origin = "shortcut" },
		func(p *LinkProperties) { p.Origin = "collapsed" },
		func(p *LinkProperties) { p.Origin = "inline" },
		func(p *LinkProperties) { p.Destination = r.Document.references.Definitions[0].spec.Destination },
		func(p *LinkProperties) { p.NormalizedLabel = Optional[string]{} },
		func(p *LinkProperties) { p.Destination.Segments[0].Transformation = MappingUnavailable },
		func(p *LinkProperties) {
			p.ReferenceLabel.Segments[0].Source = Parse(context.Background(), NewSource(source.Bytes())).Document.root.Span()
		},
	} {
		p := original.Properties().Link
		mutate(p)
		if _, err := rebuild(p); !errors.Is(err, ErrModel) {
			t.Fatal("invalid reference shape/source accepted", p, err)
		}
	}
	if _, err := NewDocument(r.Document.Root(), r.Document.References()); err != nil {
		t.Fatal("valid binding rejected", err)
	}
	before := r.Document.Root()
	r.Document.references.LookupAvailability = Unavailable
	if state, err := parseInlines(context.Background(), r.Document, PhaseComplete); state != NotStarted || err != nil || !reflect.DeepEqual(before, r.Document.Root()) {
		t.Fatal("unavailable registry published inline work", state, err)
	}
}

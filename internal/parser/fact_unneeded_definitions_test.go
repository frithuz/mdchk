package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type md053Want struct {
	start, end int
	label      string
	shadowed   bool
}

func md053Check(t *testing.T, got UnneededDefinitionInventory, index LineIndex, want []md053Want) {
	t.Helper()
	var seen []md053Want
	for _, d := range got.Definitions {
		if !d.Span.present || d.Span.SourceToken() != index.source.Token() {
			t.Fatal(d)
		}
		seen = append(seen, md053Want{d.Span.start, d.Span.end, d.Label, d.Shadowed})
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatal(seen, want)
	}
}

func TestUnneededDefinitionsInventory(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		ignored      []string
		want         []md053Want
	}{
		{"empty", "", nil, nil},
		{"unused", "[unused]: guide.md", nil, []md053Want{{0, 9, "unused", false}}},
		{"used-full", "[Guide][manual]\n\n[manual]: guide.md", nil, nil},
		{"used-collapsed", "[Manual][]\n\n[manual]: u", nil, nil},
		{"used-shortcut", "[manual]\n\n[manual]: u", nil, nil},
		{"used-image", "![logo]\n\n[logo]: u", nil, nil},
		{"used-before", "[a]: u\n\n[a]", nil, nil},
		{"inline-link", "[a](u)\n\n[a]: v", nil, []md053Want{{8, 12, "a", false}}},
		{"shadowed-used", "[a]\n\n[a]: x\n[A]: y\n", nil, []md053Want{{12, 16, "a", true}}},
		{"shadowed-unused", "[a]: x\n[a]: y\n", nil, []md053Want{{0, 4, "a", false}, {7, 11, "a", true}}},
		{"folding", "[Foo  Bar]\n\n[foo bar]: x\n[FOO\tBAR]: y\n", nil, []md053Want{{25, 35, "foo bar", true}}},
		{"full-folding", "[ẞ]: x\n[ss]: y\n", nil, []md053Want{{0, 6, "ss", false}, {9, 14, "ss", true}}},
		{"quoted-list", "> - [x]: u\n", nil, []md053Want{{4, 8, "x", false}}},
		{"quoted-list-used", "> - [x]: u\n\n[x]\n", nil, nil},
		{"nested-list", "- a\n  - [y]: u\n", nil, []md053Want{{8, 12, "y", false}}},
		{"code-span", "`[x]`\n\n[x]: u\n", nil, []md053Want{{7, 11, "x", false}}},
		{"fenced", "```\n[x]\n```\n\n[x]: u\n", nil, []md053Want{{13, 17, "x", false}}},
		{"html-block", "<div>\n[x]\n</div>\n\n[x]: u\n", nil, []md053Want{{18, 22, "x", false}}},
		{"inline-html", "a <span>[x]</span>\n\n[x]: u\n", nil, nil},
		{"ignored", "[foo bar]: x\n[FOO BAR]: y\n", []string{"Foo  Bar"}, []md053Want{{13, 23, "foo bar", true}}},
		{"ignored-folded", "[ss]: x\n", []string{"ẞ"}, nil},
		{"ignored-empty", "[a]: x\n", []string{"", " \t "}, []md053Want{{0, 4, "a", false}}},
		{"ignored-brackets", "[a]: x\n", []string{"[a]"}, []md053Want{{0, 4, "a", false}}},
		{"multi-line", "[foo\nbar]: x\n", nil, []md053Want{{0, 10, "foo bar", false}}},
		{"crlf", "[a]\r\n\r\n[a]: x\r\n[A]: y\r\n", nil, []md053Want{{15, 19, "a", true}}},
	} {
		for _, gfm := range []bool{false, true} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, index, o := md011Parse(t, tc.source, gfm)
				got, e := d.UnneededDefinitions(context.Background(), b, index, o, tc.ignored)
				if e != nil || got.Shadowed != Available || got.Unused != Available {
					t.Fatal(got, e)
				}
				md053Check(t, got, index, tc.want)
			})
		}
	}
}

func TestUnneededDefinitionsUnavailableDefinitions(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			d, b, index, o := md011Parse(t, "[a]: x\n[a]: y\n", gfm)
			d.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
			got, e := d.UnneededDefinitions(context.Background(), b, index, o, nil)
			if e != nil || got.Shadowed != Unavailable || got.Unused != Unavailable || got.Definitions != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestUnneededDefinitionsUnavailableUses(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"lookup", "resolution", "link-properties"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, index, o := md011Parse(t, "[b]\n\n[a]: x\n[b]: y\n[B]: z\n", gfm)
				before, e := d.UnneededDefinitions(context.Background(), b, index, o, nil)
				if e != nil || before.Unused != Available {
					t.Fatal("actual positive prerequisite", before, e)
				}
				md053Check(t, before, index, []md053Want{{5, 9, "a", false}, {19, 23, "b", true}})
				switch fault {
				case "lookup":
					d.references.LookupAvailability = Unavailable
					d.references.Lookup = nil
				case "resolution":
					md070Nodes(d, Link)[0].spec.Properties.Link.Resolution = Partial
				case "link-properties":
					md070Nodes(d, Link)[0].spec.Properties.Link = nil
				}
				got, e := d.UnneededDefinitions(context.Background(), b, index, o, nil)
				if e != nil || got.Shadowed != Available || got.Unused != Unavailable {
					t.Fatal(got, e)
				}
				md053Check(t, got, index, []md053Want{{19, 23, "b", true}})
			})
		}
	}
}

func TestUnneededDefinitionsModelFaults(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"missing-colon", "foreign-marker", "lookup-miss"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				source := "[a]: x\n" // reported, so its markers are read
				if fault == "lookup-miss" {
					source = "[a]\n\n[a]: x\n"
				}
				d, b, index, o := md011Parse(t, source, gfm)
				switch fault {
				case "missing-colon":
					d.references.Definitions[0].spec.Markers = d.references.Definitions[0].spec.Markers[:2]
				case "foreign-marker":
					d.references.Definitions[0].spec.Markers[0].Source.source = NewSource(index.source.Bytes())
				case "lookup-miss":
					md070Nodes(d, Link)[0].spec.Properties.Link.NormalizedLabel = Some("missing")
				}
				got, e := d.UnneededDefinitions(context.Background(), b, index, o, nil)
				if !errors.Is(e, ErrModel) || !reflect.DeepEqual(got, UnneededDefinitionInventory{}) {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestUnneededDefinitionsRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		const source = "[a]: x\n[a]: y\n"
		d, b, i, o := md011Parse(t, source, gfm)
		ignored := []string{"b"}
		before, e := d.UnneededDefinitions(context.Background(), b, i, o, ignored)
		if e != nil || len(before.Definitions) != 2 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.UnneededDefinitions(context.Background(), b, i, o, ignored)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Definitions) > 0 {
					got.Definitions[0] = UnneededDefinition{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != source || !reflect.DeepEqual(ignored, []string{"b"}) {
			t.Fatal("original source/tree changed")
		}
	}
}

func TestUnneededDefinitionsBudgetAndCancellation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			d, b, i, o := md011Parse(t, "[unused]: guide.md", gfm)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, e := d.UnneededDefinitions(ctx, b, i, o, nil); !errors.Is(e, context.Canceled) || !reflect.DeepEqual(got, UnneededDefinitionInventory{}) {
				t.Fatal(got, e)
			}
			if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
				t.Fatal(e)
			}
			var stop *LimitExceeded
			for range 2 {
				if got, e := d.UnneededDefinitions(context.Background(), b, i, o, nil); !errors.As(e, &stop) || !reflect.DeepEqual(got, UnneededDefinitionInventory{}) {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestUnneededDefinitionsOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			d, b, i, o := md011Parse(t, "[unused]: guide.md", gfm)
			constructed, e := NewDocument(d.Root(), d.References())
			if e != nil {
				t.Fatal(e)
			}
			other, _ := NewOperationBudget(DefaultLimits())
			for _, call := range []func() (UnneededDefinitionInventory, error){
				func() (UnneededDefinitionInventory, error) {
					return constructed.UnneededDefinitions(context.Background(), b, i, o, nil)
				},
				func() (UnneededDefinitionInventory, error) {
					return d.UnneededDefinitions(context.Background(), other, i, o, nil)
				},
				func() (UnneededDefinitionInventory, error) {
					return d.UnneededDefinitions(context.Background(), b, NewLineIndex(NewSource([]byte("[unused]: guide.md"))), o, nil)
				},
				func() (UnneededDefinitionInventory, error) {
					return d.UnneededDefinitions(context.Background(), b, i, ParseOptions{GFM: !gfm}, nil)
				},
			} {
				if got, e := call(); !errors.Is(e, ErrModel) || !reflect.DeepEqual(got, UnneededDefinitionInventory{}) {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestUnneededDefinitionsManyDefinitionsWithinBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			var text strings.Builder
			for n := range count {
				fmt.Fprintf(&text, "[d%d]: u\n[D%d]: v\n", n, n)
			}
			source := NewSource([]byte(text.String()))
			limits := DefaultLimits()
			limits.WorkUnits = 2048*int64(source.Len()) + 1<<20
			b, e := NewOperationBudget(limits)
			if e != nil {
				t.Fatal(e)
			}
			o := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, o)
			if p.Status != Complete || p.Document == nil {
				t.Fatal(p)
			}
			got, e := p.Document.UnneededDefinitions(context.Background(), b, NewLineIndex(source), o, nil)
			if e != nil || got.Unused != Available || len(got.Definitions) != 2*count {
				t.Fatal(len(got.Definitions), got.Unused, e)
			}
		})
	}
}

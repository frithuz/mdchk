package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFencedLanguagesOriginalPrivateAuthority(t *testing.T) {
	d, b, index := md014Parse(t, "```sh\n$ pwd\n```\n")
	public, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {public, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("```sh\n$ pwd\n```\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		got, err := tc.d.FencedLanguages(context.Background(), tc.b, tc.i, tc.o, nil)
		if !errors.Is(err, ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.FencedLanguages(nil, b, index, ParseOptions{}, nil); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		for _, fullFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile-%d/full-flags-%v", profile, fullFlags), func(t *testing.T) {
				s := NewSource([]byte("```sh\n$ pwd\n```\n"))
				budget, _ := NewOperationBudget(DefaultLimits())
				p := parseWithProfile(context.Background(), s, budget, profile)
				if p.Status != Complete {
					t.Fatal(p)
				}
				if fullFlags {
					p.Document.parsedOptions.GFM = true
				}
				got, err := p.Document.FencedLanguages(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions, nil)
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, nil); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
	source := NewSource([]byte("```sh\n$ pwd\n```\n"))
	budget, _ := NewOperationBudget(DefaultLimits())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := ParseWithOptions(cancelled, source, budget, ParseOptions{})
	if partial.Status == Complete {
		t.Fatal("genuine interrupted parse completed")
	}
	if got, err := partial.Document.FencedLanguages(context.Background(), budget, NewLineIndex(source), ParseOptions{}, nil); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection527W/2N.
func TestFencedLanguagesStaticEmptyOriginalReservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, nodes := range []bool{false, true} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("gfm-%v/nodes-%v/delta-%d", gfm, nodes, delta), func(t *testing.T) {
					s := NewSource(nil)
					b, _ := NewOperationBudget(DefaultLimits())
					o := ParseOptions{GFM: gfm}
					p := ParseWithOptions(context.Background(), s, b, o)
					if p.Status != Complete {
						t.Fatal(p)
					}
					charge, limit, setup, residual := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(527)
					if gfm {
						setup = 30
					}
					if nodes {
						charge, limit, setup, residual = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
					}
					if err := charge(limit - setup - residual - delta); err != nil {
						t.Fatal(err)
					}
					got, err := p.Document.FencedLanguages(context.Background(), b, NewLineIndex(s), o, nil)
					if delta == 0 {
						if err != nil || got.Availability != Available || len(got.Facts) != 0 {
							t.Fatal(got, err)
						}
					} else {
						for range 2 {
							var stop *LimitExceeded
							if !errors.As(err, &stop) || got.Facts != nil {
								t.Fatal(got, err)
							}
							got, err = p.Document.FencedLanguages(context.Background(), b, NewLineIndex(s), o, nil)
						}
					}
				})
			}
		}
	}
}

func TestFencedLanguagesUnknownRequiredMappingAndDetached(t *testing.T) {
	for _, kind := range []string{"properties", "info", "semantic", "mapping", "bad-marker", "allowed-mapping-irrelevant", "missing-mapping-irrelevant"} {
		t.Run(kind, func(t *testing.T) {
			opener := "```"
			if kind == "missing-mapping-irrelevant" {
				opener = "```{#id}"
			}
			d, b, index := md014Parse(t, opener+"\n```\n\n<div>\nx\n</div>\n\n~~~ruby\n~~~")
			allowed := []string{"go"}
			pre, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, allowed)
			if e != nil || pre.Availability != Available || len(pre.Facts) != 2 || !pre.Facts[0].Missing || pre.Facts[1].Missing {
				t.Fatal("positive", pre, e)
			}
			var first, last *Node
			for _, n := range d.root.spec.Children {
				if n.Kind() == CodeBlock {
					if first == nil {
						first = n
					}
					last = n
				}
			}
			if first == nil || first == last {
				t.Fatal("missing owners")
			}
			switch kind {
			case "properties":
				last.spec.Properties.Code = nil
			case "info":
				last.spec.Properties.Code.Info = nil
			case "semantic":
				last.spec.Properties.Code.Info.Semantic = Optional[string]{}
			case "mapping":
				last.spec.Properties.Code.Info.Segments[0].Transformation = MappingUnavailable
			case "bad-marker":
				last.spec.Markers[0].Source = Span{}
			case "allowed-mapping-irrelevant":
				last.spec.Properties.Code.Info.Segments[0].Transformation = MappingUnavailable
				allowed = []string{"ruby"}
			case "missing-mapping-irrelevant":
				info := first.spec.Properties.Code.Info
				if info == nil || info.Semantic != Some("{#id}") || len(info.Segments) == 0 {
					t.Fatal("expected actual attribute-like info mapping", info)
				}
				info.Segments[0].Transformation = MappingUnavailable
			}
			got, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, allowed)
			switch kind {
			case "bad-marker":
				if !errors.Is(e, ErrModel) || got.Facts != nil {
					t.Fatal(got, e)
				}
			case "allowed-mapping-irrelevant":
				if e != nil || got.Availability != Available || len(got.Facts) != 1 {
					t.Fatal(got, e)
				}
			case "missing-mapping-irrelevant":
				if e != nil || got.Availability != Available || len(got.Facts) != 2 {
					t.Fatal(got, e)
				}
			default:
				if e != nil || got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got, e)
				}
			}
		})
	}
	d, b, index := md014Parse(t, "~~~ruby\n~~~")
	allowed := []string{"go"}
	pre, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, allowed)
	if e != nil || len(pre.Facts) != 1 {
		t.Fatal(pre, e)
	}
	pre.Facts[0] = FencedLanguageFact{}
	if allowed[0] != "go" {
		t.Fatal("mutated borrowed options")
	}
	again, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, allowed)
	if e != nil || len(again.Facts) != 1 || again.Facts[0].Missing || again.Facts[0].Span.Start() != 3 || again.Facts[0].Span.End() != 7 {
		t.Fatal(again, e)
	}
}

func TestFencedLanguagesRawAndUnknownRelevance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		count        int
	}{
		{"raw-protects", "prefix <script>\n\n```\n```", 0},
		{"image-isolates", "![<script>](/x)\n```\n```", 1},
		{"closer-restores", "prefix <script>\n\n</script>\n\n```\n```", 1},
		{"partial-tab", ">\t```\n>\t```", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := md014Parse(t, tc.source)
			got, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, nil)
			if e != nil || got.Availability != Available || len(got.Facts) != tc.count {
				t.Fatal(got, e)
			}
		})
	}
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown-before-allowed-%v", allowed), func(t *testing.T) {
			d, b, index := md014Parse(t, "```\n```\n\n<div>\nx\n</div>\n\n~~~ruby\n~~~")
			var html *Node
			for _, n := range d.root.spec.Children {
				if n.Kind() == HTMLBlock {
					html = n
				}
			}
			if html == nil {
				t.Fatal("missing html owner")
			}
			html.spec.Content[0].Transformation = MappingUnavailable
			list := []string{"go"}
			if allowed {
				list = []string{"ruby"}
			}
			got, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, list)
			if e != nil {
				t.Fatal(e)
			}
			if allowed {
				if got.Availability != Available || len(got.Facts) != 1 {
					t.Fatal(got)
				}
			} else if got.Availability != Unavailable || got.Facts != nil {
				t.Fatal(got)
			}
		})
	}
	d, b, index := md014Parse(t, "prefix <script>\n\n~~~ruby\n~~~")
	for _, n := range d.root.spec.Children {
		if n.Kind() == CodeBlock {
			n.spec.Properties.Code.Info = nil
		}
	}
	got, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, []string{"go"})
	if e != nil || got.Availability != Available || len(got.Facts) != 0 {
		t.Fatal(got, e)
	}
}

func TestFencedLanguagesUnusedTailMappingAndAtomicSource(t *testing.T) {
	d, b, index := md014Parse(t, "~~~ruby &#32;attrs\n~~~")
	code := d.root.spec.Children[0].spec.Properties.Code
	if code == nil || code.Info == nil || len(code.Info.Segments) < 3 {
		t.Fatal("expected actual decoded info segments")
	}
	code.Info.Segments[len(code.Info.Segments)-1].Transformation = MappingUnavailable
	got, e := d.FencedLanguages(context.Background(), b, index, ParseOptions{}, []string{"go"})
	if e != nil || got.Availability != Available || len(got.Facts) != 1 || got.Facts[0].Span.Start() != 3 || got.Facts[0].Span.End() != 7 {
		t.Fatal(got, e)
	}
	// A real multi-scalar entity is one indivisible original contributor.
	d, b, index = md014Parse(t, "~~~&fjlig; opts\n~~~")
	got, e = d.FencedLanguages(context.Background(), b, index, ParseOptions{}, []string{"go"})
	if e != nil || got.Availability != Available || len(got.Facts) != 1 || got.Facts[0].Span.Start() != 3 || got.Facts[0].Span.End() != 10 {
		t.Fatal(got, e)
	}
}

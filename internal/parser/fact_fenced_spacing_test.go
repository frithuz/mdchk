package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFencedSpacingOriginalPrivateAuthority(t *testing.T) {
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
		got, err := tc.d.FencedSpacing(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(err, ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.FencedSpacing(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
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
				got, err := p.Document.FencedSpacing(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.FencedSpacing(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
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
	if got, err := partial.Document.FencedSpacing(context.Background(), budget, NewLineIndex(source), ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection527W/2N.
func TestFencedSpacingStaticEmptyOriginalReservations(t *testing.T) {
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
					got, err := p.Document.FencedSpacing(context.Background(), b, NewLineIndex(s), o)
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
							got, err = p.Document.FencedSpacing(context.Background(), b, NewLineIndex(s), o)
						}
					}
				})
			}
		}
	}
}

func TestFencedSpacingOriginalUnknownAndDetached(t *testing.T) {
	for _, kind := range []string{"code-properties", "html-mapping", "bad-marker", "info-irrelevant"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md014Parse(t, "x\n```\n```\n\n<div>\nx\n</div>\n\ny\n~~~\n~~~")
			pre, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
			if e != nil || pre.Availability != Available || len(pre.Facts) != 2 {
				t.Fatal("positive", pre, e)
			}
			var code, html *Node
			for _, n := range d.root.spec.Children {
				if n.Kind() == CodeBlock {
					code = n
				}
				if n.Kind() == HTMLBlock {
					html = n
				}
			}
			if code == nil || html == nil {
				t.Fatal("missing source owners")
			}
			switch kind {
			case "code-properties":
				code.spec.Properties.Code = nil
			case "html-mapping":
				html.spec.Content[0].Transformation = MappingUnavailable
			case "bad-marker":
				code.spec.Markers[0].Source = Span{}
			case "info-irrelevant":
				code.spec.Properties.Code.Info = nil
			}
			got, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
			switch kind {
			case "bad-marker":
				if !errors.Is(e, ErrModel) || got.Facts != nil {
					t.Fatal(got, e)
				}
			case "info-irrelevant":
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
	d, b, index := md014Parse(t, "x\n```\n```")
	first, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
	if e != nil || len(first.Facts) != 1 {
		t.Fatal(first, e)
	}
	first.Facts[0] = FencedSpacingFact{}
	next, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
	if e != nil || len(next.Facts) != 1 || next.Facts[0].Point.Start() != 2 || next.Facts[0].Point.End() != 2 {
		t.Fatal(next, e)
	}
}

func TestFencedSpacingRawOwnershipAndIrrelevantUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		count        int
	}{
		{"raw-protects", "prefix <script>\n\nx\n```\n```", 0},
		{"image-isolates", "![<script>](/x)\n```\n```", 1},
		{"closer-restores", "prefix <script>\n\n</script>\n\nx\n```\n```", 1},
		{"partial-tab-prefix", ">\ttext\n>\t```\n>\t```", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := md014Parse(t, tc.source)
			got, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
			if e != nil || got.Availability != Available || len(got.Facts) != tc.count {
				t.Fatal(got, e)
			}
		})
	}
	d, b, index := md014Parse(t, "x\n```\n```\n\n<div>\nx")
	last := d.root.spec.Children[len(d.root.spec.Children)-1]
	if last.Kind() != HTMLBlock {
		t.Fatal(last.Kind())
	}
	last.spec.Content[0].Transformation = MappingUnavailable
	got, e := d.FencedSpacing(context.Background(), b, index, ParseOptions{})
	if e != nil || got.Availability != Available || len(got.Facts) != 1 {
		t.Fatal(got, e)
	}
}

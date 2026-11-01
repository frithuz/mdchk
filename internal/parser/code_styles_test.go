package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCodeStylesOriginalPrivateAuthority(t *testing.T) {
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
		got, err := tc.d.CodeStyles(context.Background(), tc.b, tc.i, tc.o, "consistent")
		if !errors.Is(err, ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.CodeStyles(nil, b, index, ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
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
				got, err := p.Document.CodeStyles(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions, "consistent")
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
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
	if got, err := partial.Document.CodeStyles(context.Background(), budget, NewLineIndex(source), ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection571W/2N.
func TestCodeStylesStaticEmptyOriginalReservations(t *testing.T) {
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
					charge, limit, setup, residual := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(571)
					if gfm {
						setup = 30
					}
					if nodes {
						charge, limit, setup, residual = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
					}
					if err := charge(limit - setup - residual - delta); err != nil {
						t.Fatal(err)
					}
					got, err := p.Document.CodeStyles(context.Background(), b, NewLineIndex(s), o, "consistent")
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
							got, err = p.Document.CodeStyles(context.Background(), b, NewLineIndex(s), o, "consistent")
						}
					}
				})
			}
		}
	}
}

func TestCodeStylesUnknownRequiredMappingAndDetached(t *testing.T) {
	for _, kind := range []string{"properties", "marker-absent", "markers-empty", "fence-absent", "width-absent", "bad-marker", "info-irrelevant", "payload-irrelevant", "matching-marker-irrelevant"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md014Parse(t, "```go\n```\n\n~~~ruby\n~~~")
			pre, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "indented")
			if e != nil || pre.Availability != Available || len(pre.Facts) != 2 {
				t.Fatal("positive", pre, e)
			}
			first, last := d.root.spec.Children[0], d.root.spec.Children[1]
			if first.Kind() != CodeBlock || last.Kind() != CodeBlock {
				t.Fatal("actual owners missing")
			}
			style := "indented"
			switch kind {
			case "properties":
				last.spec.Properties.Code = nil
			case "marker-absent":
				last.spec.Markers[0].Source = Span{}
			case "markers-empty":
				last.spec.Markers = nil
			case "fence-absent":
				last.spec.Properties.Code.Fence = Optional[byte]{}
			case "width-absent":
				last.spec.Properties.Code.Width = Optional[int]{}
			case "bad-marker":
				last.spec.Markers[0].Role = "closing"
			case "info-irrelevant":
				last.spec.Properties.Code.Info = nil
			case "payload-irrelevant":
				last.spec.Content = nil
				last.spec.Properties.Literal = Optional[string]{}
			case "matching-marker-irrelevant":
				last.spec.Markers = nil
				style = "fenced"
			}
			got, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, style)
			switch kind {
			case "bad-marker":
				if !errors.Is(e, ErrModel) || got.Facts != nil {
					t.Fatal(got, e)
				}
			case "info-irrelevant", "payload-irrelevant":
				if e != nil || got.Availability != Available || len(got.Facts) != 2 {
					t.Fatal(got, e)
				}
			case "matching-marker-irrelevant":
				if e != nil || got.Availability != Available || len(got.Facts) != 0 {
					t.Fatal(got, e)
				}
			default:
				if e != nil || got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got, e)
				}
			}
		})
	}
	d, b, index := md014Parse(t, "    x")
	pre, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "fenced")
	if e != nil || len(pre.Facts) != 1 {
		t.Fatal(pre, e)
	}
	pre.Facts[0] = CodeStyleFact{}
	again, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "fenced")
	if e != nil || len(again.Facts) != 1 || again.Facts[0].Span.Start() != 0 || again.Facts[0].Span.End() != 4 || again.Facts[0].Expected != "fenced" {
		t.Fatal(again, e)
	}
}

func TestCodeStylesRawAndHomogeneousRelevance(t *testing.T) {
	for _, tc := range []struct {
		name, source, style string
		count               int
	}{
		{"raw-protects", "prefix <script>\n\n```\n```", "indented", 0},
		{"image-isolates", "![<script>](/x)\n```\n```", "indented", 1},
		{"closer-restores", "prefix <script>\n\n</script>\n\n```\n```", "indented", 1},
		{"raw-looking-payload", "```\n<script>\n```\n\n    x", "consistent", 1},
		{"partial-tab", ">\t\tcode\n", "fenced", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := md014Parse(t, tc.source)
			got, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, tc.style)
			if e != nil || got.Availability != Available || len(got.Facts) != tc.count {
				t.Fatal(got, e)
			}
		})
	}
	for _, kind := range []string{"homogeneous", "mixed", "unknown-style", "after-last", "matching-fixed", "missing-homogeneous-marker"} {
		t.Run(kind, func(t *testing.T) {
			tail := "~~~\n~~~"
			if kind == "mixed" {
				tail = "    x"
			}
			source := "```\n```\n\n<div>\nx\n</div>\n\n" + tail
			if kind == "after-last" {
				source = "```\n```\n\n<div>\nx\n</div>"
			}
			d, b, index := md014Parse(t, source)
			var html, last *Node
			for _, n := range d.root.spec.Children {
				if n.Kind() == HTMLBlock {
					html = n
				}
				if n.Kind() == CodeBlock {
					last = n
				}
			}
			if html == nil || last == nil || len(html.spec.Content) == 0 {
				t.Fatal("missing original owners")
			}
			html.spec.Content[0].Transformation = MappingUnavailable
			style := "consistent"
			if kind == "unknown-style" {
				last.spec.Properties.Code = nil
			}
			if kind == "after-last" {
				style = "indented"
			}
			if kind == "matching-fixed" {
				style = "fenced"
			}
			if kind == "missing-homogeneous-marker" {
				last.spec.Markers = nil
			}
			got, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, style)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "mixed" || kind == "unknown-style" {
				if got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got)
				}
			} else {
				want := 0
				if kind == "after-last" {
					want = 1
				}
				if got.Availability != Available || len(got.Facts) != want {
					t.Fatal(got)
				}
			}
		})
	}
	d, b, index := md014Parse(t, "prefix <script>\n\n~~~ruby\n~~~")
	for _, n := range d.root.spec.Children {
		if n.Kind() == CodeBlock {
			n.spec.Properties.Code = nil
		}
	}
	got, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "consistent")
	if e != nil || got.Availability != Available || len(got.Facts) != 0 {
		t.Fatal(got, e)
	}
}

func TestCodeStylesLateIndentedMarkerAndOrderValidation(t *testing.T) {
	for _, kind := range []string{"absent", "role", "foreign", "nonspace", "order"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md014Parse(t, "    x\n\nText\n\n    y")
			pre, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "fenced")
			if e != nil || len(pre.Facts) != 2 {
				t.Fatal("positive", pre, e)
			}
			last := d.root.spec.Children[len(d.root.spec.Children)-1]
			switch kind {
			case "absent":
				last.spec.Markers[0].Source = Span{}
			case "role":
				last.spec.Markers[0].Role = "opening"
			case "foreign":
				last.spec.Markers[0].Source = NewLineIndex(NewSource([]byte("    y"))).span(0, 4)
			case "nonspace":
				last.spec.Markers[0].Source.end++
			case "order":
				last.spec.Envelope.start = 0
			}
			got, e := d.CodeStyles(context.Background(), b, index, ParseOptions{}, "fenced")
			if kind == "absent" {
				if e != nil || got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Facts != nil {
				t.Fatal(got, e)
			}
		})
	}
}

package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFenceStylesOriginalPrivateAuthority(t *testing.T) {
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
		got, err := tc.d.FenceStyles(context.Background(), tc.b, tc.i, tc.o, "consistent")
		if !errors.Is(err, ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.FenceStyles(nil, b, index, ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
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
				got, err := p.Document.FenceStyles(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions, "consistent")
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.FenceStyles(context.Background(), b, index, ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
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
	if got, err := partial.Document.FenceStyles(context.Background(), budget, NewLineIndex(source), ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection571W/2N.
func TestFenceStylesStaticEmptyOriginalReservations(t *testing.T) {
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
					got, err := p.Document.FenceStyles(context.Background(), b, NewLineIndex(s), o, "consistent")
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
							got, err = p.Document.FenceStyles(context.Background(), b, NewLineIndex(s), o, "consistent")
						}
					}
				})
			}
		}
	}
}

func TestFenceStylesLateRequiredMappingAndDetached(t *testing.T) {
	for _, kind := range []string{"properties", "marker-absent", "markers-empty", "fence-absent", "width-absent", "bad-role", "bad-character", "bad-style", "bad-width", "foreign", "bytes", "line", "order", "info-irrelevant", "payload-irrelevant", "closing-irrelevant"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md014Parse(t, "```\na\n```\n\n~~~\nb\n~~~\n\n~~~\nc\n~~~")
			pre, e := d.FenceStyles(context.Background(), b, index, ParseOptions{}, "consistent")
			if e != nil || pre.Availability != Available || len(pre.Facts) != 2 || pre.Facts[0].Span.Start() != 11 || pre.Facts[1].Span.Start() != 22 {
				t.Fatal("positive", pre, e)
			}
			pre.Facts[0] = FenceStyleFact{}
			last := d.root.spec.Children[2]
			if last.Kind() != CodeBlock {
				t.Fatal("actual last owner missing")
			}
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
			case "bad-role":
				last.spec.Markers[0].Role = "closing"
			case "bad-character":
				last.spec.Properties.Code.Fence = Some(byte('!'))
			case "bad-style":
				last.spec.Properties.Code.Style = "other"
			case "bad-width":
				last.spec.Properties.Code.Width = Some(2)
			case "foreign":
				last.spec.Markers[0].Source = NewLineIndex(NewSource([]byte("~~~"))).span(0, 3)
			case "bytes":
				last.spec.Markers[0].Source = index.span(23, 26)
			case "line":
				last.spec.Markers[0].Source = index.span(28, 31)
			case "order":
				last.spec.Envelope.start = 0
			case "info-irrelevant":
				last.spec.Properties.Code.Info = nil
			case "payload-irrelevant":
				last.spec.Content = nil
				last.spec.Properties.Literal = Optional[string]{}
			case "closing-irrelevant":
				last.spec.Markers = last.spec.Markers[:1]
			}
			got, e := d.FenceStyles(context.Background(), b, index, ParseOptions{}, "consistent")
			switch kind {
			case "info-irrelevant", "payload-irrelevant", "closing-irrelevant":
				if e != nil || got.Availability != Available || len(got.Facts) != 2 || got.Facts[0].Span.Start() != 11 || got.Facts[0].Expected != "backtick" {
					t.Fatal(got, e)
				}
			case "properties", "marker-absent", "markers-empty", "fence-absent", "width-absent":
				if e != nil || got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got, e)
				}
			default:
				if !errors.Is(e, ErrModel) || got.Facts != nil {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestFenceStylesRawHomogeneousRelevance(t *testing.T) {
	for _, kind := range []string{"homogeneous", "mixed", "unknown-owner", "unknown-fence", "after-last", "matching-fixed", "missing-homogeneous-marker"} {
		t.Run(kind, func(t *testing.T) {
			tail := "```\n```"
			if kind == "mixed" {
				tail = "~~~\n~~~"
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
				t.Fatal("required actual original owners missing")
			}
			// Prove this exact original HTML owner reaches the uncertainty path.
			pc := &parserContext{Context: context.Background(), budget: &parseBudget{operation: b}}
			_, _, available, e := conflictHTMLRaw(pc, index.source, html, conflictRawState{})
			if e != nil || !available {
				t.Fatal("raw positive control", available, e)
			}
			html.spec.Content[0].Transformation = MappingUnavailable
			_, _, available, e = conflictHTMLRaw(pc, index.source, html, conflictRawState{})
			if e != nil || available {
				t.Fatal("raw fault was not reached", available, e)
			}
			style := "consistent"
			switch kind {
			case "unknown-owner":
				last.spec.Properties.Code = nil
			case "unknown-fence":
				last.spec.Properties.Code.Fence = Optional[byte]{}
			case "after-last":
				style = "tilde"
			case "matching-fixed":
				style = "backtick"
				last.spec.Markers = nil
				last.spec.Properties.Code.Width = Optional[int]{}
			case "missing-homogeneous-marker":
				last.spec.Markers = nil
			}
			got, e := d.FenceStyles(context.Background(), b, index, ParseOptions{}, style)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "mixed" || kind == "unknown-owner" || kind == "unknown-fence" {
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
}

func TestFenceStylesExcludedOwnersAndFixedIrrelevance(t *testing.T) {
	for _, tc := range []struct {
		name, source, style string
		count               int
	}{
		{"raw-protects", "prefix <script>\n\n```\n```", "tilde", 0},
		{"image-isolates", "![<script>](/x)\n```\n```", "tilde", 1},
		{"closer-restores", "prefix <script>\n\n</script>\n\n```\n```", "tilde", 1},
		{"raw-looking-payload", "```\n<script>\n```\n\n~~~\nx\n~~~", "consistent", 1},
		{"partial-tab", ">\t~~~\n>\tx\n>\t~~~", "backtick", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := md014Parse(t, tc.source)
			got, e := d.FenceStyles(context.Background(), b, index, ParseOptions{}, tc.style)
			if e != nil || got.Availability != Available || len(got.Facts) != tc.count {
				t.Fatal(got, e)
			}
		})
	}
	for _, kind := range []string{"indented", "protected-unknown", "fixed-match"} {
		t.Run(kind, func(t *testing.T) {
			source := "    x"
			style := "backtick"
			if kind == "protected-unknown" {
				source = "prefix <script>\n\n~~~ruby\n~~~"
				style = "consistent"
			}
			if kind == "fixed-match" {
				source = "```\nx\n```"
			}
			d, b, index := md014Parse(t, source)
			count := 0
			for _, n := range d.root.spec.Children {
				if n.Kind() == CodeBlock {
					count++
					if kind == "protected-unknown" {
						n.spec.Properties.Code = nil
					} else {
						n.spec.Markers = nil
						n.spec.Properties.Code.Width = Optional[int]{}
						if kind == "indented" {
							n.spec.Properties.Code.Fence = Optional[byte]{}
						}
					}
				}
			}
			if count != 1 {
				t.Fatal("missing actual candidate")
			}
			got, e := d.FenceStyles(context.Background(), b, index, ParseOptions{}, style)
			if e != nil || got.Availability != Available || len(got.Facts) != 0 {
				t.Fatal(got, e)
			}
		})
	}
}

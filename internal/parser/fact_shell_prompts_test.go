package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func md014Parse(t *testing.T, source string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestShellPromptsOriginalPrivateAuthority(t *testing.T) {
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
		got, err := tc.d.ShellPrompts(context.Background(), tc.b, tc.i, tc.o, []string{"sh"})
		if !errors.Is(err, ErrModel) || got.Dollars != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.ShellPrompts(nil, b, index, ParseOptions{}, []string{"sh"}); !errors.Is(err, ErrModel) || got.Dollars != nil {
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
				got, err := p.Document.ShellPrompts(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions, []string{"sh"})
				if !errors.Is(err, ErrModel) || got.Dollars != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, []string{"sh"}); !errors.Is(err, ErrModel) || got.Dollars != nil {
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
	if got, err := partial.Document.ShellPrompts(context.Background(), budget, NewLineIndex(source), ParseOptions{}, []string{"sh"}); !errors.Is(err, ErrModel) || got.Dollars != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection528W/2N.
func TestShellPromptsStaticEmptyOriginalReservations(t *testing.T) {
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
					charge, limit, setup, residual := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(528)
					if gfm {
						setup = 30
					}
					if nodes {
						charge, limit, setup, residual = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
					}
					if err := charge(limit - setup - residual - delta); err != nil {
						t.Fatal(err)
					}
					got, err := p.Document.ShellPrompts(context.Background(), b, NewLineIndex(s), o, []string{"sh"})
					if delta == 0 {
						if err != nil || got.Availability != Available || len(got.Dollars) != 0 {
							t.Fatal(got, err)
						}
					} else {
						for range 2 {
							var stop *LimitExceeded
							if !errors.As(err, &stop) || got.Dollars != nil {
								t.Fatal(got, err)
							}
							got, err = p.Document.ShellPrompts(context.Background(), b, NewLineIndex(s), o, []string{"sh"})
						}
					}
				})
			}
		}
	}
}

func TestShellPromptsStickyControlsAndDetachedFacts(t *testing.T) {
	for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "```sh\n$ pwd\n```\n", "```sh\n$ pwd\n```\n\n<div>\n<x title=\"\n\n```sh\n$ pwd\n```\n"} {
		for _, kind := range []string{"work", "nodes", "elapsed", "cancelled", "deadline"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					d, b, index := md014Parse(t, source)
					ctx := context.Background()
					switch kind {
					case "work":
						_ = b.ChargeWork(b.Limits().WorkUnits)
					case "nodes":
						_ = b.ChargeNodes(b.Limits().TotalNodes)
					case "elapsed":
						time.Sleep(61 * time.Second)
					case "cancelled":
						c, cancel := context.WithCancel(ctx)
						cancel()
						ctx = c
					case "deadline":
						c, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
						defer cancel()
						ctx = c
					}
					for range 2 {
						got, err := d.ShellPrompts(ctx, b, index, ParseOptions{}, []string{"sh"})
						if err == nil || got.Dollars != nil {
							t.Fatal(got, err)
						}
					}
				})
			})
		}
	}
	d, b, index := md014Parse(t, "```sh\n$ pwd\n$ whoami\n$ date\n```\n")
	first, err := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, []string{"sh"})
	if err != nil || len(first.Dollars) != 3 {
		t.Fatal(first, err)
	}
	saved := append([]Span(nil), first.Dollars...)
	first.Dollars[0] = Span{}
	next, err := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, []string{"sh"})
	if err != nil || !reflect.DeepEqual(next.Dollars, saved) {
		t.Fatal(next, err)
	}
}

func TestShellPromptsLateOriginalMappingAndUnknownAtomicity(t *testing.T) {
	for _, kind := range []string{"foreign-source", "missing-semantic", "unavailable-mapping", "bad-ending", "missing-line", "bad-marker"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md014Parse(t, "```sh\n$ pwd\n$ whoami\n```\n")
			pre, e := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, []string{"sh"})
			if e != nil || pre.Availability != Available || len(pre.Dollars) != 2 || pre.Dollars[0].start != 6 || pre.Dollars[1].start != 12 {
				t.Fatal("positive original precondition", pre, e)
			}
			n := d.root.spec.Children[0]
			switch kind {
			case "foreign-source":
				s := NewSource([]byte("$ whoami"))
				n.spec.Content[2].Source, _ = s.Span(0, 8)
			case "missing-semantic":
				n.spec.Content[2].Semantic = Optional[string]{}
			case "unavailable-mapping":
				n.spec.Content[2].Transformation = MappingUnavailable
			case "bad-ending":
				n.spec.Content[3].Semantic = Some("x")
			case "missing-line":
				n.spec.Content = n.spec.Content[:2]
			case "bad-marker":
				n.spec.Markers[len(n.spec.Markers)-1].Source = Span{}
			default:
				t.Fatal(kind)
			}
			got, e := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, []string{"sh"})
			if got.Dollars != nil {
				t.Fatal("discarded prefix escaped", got, e)
			}
			if kind == "missing-semantic" || kind == "unavailable-mapping" {
				if e != nil || got.Availability != Unavailable {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) {
				t.Fatal(got, e)
			}
		})
	}
}

func TestShellPromptsNecessaryInfoAndIrrelevantOwnership(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("protected-%v", raw), func(t *testing.T) {
			text := "```sh\n$ pwd\n```\n"
			if raw {
				text = "prefix <script>\n\n" + text
			}
			d, b, index := md014Parse(t, text)
			n := d.root.spec.Children[len(d.root.spec.Children)-1]
			if n.spec.Kind != CodeBlock {
				t.Fatal(n.spec.Kind)
			}
			n.spec.Properties.Code.Info.Semantic = Optional[string]{}
			for _, languages := range [][]string{{}, {"sh"}} {
				got, e := d.ShellPrompts(context.Background(), b, index, ParseOptions{}, languages)
				want := Available
				if len(languages) > 0 && !raw {
					want = Unavailable
				}
				if e != nil || got.Availability != want || len(got.Dollars) != 0 {
					t.Fatal(got, e, want)
				}
			}
		})
	}
}

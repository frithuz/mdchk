package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func tableSpacingParse(t *testing.T, text string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	source := NewSource([]byte(text))
	budget, _ := NewOperationBudget(DefaultLimits())
	parsed := ParseWithOptions(context.Background(), source, budget, ParseOptions{GFM: true})
	if parsed.Status != Complete {
		t.Fatal(parsed)
	}
	return parsed.Document, budget, NewLineIndex(source)
}

type tableSpacingPoint struct {
	at     int
	before bool
}

func TestTableSpacingNeighboursOwnersAndPoints(t *testing.T) {
	for _, tc := range []struct {
		source string
		points []tableSpacingPoint
	}{
		{"| a |\n| - |\n", nil},
		{"# T\n\n| a |\n| - |\n\n# N\n", nil},
		{"# T\n| a |\n| - |\n", []tableSpacingPoint{{4, true}}},
		{"# T\r\n| a |\r\n| - |\r\n# N\r\n", []tableSpacingPoint{{5, true}, {17, false}}},
		{"```\nx\n```\n| a |\n| - |\n", []tableSpacingPoint{{10, true}}},
		{"***\n| a |\n| - |\n", []tableSpacingPoint{{4, true}}},
		{"[r]: /u\n| a |\n| - |\n", []tableSpacingPoint{{8, true}}},
		{"| a |\n| - |\n# N\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n```\nx\n```\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n> q\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n- i\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n1. i\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n<div>\n", []tableSpacingPoint{{11, false}}},
		{"| a |\n| - |\n***\n", []tableSpacingPoint{{11, false}}},
		{"| a |  \n| - |\t\n# N\n", []tableSpacingPoint{{14, false}}},
		{"# T\n  | a |\n  | - |\n", []tableSpacingPoint{{4, true}}},
		{"> # T\n> | a |\n> | - |\n", []tableSpacingPoint{{8, true}}},
		{"> # T\n>\n> | a |\n> | - |\n>\n> # N\n", nil},
		{"> # T\n> \t\n> | a |\n> | - |\n", nil},
		{"> | a |\n> | - |\n", nil},
		{"> | a |\n> | - |\n\n# N\n", nil},
		{"- | a |\n  | - |\n", nil},
		{"- | a |\n  | - |\n  # N\n", []tableSpacingPoint{{15, false}}},
		{"- > # T\n  > | a |\n  > | - |\n", []tableSpacingPoint{{12, true}}},
		{"```\n| a |\n| - |\n```\n", nil},
		{"    | a |\n    | - |\n", nil},
		{"<div>\n| a |\n| - |\n", nil},
		{"Text\nA | B\n--- | ---\n", nil},
		{"a <script>\n\n# T\n| a |\n| - |\n", nil},
		{"![<script>](x)\n\n# T\n| a |\n| - |\n", []tableSpacingPoint{{20, true}}},
		{"---\nx: 1\n---\n| a |\n| - |\n", nil},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: true}
			if tc.source[:3] == "---" {
				fm, _ := NewFrontmatterOptions(true, nil)
				options.Frontmatter = fm
			}
			p := ParseWithOptions(context.Background(), source, b, options)
			if p.Status != Complete {
				t.Fatal(p)
			}
			got, err := p.Document.TableSpacing(context.Background(), b, NewLineIndex(source), options)
			if err != nil {
				t.Fatal(err)
			}
			var points []tableSpacingPoint
			for _, f := range got {
				if !f.Point.Present() || f.Point.Start() != f.Point.End() || f.Point.SourceToken() != source.Token() {
					t.Fatal(f)
				}
				points = append(points, tableSpacingPoint{f.Point.Start(), f.Before})
			}
			if !reflect.DeepEqual(points, tc.points) {
				t.Fatal(points, tc.points)
			}
		})
	}
}

func TestTableSpacingIgnoresThematicBreaksAndCommonMark(t *testing.T) {
	source := "Text\n***\n| a |\n| - |\n"
	d, b, index := tableSpacingParse(t, source)
	got, err := d.TableSpacing(context.Background(), b, index, ParseOptions{GFM: true})
	if err != nil || len(got) != 1 || got[0].Point.Start() != 9 || !got[0].Before {
		t.Fatal(got, err)
	}
	thematic, err := d.ThematicSpacing(context.Background(), b, index, ParseOptions{GFM: true})
	if err != nil || len(thematic) != 2 || thematic[0].Point.Start() != 5 || thematic[1].Point.Start() != 8 {
		t.Fatal(thematic, err)
	}
	d, b, index = sectionParse(t, "# T\n| a |\n| - |\n")
	if got, err := d.TableSpacing(context.Background(), b, index, ParseOptions{}); err != nil || got != nil {
		t.Fatal("CommonMark has no tables", got, err)
	}
}

func TestTableSpacingRejectsDetachedAndMalformedAuthority(t *testing.T) {
	gfm := ParseOptions{GFM: true}
	d, b, index := tableSpacingParse(t, "# T\n| a |\n| - |\n")
	constructed, err := NewDocument(d.Root(), d.References())
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d       *Document
		b       *OperationBudget
		index   LineIndex
		options ParseOptions
	}{
		{nil, b, index, gfm}, {constructed, b, index, gfm}, {d, nil, index, gfm}, {d, foreign, index, gfm},
		{d, b, NewLineIndex(NewSource([]byte("# T\n| a |\n| - |\n"))), gfm}, {d, b, index, ParseOptions{}},
	} {
		if got, e := tc.d.TableSpacing(context.Background(), tc.b, tc.index, tc.options); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	for _, invalidate := range []func(){func() { d.parsedComplete = false }, func() { d.parsedProfile = parserProfile(255) }} {
		d, b, index = tableSpacingParse(t, "# T\n| a |\n| - |\n")
		invalidate()
		if got, e := d.TableSpacing(context.Background(), b, index, gfm); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	d, b, index = tableSpacingParse(t, "[a]: /a\n| a |\n| - |\n\n[b]: /b")
	d.references.Definitions[0], d.references.Definitions[1] = d.references.Definitions[1], d.references.Definitions[0]
	if got, e := d.TableSpacing(context.Background(), b, index, gfm); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	//nolint:staticcheck // Deliberate absent caller.
	if got, e := d.TableSpacing(nil, b, index, gfm); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

// These totals were measured once from the held source and are pinned to catch
// silent changes to the shared body walk's reservations.
var tableSpacingCosts = []struct {
	name, source string
	facts        int
	work, nodes  int64
	final        int
}{
	{"empty", "", 0, 16, 4, 15},
	{"clean", "clean", 0, 53, 15, 40},
	{"edge", "| a |\n| - |", 0, 111, 34, 81},
	{"protected", "```\n| a |\n| - |\n```", 0, 56, 12, 36},
	{"violation", "# T\n| a |\n| - |\n# N", 2, 194, 49, 137},
	{"definitions", "[r]: /x\n| a |\n| - |", 1, 133, 37, 92},
	{"rollback", "> # T\n> | a |\n> | - |\n> # N", 2, 301, 65, 170},
	{"growth", "| a |\n| - |\n# M\n| b |\n| - |\n# N", 3, 302, 87, 206},
}

func TestTableSpacingStaticReservations(t *testing.T) {
	for _, tc := range tableSpacingCosts {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(tc.name+"/"+kind+map[int64]string{-1: "/short", 0: "/exact"}[delta], func(t *testing.T) {
					d, b, index := tableSpacingParse(t, tc.source)
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, e := d.TableSpacing(context.Background(), b, index, ParseOptions{GFM: true})
					if delta == 0 {
						if e != nil || len(got) != tc.facts {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							var limit *LimitExceeded
							if !errors.As(e, &limit) || got != nil {
								t.Fatal("direct producer withholding", got, e)
							}
							got, e = d.TableSpacing(context.Background(), b, index, ParseOptions{GFM: true})
						}
					}
				})
			}
		}
	}
}

func TestTableSpacingCallerAndOriginControls(t *testing.T) {
	for _, tc := range tableSpacingCosts {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+"/caller/"+reason.Error(), func(t *testing.T) {
				d, b, index := tableSpacingParse(t, tc.source)
				caller := &thematicSpacingStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, e := d.TableSpacing(caller, b, index, ParseOptions{GFM: true})
				if !errors.Is(e, reason) || got != nil || caller.calls != tc.final {
					t.Fatal(got, e, caller.calls, tc.final)
				}
			})
			t.Run(tc.name+"/origin/"+reason.Error(), func(t *testing.T) {
				origin := &thematicSpacingStop{Context: context.Background(), stop: 1 << 30, reason: reason}
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				p := ParseWithOptions(origin, source, b, ParseOptions{GFM: true})
				if p.Status != Complete {
					t.Fatal(p)
				}
				origin.stop = origin.calls + tc.final
				got, e := p.Document.TableSpacing(blockquoteOperation{Context: context.Background(), origin: origin}, b, NewLineIndex(source), ParseOptions{GFM: true})
				if !errors.Is(e, reason) || got != nil || origin.calls != origin.stop {
					t.Fatal(got, e, origin.calls)
				}
			})
		}
	}
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := tableSpacingParse(t, "# T\n| a |\n| - |\n# N")
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.TableSpacing(context.Background(), b, index, ParseOptions{GFM: true})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
}

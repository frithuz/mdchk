package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestThematicHeadingProjectionReservesBeforeAllocation(t *testing.T) {
	for _, text := range []string{"***\n# H", "> ***\n> # H"} {
		d, b, index := sectionParse(t, text)
		b.limits.TotalNodes = b.nodes + 1
		b.limits.DocumentNodes = b.limits.TotalNodes
		got, err := d.ThematicBreaksBeforeHeadings(context.Background(), b, index, ParseOptions{})
		var limit *LimitExceeded
		if !errors.As(err, &limit) || got != nil {
			t.Fatal(text, got, err)
		}
	}
}
func TestThematicHeadingProjectionOriginalMarkerWork(t *testing.T) {
	d, b, index := sectionParse(t, strings.Repeat("* ", 3000)+"\n# H")
	b.limits.WorkUnits = b.work + 128
	got, err := d.ThematicBreaksBeforeHeadings(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "work-units" || got != nil {
		t.Fatal(got, err)
	}
}
func TestThematicHeadingProjectionOwnedFacts(t *testing.T) {
	d, b, index := sectionParse(t, "***\n# A\n\n> ___\n> # B\n\n***\n# C")
	got, err := d.ThematicBreaksBeforeHeadings(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	for i, span := range got {
		if span.SourceToken() != d.Source().Token() || i > 0 && got[i-1].Start() >= span.Start() {
			t.Fatal(got)
		}
	}
	got[0] = Span{}
	again, err := d.ThematicBreaksBeforeHeadings(context.Background(), b, index, ParseOptions{})
	if err != nil || !again[0].Present() {
		t.Fatal(again, err)
	}
}

func TestThematicHeadingProjectionReservesStackBeforeGrowth(t *testing.T) {
	d, b, index := sectionParse(t, "> words")
	b.limits.TotalNodes = b.nodes + 1
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.ThematicBreaksBeforeHeadings(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}

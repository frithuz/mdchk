package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHeadingConstructProjectionOwnsNestedKindsAndSpans(t *testing.T) {
	d, b, index := sectionParse(t, "# [**x**](u) ![*y*](u) `z`")
	got, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
	kinds := []Kind{Link, Strong, Image, Emphasis, CodeSpan}
	starts := []int{2, 3, 13, 15, 23}
	ends := []int{12, 8, 22, 18, 26}
	if err != nil || len(got) != 5 {
		t.Fatal(got, err)
	}
	for i, f := range got {
		if f.Kind != kinds[i] || f.Span.Start() != starts[i] || f.Span.End() != ends[i] || f.Span.SourceToken() != d.Source().Token() {
			t.Fatal(i, f)
		}
	}
	got[0] = HeadingConstruct{}
	again, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
	if err != nil || again[0].Kind != Link {
		t.Fatal(again, err)
	}
}
func TestHeadingConstructProjectionReservesStackAndFacts(t *testing.T) {
	for _, text := range []string{"# *x*", "> # *x*"} {
		d, b, index := sectionParse(t, text)
		b.limits.TotalNodes = b.nodes + 1
		b.limits.DocumentNodes = b.limits.TotalNodes
		got, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
		var limit *LimitExceeded
		if !errors.As(err, &limit) || got != nil {
			t.Fatal(got, err)
		}
	}
}
func TestHeadingConstructProjectionOriginalLongWork(t *testing.T) {
	d, b, index := sectionParse(t, "# "+strings.Repeat("*x* ", 2000))
	b.limits.WorkUnits = b.work + 128
	got, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "work-units" || got != nil {
		t.Fatal(got, err)
	}
}

func TestHeadingConstructProjectionFixedFinalCheck(t *testing.T) {
	d, b, index := sectionParse(t, "")
	// Empty actual syntax has one entry check, one root-frame observation, then
	// this fixed third observation at final publication. Do not derive the stop
	// from a successful run of the instrumented method.
	caller := &headingConstructStop{Context: context.Background(), stop: 3}
	got, err := d.HeadingConstructs(caller, b, index, ParseOptions{})
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal(got, err, caller.calls)
	}
}

type headingConstructStop struct {
	context.Context
	stop, calls int
}

func (c *headingConstructStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return context.Canceled
	}
	return nil
}

func TestHeadingConstructProjectionReservesFactBeforePublication(t *testing.T) {
	d, b, index := sectionParse(t, "# `x`")
	// One initial frame and its two-record growth fit; the output record does not.
	b.limits.TotalNodes = b.nodes + 3
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}

func TestHeadingConstructProjectionReservesOnlyStack(t *testing.T) {
	d, b, index := sectionParse(t, "# plain")
	b.limits.TotalNodes = b.nodes + 1
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.HeadingConstructs(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}

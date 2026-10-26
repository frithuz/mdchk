package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestListProjectionOwnsGroupsItemsAndMarkers(t *testing.T) {
	d, b, index := sectionParse(t, "- + nested\n1. last")
	got, err := d.ListItems(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	for i, w := range []struct{ start, end int }{{0, 1}, {2, 3}, {11, 13}} {
		f := got[i]
		if f.Marker.Start() != w.start || f.Marker.End() != w.end || f.Marker.SourceToken() != d.Source().Token() {
			t.Fatal(i, f)
		}
	}
	if got[0].Bullet != Some(byte('-')) || got[1].Bullet != Some(byte('+')) || got[0].ParentItem.Present() || got[1].ParentItem != got[0].Item || got[0].Group == got[1].Group || got[2].ParentItem.Present() || !got[2].Ordered || got[2].Start != Some(1) || got[2].Delimiter != Some(byte('.')) {
		t.Fatal(got)
	}
	got[0] = ListItemFact{}
	again, err := d.ListItems(context.Background(), b, index, ParseOptions{})
	if err != nil || again[0].Marker.Start() != 0 || !again[0].Marker.Present() {
		t.Fatal(again, err)
	}
}
func TestListProjectionReservesOnlyStack(t *testing.T) {
	d, b, index := sectionParse(t, "> plain")
	b.limits.TotalNodes = b.nodes + 1
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.ListItems(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}
func TestListProjectionReservesFactBeforePublication(t *testing.T) {
	d, b, index := sectionParse(t, "-")
	// Root plus the two-record group-frame growth fit. The empty item has no
	// descendant stack growth that could mask the missing fact reservation.
	b.limits.TotalNodes = b.nodes + 3
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.ListItems(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}
func TestListProjectionOriginalLongWork(t *testing.T) {
	d, b, index := sectionParse(t, strings.Repeat("- item\n", 2000))
	b.limits.WorkUnits = b.work + 128
	got, err := d.ListItems(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "work-units" || got != nil {
		t.Fatal(got, err)
	}
}
func TestListProjectionFixedFinalCheck(t *testing.T) {
	d, b, index := sectionParse(t, "")
	caller := &headingConstructStop{Context: context.Background(), stop: 3}
	got, err := d.ListItems(caller, b, index, ParseOptions{})
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal(got, err, caller.calls)
	}
}

package rules

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

const (
	tocListUnavailable    = "Table of contents list context is unavailable; MD073 comparisons were withheld."
	tocAnchorsUnavailable = "Heading anchor context is unavailable or uncertain; MD073 comparisons were withheld."
	tocOwnershipUncertain = "A heading's table of contents region could not be decided; MD073 comparisons were withheld."
	tocRegionUncertain    = "A table of contents region does not hold exactly one comparable list; that region was not compared."
)

type tocEntry struct {
	label, destination string
	link               bool
	parent             int
	span               parser.Span
}

type tocRegion struct{ start, end int }

// tableOfContents compares each reliable marked region's list with the
// document's selected headings. Marker problems are reported at the marker;
// uncertain ownership, anchors or list shape reduce coverage instead.
func tableOfContents(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	toc, err := c.TableOfContents(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if toc.Availability != parser.Available {
		return engine.RuleOutcome{IncompleteReason: tocListUnavailable}
	}
	if len(toc.Markers) == 0 {
		return engine.RuleOutcome{}
	}
	markers := toc.Markers
	var findings []engine.Finding
	var regions []tocRegion
	valid := make([]bool, len(markers))
	open, tainted := -1, false
	for i, m := range markers {
		if err := c.HeadingAnchorCheckpoint(ctx, 2, 1); err != nil {
			return engine.RuleOutcome{Err: err}
		}
		switch {
		case m.Start && open < 0:
			open, tainted = i, false
		case m.Start:
			tainted = true
			findings = append(findings, engine.Finding{Span: m.Envelope, Message: "Table of contents start marker is nested inside another table of contents region."})
		case open < 0:
			findings = append(findings, engine.Finding{Span: m.Envelope, Message: "Table of contents end marker has no matching start marker."})
		default:
			if markers[open].Container != m.Container {
				findings = append(findings, engine.Finding{Span: m.Envelope, Message: "Table of contents end marker must be in the same container as its start marker."})
			} else if !tainted {
				valid[open], valid[i] = true, true
				regions = append(regions, tocRegion{open, i})
			}
			open = -1
		}
	}
	if open >= 0 {
		findings = append(findings, engine.Finding{Span: markers[open].Envelope, Message: "Table of contents start marker has no matching end marker."})
	}
	headings, availability, err := c.HeadingAnchors(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if availability != resource.Available {
		return tocOutcome(findings, tocAnchorsUnavailable)
	}
	expected, uncertain, err := tocExpected(ctx, c, headings, markers, valid)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if uncertain {
		return tocOutcome(findings, tocOwnershipUncertain)
	}
	reason := ""
	for _, r := range regions {
		actual, ok, err := tocActual(ctx, c, toc.Blocks, markers[r.start], markers[r.end])
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if !ok {
			reason = tocRegionUncertain
			continue
		}
		finding, found, err := tocCompare(ctx, c, expected, actual, markers[r.end].Envelope)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if found {
			findings = append(findings, finding)
		}
	}
	return tocOutcome(findings, reason)
}

func tocOutcome(findings []engine.Finding, reason string) engine.RuleOutcome {
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Span.Start() < findings[j].Span.Start() })
	return engine.RuleOutcome{Findings: findings, IncompleteReason: reason}
}

// tocExpected selects main-flow headings in range outside every valid region
// and nests each under the nearest earlier shallower selection. It reports
// uncertainty when a heading that would be selected lies between markers that
// do not bound either the outside or a valid region.
func tocExpected(ctx context.Context, c engine.RuleContext, headings []resource.HeadingAnchor, markers []parser.TOCMarker, valid []bool) ([]tocEntry, bool, error) {
	minimum, _ := c.Options()["min_level"].(int)
	maximum, _ := c.Options()["max_level"].(int)
	var expected []tocEntry
	var levels []int
	var stack []int
	next := 0
	for _, h := range headings {
		if err := c.HeadingAnchorCheckpoint(ctx, int64(len(h.Containers)+len(h.Visible))+4, 0); err != nil {
			return nil, false, err
		}
		if h.Level < minimum || h.Level > maximum || len(h.Containers) != 0 {
			continue
		}
		for next < len(markers) && markers[next].Envelope.Start() < h.Envelope.Start() {
			next++
		}
		previous := next - 1
		outsideBefore := previous < 0 || !markers[previous].Start && valid[previous]
		outsideAfter := next == len(markers) || markers[next].Start && valid[next]
		inside := previous >= 0 && markers[previous].Start && valid[previous] && next < len(markers) && !markers[next].Start && valid[next]
		if inside {
			continue
		}
		if !outsideBefore || !outsideAfter {
			return nil, true, nil
		}
		if !h.ID.Present || h.ID.Value == "" {
			continue
		}
		for len(stack) != 0 && levels[stack[len(stack)-1]] >= h.Level {
			stack = stack[:len(stack)-1]
		}
		parent := -1
		if len(stack) != 0 {
			parent = stack[len(stack)-1]
		}
		if err := c.HeadingAnchorCheckpoint(ctx, int64(len(expected))/8+2, 1); err != nil {
			return nil, false, err
		}
		stack = append(stack, len(expected))
		levels = append(levels, h.Level)
		expected = append(expected, tocEntry{label: h.Visible, destination: h.ID.Value, link: true, parent: parent})
	}
	return expected, false, nil
}

// tocActual reads the entries of one valid region. It reports false when the
// region's direct blocks hold no list beside other content, or several lists.
func tocActual(ctx context.Context, c engine.RuleContext, blocks []parser.TOCBlock, start, end parser.TOCMarker) ([]tocEntry, bool, error) {
	from, to, container := start.Line.End(), end.Line.Start(), start.Container
	first := sort.Search(len(blocks), func(i int) bool { return blocks[i].Envelope.Start() >= from })
	if err := c.HeadingAnchorCheckpoint(ctx, int64(len(blocks)/64)+1, 0); err != nil {
		return nil, false, err
	}
	list, lists, others, seen := -1, 0, 0, false
	for j := first; j < len(blocks) && blocks[j].Envelope.Start() < to; j++ {
		if err := c.HeadingAnchorCheckpoint(ctx, 1, 0); err != nil {
			return nil, false, err
		}
		if blocks[j].Parent != container {
			continue
		}
		title := !seen && blocks[j].Kind == parser.Heading
		seen = true
		switch {
		case title:
		case blocks[j].Kind == parser.List:
			list = j
			lists++
		default:
			others++
		}
	}
	if lists > 1 || lists == 0 && others != 0 {
		return nil, false, nil
	}
	if list < 0 {
		return nil, true, nil
	}
	var actual []tocEntry
	entries := map[int]int{}
	for j := list + 1; j < len(blocks) && blocks[j].Envelope.Start() < blocks[list].Envelope.End(); j++ {
		if err := c.HeadingAnchorCheckpoint(ctx, int64(len(blocks[j].Label))+2, 0); err != nil {
			return nil, false, err
		}
		b := blocks[j]
		if b.Kind != parser.ListItem {
			continue
		}
		parent := -1
		if b.Parent != list {
			owner := blocks[b.Parent]
			index, nested := entries[owner.Parent]
			if owner.Kind != parser.List || !nested {
				continue
			}
			parent = index
		}
		if err := c.HeadingAnchorCheckpoint(ctx, int64(len(actual))/8+2, 2); err != nil {
			return nil, false, err
		}
		entries[j] = len(actual)
		actual = append(actual, tocEntry{label: b.Label, destination: b.Destination.Value, link: b.Link, parent: parent, span: b.Marker})
	}
	return actual, true, nil
}

// tocCompare reports the first preorder difference between the two outlines.
func tocCompare(ctx context.Context, c engine.RuleContext, expected, actual []tocEntry, end parser.Span) (engine.Finding, bool, error) {
	for i := 0; i < len(expected) || i < len(actual); i++ {
		switch {
		case i == len(actual):
			e := expected[i]
			return engine.Finding{Span: end, Message: fmt.Sprintf("Table of contents is missing an entry for heading \"%s\" (#%s).", tocCollapse(e.label), e.destination)}, true, nil
		case i == len(expected):
			return engine.Finding{Span: actual[i].span, Message: "Table of contents entry has no matching heading."}, true, nil
		}
		e, a := expected[i], actual[i]
		if err := c.HeadingAnchorCheckpoint(ctx, 2*int64(len(e.label)+len(a.label)+len(e.destination)+len(a.destination))+4, 0); err != nil {
			return engine.Finding{}, false, err
		}
		if !a.link || a.parent != e.parent || tocCollapse(a.label) != tocCollapse(e.label) || !tocDestination(a.destination, e.destination) {
			return engine.Finding{Span: a.span, Message: fmt.Sprintf("Table of contents entry does not match heading \"%s\" (#%s).", tocCollapse(e.label), e.destination)}, true, nil
		}
	}
	return engine.Finding{}, false, nil
}

func tocCollapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// tocDestination accepts only a same-document fragment that, percent-decoded
// once as UTF-8, spells the expected ID exactly.
func tocDestination(destination, id string) bool {
	fragment, ok := strings.CutPrefix(destination, "#")
	if !ok {
		return false
	}
	decoded, err := url.PathUnescape(fragment)
	return err == nil && utf8.ValidString(decoded) && decoded == id
}

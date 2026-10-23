package parser

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PandocListProperties retain the selected reader's numbering independently of
// the core list contract. Item fields are absent on the grouped List. Decimal
// alone grants a source prefix to MD029; semantic numbering never grants it.
// Marker/Content/Container columns use the recursive reader frame; Source
// columns use original physical visual positions. Neither is a public scalar
// position. Tabs retain source bytes even when the two frames expand differently.
type PandocListProperties struct {
	Style, Delimiter                                string
	MarkerStyle                                     string
	Start                                           int
	Reset                                           Optional[int]
	Marker, Decimal, Separator, Label               Span
	MarkerColumn, ContentColumn, ContinuationIndent int
	SeparatorColumns                                int
	SeparatorPartialTab                             bool
	ContainerColumn                                 int
	LeadingColumns                                  int
	SourceMarkerColumn, SourceContentColumn         int
}

// PandocTaskProperties are block-owned literal task source facts. InlinePending
// explicitly withholds interpretation of escaped/entity/link-dependent forms.
// Separator is absent for a terminal marker; no source whitespace is invented.
type PandocTaskProperties struct {
	Marker, Check, Separator Span
	Segments                 []Span
	CheckSegments            []Span
	Checked                  bool
	CheckSpelling            string
	InlinePending            bool
}

// PandocLineProperties retain the source-backed indentation of one logical line.
// Its semantic projection is NBSP; Content retains raw inline payload, including
// separately mapped physical continuations. No inline tree is implied.
type PandocLineProperties struct {
	Indentation Value
	Blank       bool
}

// DefinitionProperties identifies compact versus blank-separated term bodies.
type DefinitionProperties struct{ Tight bool }

func validPandocSpan(outer, inner Span) bool {
	if !contains(outer, inner) || inner.start < 0 || inner.end < inner.start || inner.end > inner.source.Len() {
		return false
	}
	raw := inner.source.content
	for _, pos := range []int{inner.start, inner.end} {
		if pos < len(raw) && !utf8.RuneStart(raw[pos]) {
			return false
		}
		if pos > 0 && pos < len(raw) && raw[pos-1] == '\r' && raw[pos] == '\n' {
			return false
		}
	}
	return true
}
func pandocColumn(source Source, offset int) int {
	start := offset
	for start > 0 && source.content[start-1] != '\n' && source.content[start-1] != '\r' {
		start--
	}
	col := 0
	for start < offset {
		r, n := utf8.DecodeRuneInString(source.content[start:offset])
		if r == '\t' {
			col += 4 - col%4
		} else {
			col++
		}
		start += n
	}
	return col
}
func validatePandocSpec(s NodeSpec) error {
	if s.Properties.PandocList != nil || s.Kind == DefinitionList || s.Kind == DefinitionItem || s.Kind == DefinitionBody || s.Kind == LineBlock {
		previous := s.Envelope.start
		for _, child := range s.Children {
			if child == nil || !validPandocSpan(s.Envelope, child.Span()) || child.Span().start < previous {
				return ErrModel
			}
			previous = child.Span().end
		}
	}

	p := s.Properties.PandocList
	if p != nil {
		if s.Kind != List && s.Kind != ListItem && s.Kind != DefinitionBody {
			return ErrModel
		}
		switch p.Style {
		case "bullet":
			if p.Delimiter != "bullet" {
				return ErrModel
			}
		case "definition":
			if p.Delimiter != "definition" || s.Kind != DefinitionBody {
				return ErrModel
			}
		case "decimal", "lower-alpha", "upper-alpha", "lower-roman", "upper-roman", "example":
			if p.Delimiter != "period" && p.Delimiter != "one-paren" && p.Delimiter != "two-parens" {
				return ErrModel
			}
		case "default":
			if p.Delimiter != "default" && p.Delimiter != "one-paren" && p.Delimiter != "two-parens" {
				return ErrModel
			}
		default:
			return ErrModel
		}
		if (p.Style == "bullet" || p.Style == "definition") && (p.Decimal.Present() || p.Label.Present()) {
			return ErrModel
		}
		if p.Start < 0 || p.Reset.Present && (p.Style != "example" || p.Reset.Value < 0) {
			return ErrModel
		}
		if s.Kind == List {
			if s.Properties.List == nil || s.Properties.List.Prefix != nil || s.Properties.List.MarkerWidth.Present || s.Properties.List.Padding.Present || s.Properties.List.ContentIndent.Present || len(s.Children) == 0 {
				return ErrModel
			}
			if s.Properties.List == nil || p.MarkerStyle != "" || p.Marker.Present() || p.Decimal.Present() || p.Separator.Present() || p.Label.Present() || p.Reset.Present || p.MarkerColumn != 0 || p.ContentColumn != 0 || p.SeparatorColumns != 0 || p.SeparatorPartialTab || p.ContainerColumn != 0 || p.LeadingColumns != 0 || p.SourceMarkerColumn != 0 || p.SourceContentColumn != 0 || p.ContinuationIndent != 0 {
				return ErrModel
			}
			first := s.Children[0]
			if first == nil || first.spec.Properties.List == nil || first.spec.Properties.PandocList == nil {
				return ErrModel
			}
			initial := first.spec.Properties.List
			if p.Start != first.spec.Properties.PandocList.Start || s.Properties.List.Ordered != initial.Ordered || s.Properties.List.Start != initial.Start || s.Properties.List.Bullet != initial.Bullet || s.Properties.List.Delimiter != initial.Delimiter {
				return ErrModel
			}
			for _, child := range s.Children {
				if child == nil {
					return ErrModel
				}
				if child.Kind() != ListItem || child.spec.Properties.PandocList == nil {
					return ErrModel
				}
				x := child.spec.Properties.PandocList
				if x.Style != p.Style || x.Delimiter != p.Delimiter {
					return ErrModel
				}
			}
		} else {
			if !validPandocSpan(s.Envelope, p.Marker) || p.Marker.start == p.Marker.end || p.SourceMarkerColumn != pandocColumn(s.Envelope.source, p.Marker.start) || p.MarkerColumn != p.ContainerColumn+p.LeadingColumns || p.ContainerColumn < 0 || p.LeadingColumns < 0 || p.LeadingColumns > 3 || p.ContinuationIndent < 1 {
				return ErrModel
			}
			raw := p.Marker.source.content[p.Marker.start:p.Marker.end]
			width := utf8.RuneCountInString(raw)
			if p.Style == "bullet" {
				if p.MarkerStyle != "bullet" {
					return ErrModel
				}
				if len(raw) != 1 || !strings.Contains("*+-", raw) || p.Start != 0 {
					return ErrModel
				}
			} else if p.Style == "definition" {
				if p.MarkerStyle != "definition" {
					return ErrModel
				}
				if raw != ":" && raw != "~" || p.Start != 0 {
					return ErrModel
				}
			} else {
				// A declared ordered delimiter must exist in original source.
				// In particular the legacy field cannot legitimize a bare digit.
				if !strings.HasSuffix(raw, ".") && !strings.HasSuffix(raw, ")") {
					return ErrModel
				}
				if strings.HasPrefix(raw, "(") && strings.HasSuffix(raw, ".") {
					return ErrModel
				}
				body := strings.TrimPrefix(raw, "(")
				body = body[:len(body)-1]
				delim := "period"
				if strings.HasSuffix(raw, ")") {
					delim = "one-paren"
					if strings.HasPrefix(raw, "(") {
						delim = "two-parens"
					}
				}
				if p.Style == "default" && delim == "period" {
					delim = "default"
				}
				if delim != p.Delimiter {
					return ErrModel
				}
				style, n, _, decimal, ok := pandocNumber(body, p)
				if !ok {
					return ErrModel
				}
				if p.MarkerStyle != style || body != "#" && style != p.Style && !(p.Style == "default" && style == "decimal") {
					return ErrModel
				}
				if p.Style != "example" && p.Start != n {
					return ErrModel
				}
				if decimal != p.Decimal.Present() {
					return ErrModel
				}
				if decimal {
					if !validPandocSpan(p.Marker, p.Decimal) || p.Decimal.source.content[p.Decimal.start:p.Decimal.end] != body {
						return ErrModel
					}
				}
				at := strings.IndexByte(body, '@')
				if at >= 0 {
					labelStart := p.Marker.start + at + 1
					if strings.HasPrefix(raw, "(") {
						labelStart++
					}
					if !validPandocSpan(p.Marker, p.Label) || p.Label.start != labelStart || p.Label.end != labelStart+len(body[at+1:]) || p.Label.source.content[p.Label.start:p.Label.end] != body[at+1:] {
						return ErrModel
					}
					reset, err := strconv.Atoi(body[:at])
					validReset := at > 0 && err == nil
					if p.Reset.Present != validReset || validReset && p.Reset.Value != reset {
						return ErrModel
					}
				} else if p.Label.Present() || p.Reset.Present {
					return ErrModel
				}
			}
			end := p.Marker.end
			if p.Separator.Present() {
				if !validPandocSpan(s.Envelope, p.Separator) || p.Separator.start != end || p.Separator.start == p.Separator.end || strings.Trim(p.Separator.source.content[p.Separator.start:p.Separator.end], " \t") != "" {
					return ErrModel
				}
			}
			// Reader columns restart at container entry; source columns remain in
			// the original physical frame. The first payload byte can be a shared tab.
			payloadOffset := p.Marker.end
			if p.Separator.Present() {
				payloadOffset = p.Separator.end
			}
			readerColumn := p.MarkerColumn + width
			if p.Separator.Present() {
				sep := p.Separator.source.content[p.Separator.start:p.Separator.end]
				for i := range len(sep) {
					if sep[i] == ' ' {
						readerColumn++
					} else {
						next := readerColumn + 4 - readerColumn%4
						if p.SeparatorPartialTab && i == len(sep)-1 {
							if p.ContentColumn <= readerColumn || p.ContentColumn >= next {
								return ErrModel
							}
							readerColumn = p.ContentColumn
						} else {
							readerColumn = next
						}
					}
				}
				if p.SeparatorPartialTab {
					if sep[len(sep)-1] != '\t' {
						return ErrModel
					}
					payloadOffset--
				}
			} else if p.SeparatorPartialTab {
				return ErrModel
			}
			if p.SourceContentColumn != pandocColumn(s.Envelope.source, payloadOffset) || p.ContentColumn != readerColumn || p.SeparatorColumns != p.ContentColumn-p.MarkerColumn-width || p.SeparatorColumns < 0 || p.SeparatorColumns > 4 {
				return ErrModel
			}

			// Both ordinary items and definition bodies derive continuation from
			// the validated reader-frame marker and consumed separator facts.
			// Bodies intentionally have no legacy ListProperties.
			if p.Style != "example" && p.ContinuationIndent != p.LeadingColumns+width+max(1, p.SeparatorColumns) {
				return ErrModel
			}

			if s.Kind == ListItem && (s.Properties.List == nil || s.Properties.List.Ordered != (p.Style != "bullet") || s.Properties.List.ContentIndent != Some(p.ContinuationIndent) || s.Properties.List.Start.Present && s.Properties.List.Start.Value != p.Start) {
				return ErrModel
			}
			if s.Kind == ListItem {
				l := s.Properties.List
				if l.Prefix == nil || validateValue(s.Envelope, *l.Prefix) != nil || l.MarkerWidth != Some(width) || l.Padding != Some(max(1, p.SeparatorColumns)) {
					return ErrModel
				}
				if p.Style == "bullet" {
					if l.Bullet != Some(raw[0]) || l.Start.Present || l.Delimiter.Present {
						return ErrModel
					}
				} else if !l.Start.Present || l.Start.Value != p.Start || l.Bullet.Present || l.Delimiter != Some(raw[len(raw)-1]) {
					return ErrModel
				}
				leading := p.LeadingColumns
				if leading < 0 || leading > 3 {
					return ErrModel
				}
				if l.Prefix.Semantic != Some(strings.Repeat(" ", leading)+raw+strings.Repeat(" ", p.SeparatorColumns)) {
					return ErrModel
				}
				found := false
				for _, m := range s.Markers {
					if m.Role == "item-marker" {
						if m.Source != p.Marker {
							return ErrModel
						}
						found = true
					}
				}
				if !found {
					return ErrModel
				}
			}
			if p.Style == "example" && p.ContinuationIndent != 4 {
				return ErrModel
			}
		}
	}
	if s.Kind == DefinitionList {
		if len(s.Children) == 0 {
			return ErrModel
		}
		for _, n := range s.Children {
			if n == nil {
				return ErrModel
			}
			if n.Kind() != DefinitionItem {
				return ErrModel
			}
		}
	}
	if s.Kind == DefinitionItem {
		if len(s.Children) < 2 || s.Children[0] == nil || s.Children[0].Kind() != DefinitionTerm || s.Properties.Definition == nil {
			return ErrModel
		}
		for _, n := range s.Children[1:] {
			if n == nil {
				return ErrModel
			}
			if n.Kind() != DefinitionBody {
				return ErrModel
			}
		}
	}
	if s.Kind == DefinitionBody && (p == nil || p.Style != "definition") {
		return ErrModel
	}
	if s.Kind == DefinitionItem && s.Properties.Definition.Tight != !strings.ContainsAny(s.Envelope.source.content[s.Children[0].Span().End():s.Children[1].Span().Start()], "\r\n") {
		return ErrModel
	}
	if s.Properties.Definition != nil && s.Kind != DefinitionItem {
		return ErrModel
	}
	if s.Kind == LineBlockLine && s.Properties.PandocLine == nil {
		return ErrModel
	}
	if s.Kind == LineBlock {
		if len(s.Children) == 0 {
			return ErrModel
		}
		for _, n := range s.Children {
			if n == nil {
				return ErrModel
			}
			if n.Kind() != LineBlockLine {
				return ErrModel
			}
		}
	}
	t := s.Properties.PandocTask
	if t != nil {
		if s.Kind != ListItem && s.Kind != DefinitionBody && s.Kind != Paragraph || !validPandocSpan(s.Envelope, t.Marker) || !validPandocSpan(t.Marker, t.Check) || !t.InlinePending {
			return ErrModel
		}
		raw := s.Envelope.source.content
		if t.Marker.end-t.Marker.start < 3 || raw[t.Marker.start] != '[' || raw[t.Marker.end-1] != ']' || t.Check.start != t.Marker.start+1 && len(t.Segments) == 1 || t.Check.end > t.Marker.end-1 {
			return ErrModel
		}
		if t.CheckSpelling != raw[t.Check.start:t.Check.end] || t.Checked != (t.CheckSpelling == "x" || t.CheckSpelling == "X") || len(t.CheckSegments) == 0 {
			return ErrModel
		}
		last := t.Check.start
		for _, part := range t.CheckSegments {
			if !validPandocSpan(t.Check, part) || part.start == part.end || part.start < last {
				return ErrModel
			}
			value := raw[part.start:part.end]
			if !t.Checked && strings.Trim(value, " \t\r\n\v\f") != "" {
				return ErrModel
			}
			last = part.end
		}
		if t.CheckSegments[0].start != t.Check.start || last != t.Check.end || t.Checked && (len(t.CheckSegments) != 1 || t.CheckSegments[0] != t.Check) {
			return ErrModel
		}
		if t.Separator.Present() && (!validPandocSpan(s.Envelope, t.Separator) || t.Separator.start != t.Marker.end || !taskToken(t.Separator)) {
			return ErrModel
		}
		expected := make([]Span, 0, len(t.CheckSegments)+2)
		expected = append(expected, Span{source: t.Marker.source, start: t.Marker.start, end: t.Marker.start + 1, present: true})
		expected = append(expected, t.CheckSegments...)
		expected = append(expected, Span{source: t.Marker.source, start: t.Marker.end - 1, end: t.Marker.end, present: true})

		canonical := make([]Span, 0, len(expected))
		for _, part := range expected {
			if len(canonical) > 0 && canonical[len(canonical)-1].end == part.start {
				canonical[len(canonical)-1].end = part.end
			} else {
				canonical = append(canonical, part)
			}
		}
		if !slices.Equal(canonical, t.Segments) {
			return ErrModel
		}
		if s.Kind == ListItem || s.Kind == DefinitionBody {
			if len(s.Children) == 0 {
				return ErrModel
			}
			for _, child := range s.Children[1:] {
				if child == nil {
					return ErrModel
				}
				if child.spec.Properties.PandocTask != nil {
					return ErrModel
				}
			}
			if p == nil || len(s.Children) == 0 || s.Children[0].Kind() != Paragraph || !reflect.DeepEqual(t, s.Children[0].spec.Properties.PandocTask) {
				return ErrModel
			}
		}
		if s.Kind == Paragraph {
			candidate := s
			candidate.Properties.PandocTask = nil
			recognizePandocTask(context.Background(), LineIndex{source: s.Envelope.source}, &candidate)
			if !reflect.DeepEqual(t, candidate.Properties.PandocTask) {
				return ErrModel
			}
			if len(s.Content) == 0 || s.Content[0].Source.start != t.Marker.start {
				return ErrModel
			}
			if !t.Separator.Present() {
				for _, part := range s.Content {
					if part.Source.Present() && part.Source.end > t.Marker.end {
						return ErrModel
					}
				}
			}
		}
	} else if s.Kind == ListItem || s.Kind == DefinitionBody {
		for _, child := range s.Children {
			if child == nil {
				return ErrModel
			}
			if child.spec.Properties.PandocTask != nil {
				return ErrModel
			}
		}
	}
	if l := s.Properties.PandocLine; l != nil {
		var expected strings.Builder
		for _, part := range l.Indentation.Segments {
			if !validPandocSpan(s.Envelope, part.Source) {
				return ErrModel
			}
			raw := part.Source.source.content[part.Source.start:part.Source.end]
			if strings.Trim(raw, " \t") != "" {
				return ErrModel
			}
			if !l.Blank {
				expected.WriteString(strings.Repeat("\u00a0", len(raw)))
			}
		}
		if l.Blank && len(s.Content) != 0 {
			return ErrModel
		}
		if l.Indentation.Semantic != Some(expected.String()) {
			return ErrModel
		}

		if s.Kind != LineBlockLine {
			return ErrModel
		}
		if err := validateValue(s.Envelope, l.Indentation); err != nil {
			return err
		}
	}
	return nil
}

func clonePandocProperties(p *Properties) {
	p.Definition = clonePointer(p.Definition)
	p.PandocList = clonePointer(p.PandocList)
	p.PandocTask = clonePointer(p.PandocTask)
	if p.PandocTask != nil {
		p.PandocTask.Segments = slices.Clone(p.PandocTask.Segments)
		p.PandocTask.CheckSegments = slices.Clone(p.PandocTask.CheckSegments)
	}
	p.PandocLine = clonePointer(p.PandocLine)
	if p.PandocLine != nil {
		p.PandocLine.Indentation = cloneValue(p.PandocLine.Indentation)
	}
}

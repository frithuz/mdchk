package parser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzGFMProfile exercises actual private integrated parsing, not the exporter.
// Arbitrary inputs and a constrained independently known layout complement each
// other: determinism alone cannot detect a consistently wrong table/marker.
func FuzzGFMProfile(f *testing.F) {
	for _, input := range []string{gfmMixed, "", "\xff", "> - [x] ~~é\t\x00\r\n>   fin~~ www.a.b <title>\r\n", "| a | b |\n| - | - |\n| `x\\|y` | ~a~~b~ | <!--c--> |\n", "<http://foo.bar/baz bim> <foo\\+bar@example.com>"} {
		f.Add(input)
	}
	data, err := os.ReadFile("testdata/gfm/0.29-gfm/examples.json")
	if err != nil {
		f.Fatal(err)
	}
	var examples []struct{ Markdown, Scope string }
	if err := json.Unmarshal(data, &examples); err != nil {
		f.Fatal(err)
	}
	if len(examples) != 677 {
		f.Fatal("published seed inventory")
	}
	for _, e := range examples {
		if e.Scope != "base" {
			f.Add(e.Markdown)
		}
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			t.Skip()
		}
		l := DefaultLimits()
		l.WorkUnits = 2000000
		l.DocumentNodes = 30000
		l.TotalNodes = 100000
		caller := []byte(input)
		s := NewSource(caller)
		op := budgetFor(t, l)
		r := parseWithProfile(context.Background(), s, op, gfmInProgressProfile)
		again := parseWithProfile(context.Background(), s, budgetFor(t, l), gfmInProgressProfile)
		if !reflect.DeepEqual(r, again) || s.content != input || string(caller) != input {
			t.Fatal("nondeterminism/input mutation")
		}
		if len(caller) > 0 {
			caller[0] ^= 255
			copy := s.Bytes()
			copy[0] ^= 255
		}
		if s.content != input {
			t.Fatal("mutable snapshot")
		}
		if r.Failure != nil {
			if r.Failure.Code != InvalidUTF8 || utf8.ValidString(input) {
				t.Fatalf("unexpected operational failure: %+v source=%q", r.Failure, input)
			}
			gfmSpan(t, s, r.Failure.Location, r.Failure.Location.Start(), r.Failure.Location.End())
			return
		}
		if r.Limit != nil {
			requireLimit(t, r, r.Limit.Name)
			gfmDenied(t, r.Document, op)
		} else {
			requireTableComplete(t, r, input)
		}
		gfmSemanticTree(t, r)
		if r.Status == Complete {
			// These are real completed same-operation consumers. Exhaustion while collecting
			// is an explicit typed limit, never a partial successful authority inventory.
			if _, err := r.Document.ProtectedNodes(context.Background(), op); err != nil {
				var limit *LimitExceeded
				if !errors.As(err, &limit) || limit.Observed <= limit.Allowed {
					t.Fatal("bounded protection", err)
				}
			}
		}
		// No parser is used to compute this layout's expected cell/marker boundaries.
		var payload strings.Builder
		for i := 0; i < min(len(input), 24); i++ {
			payload.WriteByte('a' + input[i]%26)
		}
		if payload.Len() == 0 {
			payload.WriteByte('a')
		}
		text := payload.String()
		generated := "| left | right |\n| - | - |\n| " + text + " | ~~" + text + "~~ | `opaque` |\n\n- [X]\t" + text + "\n"
		known := gfmParse(t, NewSource([]byte(generated)), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
		table := tableNodes(known.Document.root, Table)
		if len(table) != 1 {
			t.Fatal("known table missing")
		}
		rows := table[0].Children()
		body := rows[2]
		p := body.Properties().Row
		if p.RawCells != 3 || p.Padding != 0 || len(body.Children()) != 3 || tableText(body.Children()[0]) != text {
			t.Fatal("known excess source cell lost")
		}
		spans := p.Separators
		start := len("| left | right |\n| - | - |\n")
		expected := []int{start, start + len(text) + 3, start + 2*len(text) + 10, start + 2*len(text) + 21}
		if len(spans) != len(expected) {
			t.Fatal("known separator count")
		}
		for i, offset := range expected {
			gfmSpan(t, known.Source, spans[i], offset, offset+1)
		}
		strike := tableNodes(known.Document.root, Strike)
		if len(strike) != 1 || strike[0].Properties().Delimiter.Width != 2 || tableText(strike[0]) != text {
			t.Fatal("known strike semantics")
		}
		items := taskItems(known.Document.root)
		if len(items) != 1 || items[0].Properties().Task.CheckSpelling != "X" || tableRaw(t, known.Source, items[0].Properties().Task.Separator) != "\t" {
			t.Fatal("known task spelling")
		}
	})
}

func gfmSemanticTree(t *testing.T, r ParseResult) {
	t.Helper()
	s := r.Source
	span := func(p Span) {
		if p.Present() {
			if p.source != s || p.Start() < 0 || p.End() < p.Start() || p.End() > s.Len() {
				t.Fatal("source bounds/ownership", p)
			}
			if _, err := s.Text(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	parts := func(parts []Segment) {
		for _, p := range parts {
			span(p.Source)
			if p.Transformation == Exact && (!p.Source.Present() || p.Semantic.Present && p.Semantic.Value != tableRaw(t, s, p.Source)) {
				t.Fatal("exact source contribution", p)
			}
		}
	}
	Walk(r.Document.Root(), func(n *Node) bool {
		span(n.Span())
		parts(n.Content())
		for _, m := range n.Markers() {
			span(m.Source)
		}
		for _, child := range n.Children() {
			if !contains(n.Span(), child.Span()) {
				t.Fatal("child outside original owner")
			}
		}
		p := n.Properties()
		if p.Table != nil {
			if p.Table.Width < 1 || len(p.Table.Alignments) != p.Table.Width {
				t.Fatal("table width")
			}
			for _, row := range n.Children() {
				q := row.Properties().Row
				if q == nil || q.RawCells != len(row.Children()) || q.Padding != max(0, p.Table.Width-q.RawCells) {
					t.Fatal("raw cells versus semantic padding")
				}
			}
		}
		if p.Row != nil {
			for _, sep := range p.Row.Separators {
				span(sep)
				if tableRaw(t, s, sep) != "|" {
					t.Fatal("nonpipe separator")
				}
			}
		}
		if p.Cell != nil {
			for _, escape := range p.Cell.EscapedPipes {
				if tableRaw(t, s, escape) != "\\|" {
					t.Fatal("nonoriginal escaped pipe")
				}
			}
		}
		if p.Task != nil {
			q := p.Task
			span(q.Marker)
			span(q.Paragraph)
			span(q.Separator)
			if tableRaw(t, s, q.Check) != q.CheckSpelling || tableRaw(t, s, q.Opening) != "[" || tableRaw(t, s, q.Closing) != "]" || !taskToken(q.Separator) {
				t.Fatal("task original spelling")
			}
		}
		if n.Kind() == Strike {
			q := p.Delimiter
			if q == nil || q.Marker != '~' || q.Width < 1 || q.Width > 2 || len(n.Markers()) != 2 {
				t.Fatal("strike arity")
			}
			for _, m := range n.Markers() {
				if tableRaw(t, s, m.Source) != strings.Repeat("~", q.Width) {
					t.Fatal("strike original markers")
				}
			}
		}
		if p.Link != nil {
			q := p.Link
			parts(q.Label.Segments)
			parts(q.Destination.Segments)
			if q.ReferenceLabel != nil {
				parts(q.ReferenceLabel.Segments)
			}
			if q.Title != nil {
				parts(q.Title.Segments)
			}
			for _, d := range q.Destination.Segments {
				if !d.Source.Present() && d.Transformation != Synthetic {
					t.Fatal("destination invented source")
				}
				if q.Origin == "full" || q.Origin == "collapsed" || q.Origin == "shortcut" {
					if d.Source.Present() || d.Transformation != Synthetic {
						t.Fatal("reference use claimed definition bytes")
					}
				}
			}
		}
		if p.HTML != nil {
			for _, tag := range p.HTML.FilteredTags {
				span(tag.Affected)
				parts(tag.Prefix.Segments)
				if tableRaw(t, s, tag.Affected) != "<" || tag.Affected.End() != tag.Affected.Start()+1 {
					t.Fatal("filtered original angle")
				}
				if tag.Tag != nil {
					parts(tag.Tag.Segments)
				}
			}
		}
		return true
	})
}

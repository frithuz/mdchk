package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestGFMInspectionSourceFacts(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%x", ending), func(t *testing.T) {
			root, o := fixture(t)
			o.Capabilities.GFM = true
			text := strings.ReplaceAll("> - [X]\té ~~ok~~ www.a.b\n\n| a\\|b | c |\n| :--- | ---: |\n| one |\n| two | `code` | excess |\n\n<textarea>\nopaque\n</textarea>\n\n<script \n", "\n", ending)
			var definition strings.Builder
			definition.WriteString("version: 1\nrules:\n")
			for _, m := range engine.Catalog() {
				fmt.Fprintf(&definition, "  %s: {enabled: false}\n", m.ID)
			}
			write(t, filepath.Join(root, config.Filename), []byte(definition.String()))
			write(t, filepath.Join(root, "x.md"), []byte(text))
			s := acquire(t, "x.md", o)
			a := analyze(t, s, false)
			nodes := func(a Analysis) map[string][]Node {
				out := map[string][]Node{}
				var visit func(Node)
				visit = func(n Node) {
					out[n.Kind] = append(out[n.Kind], n)
					for _, c := range n.Children {
						visit(c)
					}
				}
				if a.Projection.AST.Data == nil {
					t.Fatal("AST unavailable", a.Result)
				}
				visit(a.Projection.AST.Data.Root)
				return out
			}
			assert := func(a Analysis) {
				t.Helper()
				n := nodes(a)
				if !a.Result.Complete || a.Projection.AST.Data.Profile != "gfm" || *a.Projection.Flavor.Data != "gfm" {
					t.Fatal("selection/profile", a.Projection)
				}
				table := n["table"][0]
				if table.Properties["width"] != 2 || !reflect.DeepEqual(table.Properties["alignments"], []string{"left", "right"}) || len(table.Children) != 4 {
					t.Fatal(table)
				}
				rows := table.Children
				if rows[0].Properties["role"] != "header" || rows[1].Properties["role"] != "delimiter" || rows[2].Properties["padding"] != 1 || rows[2].Properties["raw_cells"] != 1 || rows[3].Properties["raw_cells"] != 3 || len(rows[3].Children) != 3 {
					t.Fatal(rows)
				}
				for _, row := range rows {
					r := row.ByteRange
					if !strings.HasSuffix(text[r.Start:r.End], ending) {
						t.Fatal("row lost ending")
					}
					if _, ok := row.Properties["separators"].([]result.ByteRange); !ok {
						t.Fatal("separator type")
					}
				}
				first := rows[0].Children[0]
				esc := strings.Index(text, "\\|")
				if !reflect.DeepEqual(first.Properties["escaped_pipes"], []result.ByteRange{{Start: esc, End: esc + 2}}) || strings.TrimSpace(text[first.ByteRange.Start:first.ByteRange.End]) != "a\\|b" {
					t.Fatal(first)
				}
				item := n["list-item"][0]
				task := item.Properties["task"].(Task)
				if !task.Checked || task.CheckSpelling != "X" || task.Marker != (result.ByteRange{Start: 4, End: 7}) || task.Opening != (result.ByteRange{Start: 4, End: 5}) || task.Check != (result.ByteRange{Start: 5, End: 6}) || task.Closing != (result.ByteRange{Start: 6, End: 7}) || task.Separator != (result.ByteRange{Start: 7, End: 8}) || !reflect.DeepEqual(task.Segments, []result.ByteRange{{Start: 4, End: 7}}) || !reflect.DeepEqual(item.Children[0].Properties["task"], task) {
					t.Fatal(task)
				}
				strike := n["strike"][0]
				if strike.Properties["marker"] != "~" || strike.Properties["width"] != 2 || text[strike.ByteRange.Start:strike.ByteRange.End] != "~~ok~~" {
					t.Fatal(strike)
				}
				link := n["link"][0]
				destination := link.Properties["destination"].(Value)
				if link.Properties["origin"] != "bare-gfm" || *destination.Semantic != "http://www.a.b" || destination.Segments[0].Transformation != "synthetic" || destination.Segments[0].ByteRange != nil || *destination.Segments[0].Semantic != "http://" {
					t.Fatal(link)
				}
				tags := n["html-block"][0].Properties["filtered_tags"].([]FilteredTag)
				if len(tags) != 2 || tags[0].Name != "textarea" || tags[0].Closing || !tags[1].Closing || tags[0].Tag == nil || text[tags[0].Affected.Start:tags[0].Affected.End] != "<" || *tags[0].Prefix.Semantic != "<textarea" {
					t.Fatal(tags)
				}
				last := n["html-block"][1].Properties["filtered_tags"].([]FilteredTag)
				if len(last) != 1 || last[0].Name != "script" || last[0].Tag != nil {
					t.Fatal(last)
				}
				validateProduced(t, a)
			}
			assert(a)
			n := nodes(a)
			n["table"][0].Properties["alignments"].([]string)[0] = "mutated"
			task := n["list-item"][0].Properties["task"].(Task)
			task.Segments[0] = result.ByteRange{}
			tags := n["html-block"][0].Properties["filtered_tags"].([]FilteredTag)
			tags[0].Prefix.Segments[0].Transformation = "mutated"
			tags[0].Tag.Segments[0].Transformation = "mutated"
			n["table-row"][0].Properties["separators"].([]result.ByteRange)[0] = result.ByteRange{}
			// Re-filtering the SAME analysis must use private retained authority.
			whole, err := Filter(context.Background(), s, a, "")
			if err != nil {
				t.Fatal(err)
			}
			assert(whole)
			assert(analyze(t, s, false))
			selected, err := Filter(context.Background(), s, a, "6")
			if err != nil {
				t.Fatal(err)
			}
			sn := nodes(selected)
			if len(sn["table-row"]) != 1 || len(sn["table-cell"]) != 3 || !sn["table"][0].ContextOnly || sn["table-row"][0].Properties["raw_cells"] != 3 || sn["table-row"][0].ID != n["table-row"][3].ID {
				t.Fatal("selection lost original source facts", selected.Projection)
			}
			validateProduced(t, selected)
			taskView, err := Filter(context.Background(), s, a, "1")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(nodes(taskView)["list-item"][0].Properties["task"], nodes(whole)["list-item"][0].Properties["task"]) {
				t.Fatal("task filter lost tokens")
			}
			validateProduced(t, taskView)
		})
	}
}

// This deliberately narrow callback tests the shared consumer boundary. It does
// not implement any production MD001 or GFM-specific rule behavior.
func TestGFMInspectionDirectiveConsumer(t *testing.T) {
	root, o := fixture(t)
	o.Capabilities.GFM = true
	text := "| a | b |\n| - | - |\n| x | y | <!-- mdchk:disable MD001 --> |\n\nactive\n<!-- mdchk:enable MD001 -->\nvisible\n\n<textarea>\n<!-- mdchk:disable MD001 -->\n</textarea>\n\n`<!-- mdchk:disable MD001 -->`\n"
	calls := 0
	r, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if c.Context().Flavor != "gfm" {
			t.Fatal("wrong parse selection")
		}
		out := engine.RuleOutcome{}
		for _, token := range []string{"active", "visible"} {
			at := strings.Index(text, token)
			sp, _ := c.Source().Span(at, at+len(token))
			out.Findings = append(out.Findings, engine.Finding{Span: sp, Message: token})
		}
		return out
	}}})
	if err != nil {
		t.Fatal(err)
	}
	o.Registry = r
	var definition strings.Builder
	definition.WriteString("version: 1\nrules:\n")
	for _, m := range engine.Catalog() {
		fmt.Fprintf(&definition, "  %s: {enabled: %t}\n", m.ID, m.ID == "MD001")
	}
	write(t, filepath.Join(root, config.Filename), []byte(definition.String()))
	write(t, filepath.Join(root, "x.md"), []byte(text))
	s := acquire(t, "x.md", o)
	a := analyze(t, s, false)
	if calls != 1 || !a.Result.Complete || len(a.Result.Diagnostics) != 1 || a.Result.Diagnostics[0].Message != "visible" || len(a.Result.SuppressedDiagnostics) != 1 || len(*a.Projection.Directives.Data) != 2 {
		t.Fatal("real consumer result", calls, a.Result)
	}
	var hidden result.SuppressedDiagnostic
	if err := json.Unmarshal(a.Result.SuppressedDiagnostics[0], &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Diagnostic.Message != "active" || hidden.SuppressedBy.DirectiveID != "directive-1" {
		t.Fatal(hidden)
	}
	input, err := s.plan.NewSourceInput(s.file.Path, s.source)
	if err != nil {
		t.Fatal(err)
	}
	lint, err := s.plan.Lint(context.Background(), []engine.Input{input}, engine.RunOptions{Budget: s.budget})
	if err != nil || !reflect.DeepEqual(lint.Diagnostics, a.Result.Diagnostics) {
		t.Fatal("lint/inspect diagnostic mismatch", err)
	}
	filtered, err := Filter(context.Background(), s, a, "5")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(filtered.Result.Diagnostics) != 0 || len(filtered.Result.SuppressedDiagnostics) != 1 || !(*filtered.Projection.Directives.Data)[0].ContextOnly || len(*filtered.Projection.Directives.Data) != 2 {
		t.Fatal("filter reran or lost directive closure", filtered.Result)
	}
	var selected result.SuppressedDiagnostic
	_ = json.Unmarshal(filtered.Result.SuppressedDiagnostics[0], &selected)
	if selected.SuppressedBy.DirectiveID != hidden.SuppressedBy.DirectiveID || selected.Diagnostic.ByteRange != hidden.Diagnostic.ByteRange {
		t.Fatal("selection changed suppression authority")
	}
	validateProduced(t, a)
	validateProduced(t, filtered)
}

package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// These isolate only the private lexical payload helper. Context-free constructed
// owners cannot grant Document.ShellPrompts production authority (tested separately).
// Literal original ranges and charge sums are frozen from the independent ledger
// plus arithmetic erratum; no parse counters or setup subtraction are used.
func TestShellPromptPayloadLexicalHelperStaticReservations(t *testing.T) {
	type contribution struct {
		start, end int
		text       string
		transform  Transformation
	}
	for _, row := range []struct {
		name, text  string
		closing     int
		parts       []contribution
		work, nodes int64
		findings    int
	}{
		{"empty-closed", "```sh\n```\n", 6, nil, 129, 0, 0},
		{"opener-only", "```sh", -1, nil, 90, 0, 0},
		{"one-LF", "```sh\n$ pwd\n```\n", 12, []contribution{{6, 11, "$ pwd", Exact}, {11, 12, "\n", Exact}}, 251, 2, 1},
		{"one-CRLF", "```sh\r\n$ pwd\r\n```\r\n", 14, []contribution{{7, 12, "$ pwd", Exact}, {12, 14, "\n", Normalized}}, 252, 2, 1},
		{"unclosed-EOF", "```sh\n$ pwd", -1, []contribution{{6, 11, "$ pwd", Exact}, {11, 11, "\n", Synthetic}}, 212, 2, 1},
		{"unclosed-LF", "```sh\n$ pwd\n", -1, []contribution{{6, 11, "$ pwd", Exact}, {11, 12, "\n", Exact}}, 213, 2, 1},
		{"nonprompt", "```sh\npwd\n```\n", 10, []contribution{{6, 9, "pwd", Exact}, {9, 10, "\n", Exact}}, 235, 0, 0},
		{"ascii-blank", "```sh\n \t\n```\n", 9, []contribution{{6, 8, " \t", Exact}, {8, 9, "\n", Exact}}, 229, 0, 0},
		{"empty-line", "```sh\n\n```\n", 7, []contribution{{6, 7, "\n", Exact}}, 172, 0, 0},
		{"mixed", "```sh\n$ pwd\nout\n```\n", 16, []contribution{{6, 11, "$ pwd", Exact}, {11, 12, "\n", Exact}, {12, 15, "out", Exact}, {15, 16, "\n", Exact}}, 358, 2, 0},
		{"two-prompts", "```sh\n$ pwd\n$ whoami\n```\n", 21, []contribution{{6, 11, "$ pwd", Exact}, {11, 12, "\n", Exact}, {12, 20, "$ whoami", Exact}, {20, 21, "\n", Exact}}, 394, 5, 2},
	} {
		for _, nodes := range []bool{false, true} {
			if nodes && row.nodes == 0 {
				continue
			}
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/nodes-%v/delta-%d", row.name, nodes, delta), func(t *testing.T) {
					source := NewSource([]byte(row.text))
					span, _ := source.Span(0, len(row.text))
					opening, _ := source.Span(0, 3)
					markers := []Marker{{Role: "opening", Source: opening}}
					if row.closing >= 0 {
						closing, _ := source.Span(row.closing, row.closing+3)
						markers = append(markers, Marker{Role: "closing", Source: closing})
					}
					var parts []Segment
					for _, p := range row.parts {
						s, e := source.Span(p.start, p.end)
						if e != nil {
							t.Fatal(e)
						}
						parts = append(parts, Segment{Source: s, Transformation: p.transform, Semantic: Some(p.text)})
					}
					n, e := NewNode(NodeSpec{Kind: CodeBlock, Envelope: span, Markers: markers, Content: parts, Properties: Properties{Code: &CodeProperties{Style: "fenced", Fence: Some(byte('`')), Width: Some(3)}}})
					if e != nil {
						t.Fatal(e)
					}
					index := NewLineIndex(source)
					limits := DefaultLimits()
					if nodes {
						limits.TotalNodes = row.nodes + delta
						limits.DocumentNodes = limits.TotalNodes
					} else {
						limits.WorkUnits = row.work + delta
					}
					budget, e := NewOperationBudget(limits)
					if e != nil {
						t.Fatal(e)
					}
					out, known, e := shellPromptPayload(context.Background(), budget, index, n, nil)
					if delta == 0 {
						if e != nil || !known || len(out) != row.findings {
							t.Fatal(out, known, e)
						}
					} else {
						var stop *LimitExceeded
						if !errors.As(e, &stop) || out != nil || known {
							t.Fatal(out, known, e)
						}
						out, known, e = shellPromptPayload(context.Background(), budget, index, n, nil)
						if !errors.As(e, &stop) || out != nil || known {
							t.Fatal("sticky retry", out, known, e)
						}
					}
				})
			}
		}
	}
}

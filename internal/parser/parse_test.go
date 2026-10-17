package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestParseStageOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		status  parser.Status
		ast     parser.Availability
		reason  parser.Reason
		failure parser.FailureCode
	}{
		{name: "nil", status: parser.Complete, ast: parser.Available},
		{name: "empty", input: []byte{}, status: parser.Complete, ast: parser.Available},
		{name: "ordinary text", input: []byte("hello\n"), status: parser.Incomplete, ast: parser.Partial, reason: parser.SyntaxUnimplemented},
		{name: "whitespace", input: []byte(" \t\r\n"), status: parser.Complete, ast: parser.Available},
		{name: "thematic only", input: []byte("***\r\n___"), status: parser.Complete, ast: parser.Available},
		{name: "unsupported core syntax", input: []byte("**strong**"), status: parser.Incomplete, ast: parser.Partial, reason: parser.SyntaxUnimplemented},
		{name: "extension looking", input: []byte("~~strike~~"), status: parser.Incomplete, ast: parser.Partial, reason: parser.SyntaxUnimplemented},
		{name: "frontmatter looking", input: []byte("---\nx: y\n---\n"), status: parser.Incomplete, ast: parser.Partial, reason: parser.SyntaxUnimplemented},
		{name: "valid replacement and NUL", input: []byte("\ufffd\x00"), status: parser.Incomplete, ast: parser.Partial, reason: parser.SyntaxUnimplemented},
		{name: "invalid leading byte", input: []byte{0xff}, status: parser.Failed, ast: parser.Unavailable, failure: parser.InvalidUTF8},
		{name: "invalid after thematic line", input: []byte("***\n\xff"), status: parser.Failed, ast: parser.Unavailable, failure: parser.InvalidUTF8},
		{name: "truncated multibyte", input: []byte{'x', 0xe2, 0x82}, status: parser.Failed, ast: parser.Unavailable, failure: parser.InvalidUTF8},
		{name: "overlong encoding", input: []byte{0xc0, 0x80}, status: parser.Failed, ast: parser.Unavailable, failure: parser.InvalidUTF8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := parser.NewSource(tt.input)
			result := parser.Parse(context.Background(), s)
			if result.Status != tt.status || result.AST != tt.ast || string(result.Source.Bytes()) != string(tt.input) {
				t.Fatalf("wrong outcome: %+v", result)
			}
			if result.Interruption != "" {
				t.Fatal("unrequested interruption")
			}
			if tt.failure != "" {
				if result.Failure == nil || result.Failure.Code != tt.failure || result.Document != nil || len(result.Uncertainty) != 0 || result.Phases != (parser.PhaseProgress{Blocks: parser.NotStarted, Inlines: parser.NotStarted}) {
					t.Fatalf("encoding gate returned normal syntax: %+v", result)
				}
				var typed *parser.Failure
				if !errors.As(result.Failure, &typed) || typed.Code != parser.InvalidUTF8 {
					t.Fatal("failure not discriminable by type/code")
				}
				return
			}
			if result.Failure != nil || result.Document == nil {
				t.Fatalf("syntax availability mistaken for operational failure: %+v", result)
			}
			root := result.Document.Root()
			if root.Kind() != parser.DocumentKind || root.Span().Start() != 0 || root.Span().End() != len(tt.input) {
				t.Fatal("incorrect document root")
			}
			if _, err := result.Source.Text(root.Span()); err != nil {
				t.Fatal("root belongs to a different snapshot")
			}
			refs := result.Document.References()
			if tt.reason == "" {
				if len(result.Uncertainty) != 0 || result.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) || refs.Availability != parser.Available || refs.LookupAvailability != parser.Available {
					t.Fatalf("supported parse is not complete: %+v", result)
				}
			} else {
				if len(result.Uncertainty) != 1 || result.Uncertainty[0].Reason != tt.reason || result.Uncertainty[0].Region.Start() != 0 || result.Uncertainty[0].Region.End() != len(tt.input) || result.Phases != (parser.PhaseProgress{Blocks: parser.Pending, Inlines: parser.NotStarted}) || refs.Availability != parser.Unavailable || refs.LookupAvailability != parser.Unavailable {
					t.Fatalf("unsupported input advertised reliable syntax: %+v", result)
				}
			}
			again := parser.Parse(context.Background(), s)
			if !reflect.DeepEqual(result, again) {
				t.Fatal("same snapshot produced nondeterministic result")
			}
		})
	}
}

func TestParseInterruptionIsNotOperationalFailure(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, tt := range []struct {
		ctx    context.Context
		reason parser.Reason
	}{{cancelled, parser.Cancelled}, {deadline, parser.DeadlineExceeded}} {
		for _, input := range []string{"", "abc", " \t\r\n", "***\r\n"} {
			r := parser.Parse(tt.ctx, parser.NewSource([]byte(input)))
			if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Failure != nil || r.Interruption != tt.reason || r.Document == nil || r.Phases.Blocks != parser.Interrupted || r.Phases.Inlines != parser.NotStarted {
				t.Fatalf("wrong interruption outcome: %+v", r)
			}
			if len(r.Uncertainty) != 1 || !r.Uncertainty[0].Region.Present() || r.Uncertainty[0].Region.End() != len(input) || r.Uncertainty[0].Reason != tt.reason || r.Document.References().LookupAvailability != parser.Unavailable {
				t.Fatalf("interruption missing affected range/availability: %+v", r)
			}
		}
	}
	// Accepted operational-error precedence: encoding failure is not hidden by
	// an already cancelled context, and malformed bytes remain retained.
	r := parser.Parse(cancelled, parser.NewSource([]byte{0xff}))
	if r.Status != parser.Failed || r.Interruption != "" || r.Failure == nil || r.Failure.Code != parser.InvalidUTF8 || r.Source.Bytes()[0] != 0xff {
		t.Fatal("cancellation hid encoding failure")
	}
}

func TestSyntheticOperationalFailureModel(t *testing.T) {
	// This constructs a result contract. Parse has no injected internal failure.
	r := parser.ParseResult{Source: parser.NewSource([]byte("retained")), Status: parser.Failed, AST: parser.Unavailable, Phases: parser.PhaseProgress{Blocks: parser.NotStarted, Inlines: parser.NotStarted}, Failure: &parser.Failure{Code: parser.InternalFailure, Message: "synthetic example"}}
	var failure *parser.Failure
	if !errors.As(r.Failure, &failure) || failure.Code != parser.InternalFailure || failure.Location.Present() || r.Document != nil || r.Interruption != "" || string(r.Source.Bytes()) != "retained" {
		t.Fatal("synthetic failure contract lost failure/availability/source distinctions")
	}
}

func TestInvalidUTF8FailureLocation(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		offset int
	}{
		{name: "byte zero", input: []byte{0xff}, offset: 0},
		{name: "multibyte CRLF prefix", input: []byte("é\r\n\xff"), offset: 4},
		{name: "truncated sequence", input: []byte{'x', 0xe2, 0x82}, offset: 1},
		{name: "overlong sequence", input: []byte{'x', 0xc0, 0x80}, offset: 1},
		{name: "invalid continuation", input: []byte{'x', 0xe2, '(', 0xa1, 0xff}, offset: 1},
		{name: "surrogate", input: []byte{'x', 0xed, 0xa0, 0x80}, offset: 1},
		{name: "above scalar limit", input: []byte{'x', 0xf4, 0x90, 0x80, 0x80}, offset: 1},
		{name: "valid replacement and NUL prefix", input: []byte("\ufffd\x00\xff"), offset: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := parser.NewSource(tt.input)
			result := parser.Parse(context.Background(), source)
			if result.Status != parser.Failed || result.AST != parser.Unavailable || result.Document != nil || result.Failure == nil || result.Failure.Code != parser.InvalidUTF8 {
				t.Fatalf("invalid encoding did not fail before syntax: %+v", result)
			}
			location := result.Failure.Location
			if !location.Present() || location.Start() != tt.offset || location.End() != tt.offset {
				t.Fatalf("failure location = [%d,%d), present=%v; want present point at %d", location.Start(), location.End(), location.Present(), tt.offset)
			}
			if text, err := result.Source.Text(location); err != nil || text != "" {
				t.Fatalf("failure point lost snapshot ownership: %q, %v", text, err)
			}
			if _, err := parser.NewSource(tt.input).Text(location); !errors.Is(err, parser.ErrRange) {
				t.Fatal("failure point accepted by an independently acquired snapshot")
			}
			if string(result.Source.Bytes()) != string(tt.input) || result.Interruption != "" || len(result.Uncertainty) != 0 || result.Phases != (parser.PhaseProgress{Blocks: parser.NotStarted, Inlines: parser.NotStarted}) {
				t.Fatal("locating the failure changed bytes or performed normal parsing")
			}
		})
	}
}

func TestCoreCorpusAccounting(t *testing.T) {
	const base = "testdata/commonmark/0.31.2/"
	b, err := os.ReadFile(base + "examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example  int    `json:"example"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(b, &examples); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(base + "foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		SchemaVersion int   `json:"schema_version"`
		Supported     []int `json:"supported_html_examples"`
		Pending       []struct {
			Reason   string `json:"reason"`
			Owners   []int  `json:"owners"`
			Examples []int  `json:"examples"`
		} `json:"pending"`
	}
	if err := json.Unmarshal(b, &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != 1 || len(examples) != 652 {
		t.Fatal("invalid inventory version or changed corpus")
	}
	seen := make(map[int]bool)
	supported := make(map[int]bool)
	for _, id := range inventory.Supported {
		if id < 1 || id > 652 || seen[id] {
			t.Fatalf("invalid/duplicate supported ID %d", id)
		}
		seen[id], supported[id] = true, true
	}
	for _, group := range inventory.Pending {
		if group.Reason != "syntax-unimplemented" || len(group.Owners) == 0 {
			t.Fatal("pending cases lack reason/owners")
		}
		for _, id := range group.Examples {
			if id < 1 || id > 652 || seen[id] {
				t.Fatalf("invalid/duplicate pending ID %d", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 652 {
		t.Fatalf("pending inventory has %d IDs, want 652", len(seen))
	}
	for i, example := range examples {
		if example.Example != i+1 || !seen[example.Example] {
			t.Fatalf("unaccounted upstream example %d", example.Example)
		}
		// Parser outcomes agree with complete unique ledger accounting. Actual HTML
		// comparison/ratcheting belongs to the separate conformance runner.
		r := parser.Parse(context.Background(), parser.NewSource([]byte(example.Markdown)))
		if supported[example.Example] {
			if r.Status != parser.Complete || r.AST != parser.Available || r.Failure != nil || len(r.Uncertainty) != 0 {
				t.Fatalf("supported example %d lost parsing: %+v", example.Example, r)
			}
		} else if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Failure != nil || len(r.Uncertainty) == 0 || r.Uncertainty[0].Reason != parser.SyntaxUnimplemented {
			t.Fatalf("pending example %d has false syntax coverage: %+v", example.Example, r)
		}
	}
}

package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestAnchorHeadingFactsRequireActualSourceOperation(t *testing.T) {
	source := parser.NewSource([]byte("# Root #\r\n\r\n> ## ![Alt](image) *text* #\r\n\r\n- ### Item #\r\n"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	facts, err := parsed.Document.Headings(context.Background(), budget)
	if err != nil || len(facts) != 3 || facts[0].Visible != "Root" || facts[1].Visible != "Alt text" || facts[2].Visible != "Item" {
		t.Fatal(facts, err)
	}
	if len(facts[1].Containers) != 1 || facts[1].Containers[0].Kind != parser.BlockQuote || len(facts[2].Containers) != 2 || facts[2].Containers[0].Kind != parser.List || facts[2].Containers[1].Kind != parser.ListItem {
		t.Fatal("container provenance lost", facts)
	}
	for _, fact := range facts {
		if _, err := source.Text(fact.Envelope); err != nil {
			t.Fatal("heading source foreign", err)
		}
		for _, ancestor := range fact.Containers {
			if _, err := source.Text(ancestor.Envelope); err != nil {
				t.Fatal("ancestor source foreign", err)
			}
		}
	}
	facts[1].Containers[0].Kind = parser.CodeBlock
	if again, err := parsed.Document.Headings(context.Background(), budget); err != nil || again[1].Containers[0].Kind != parser.BlockQuote {
		t.Fatal("caller mutated parser facts", again, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := parsed.Document.Headings(context.Background(), foreign); err == nil {
		t.Fatal("foreign budget manufactured verified headings")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := constructed.Headings(context.Background(), budget); err == nil {
		t.Fatal("constructed syntax manufactured recognition")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := parsed.Document.Headings(cancelled, budget); !errors.Is(err, context.Canceled) || partial != nil {
		t.Fatal("cancelled collection published facts", partial, err)
	}
	if err := budget.ChargeNodes(budget.Limits().TotalNodes); err == nil {
		t.Fatal("actual node quota not exhausted")
	}
	var limit *parser.LimitExceeded
	if partial, err := parsed.Document.Headings(context.Background(), budget); !errors.As(err, &limit) || partial != nil {
		t.Fatal("quota became empty successful inventory", partial, err)
	}
}

func TestAnchorSourceTokensAreIdentityNotAdmission(t *testing.T) {
	zero := parser.Source{}
	empty := parser.NewSource(nil)
	first := parser.NewSource([]byte("same bytes"))
	copy := first
	second := parser.NewSource(first.Bytes())
	if zero.Token() != (parser.SourceToken{}) || empty.Token() == zero.Token() || first.Token() != copy.Token() || first.Token() == second.Token() {
		t.Fatal("token confused zero/empty/copy/equal-text snapshots")
	}
	// A token describes ownership only; it cannot establish a completed parse.
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	span, _ := first.Span(0, first.Len())
	root, _ := parser.NewNode(parser.NodeSpec{Kind: parser.DocumentKind, Envelope: span})
	document, err := parser.NewDocument(root, parser.ReferenceCollection{Availability: parser.Available, LookupAvailability: parser.Available})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.Headings(context.Background(), budget); err == nil {
		t.Fatal("identity token became syntax authority")
	}
}

func TestHTMLIDsFilteredRawScriptNeverClaimsEmptyAuthority(t *testing.T) {
	text := "<script id=s><span id=x></span></script>\n"
	for _, gfm := range []bool{false, true} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		source := parser.NewSource([]byte(text))
		parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{GFM: gfm})
		if parsed.Status != parser.Complete {
			t.Fatal(parsed)
		}
		ids, err := parsed.Document.HTMLIDs(context.Background(), budget)
		if err != nil {
			t.Fatal(err)
		}
		if gfm {
			// GFM only escapes disallowed prefixes; it is not a sanitizer and
			// cannot justify inheriting CommonMark's raw-text negative inventory.
			if ids.Availability != parser.Unavailable || len(ids.Occurrences) != 0 || len(ids.Uncertainty) == 0 {
				t.Fatal("filtered raw text became complete empty targets", ids)
			}
		} else if ids.Availability != parser.Available || len(ids.Occurrences) != 1 || ids.Occurrences[0].Value.Semantic != parser.Some("s") {
			t.Fatal("CommonMark opening/raw payload ownership lost", ids)
		}
	}
}

func TestHeadingClosingFactsUseOriginalMarkers(t *testing.T) {
	text := "#\n# #\n## text #### \t\n# text###\n# text \\###\n\nSetext\n===\n"
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(text))
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	facts, err := parsed.Document.Headings(context.Background(), budget)
	if err != nil || len(facts) != 6 {
		t.Fatal(facts, err)
	}
	for i, closed := range []bool{false, true, true, false, false, false} {
		style := "atx"
		if i == 5 {
			style = "setext"
		}
		if facts[i].Closed != closed || facts[i].Style != style {
			t.Fatal(i, facts[i])
		}
	}
	if string(source.Bytes()) != text {
		t.Fatal("projection changed source")
	}
}

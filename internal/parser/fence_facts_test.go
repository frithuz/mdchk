package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestFencedFactsCompletedWitnessAndMappings(t *testing.T) {
	raw := "> - ~~~{r label='é 図'}\r\n>   #| label: body\r\n>   print(1)\r\n>   ~~~\r\n\n````markdown\n```{python}\n```\n````\n\n```{julia}\nlast"
	source := parser.NewSource([]byte(raw))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	facts, err := parsed.Document.FencedBlocks(context.Background(), budget)
	if err != nil || len(facts) != 3 {
		t.Fatal(facts, err)
	}
	first := facts[0]
	if first.Fence != '~' || first.Width != 3 || first.RawInfo.Start() != 7 || first.RawInfo.End() != 25 || first.Content.Semantic.Value != "#| label: body\nprint(1)\n" || len(first.Containers) != 3 {
		t.Fatal(first)
	}
	if first.Containers[0].Kind != parser.BlockQuote || first.Containers[1].Kind != parser.List || first.Containers[2].Kind != parser.ListItem {
		t.Fatal(first.Containers)
	}
	if got, _ := source.Text(first.RawInfo); got != "{r label='é 図'}" {
		t.Fatal(got)
	}
	last := facts[2]
	if len(last.Markers) != 1 || last.Content.Semantic.Value != "last\n" || last.Content.Segments[len(last.Content.Segments)-1].Transformation != parser.Synthetic {
		t.Fatal("EOF fence lost", last)
	}
	facts[0].Info.Segments[0].Semantic = parser.Some("changed")
	facts[0].Content.Segments[0].Semantic = parser.Some("changed")
	facts[0].Markers[0].Role = "changed"
	facts[0].Containers[0].Kind = parser.Paragraph
	again, err := parsed.Document.FencedBlocks(context.Background(), budget)
	if err != nil || again[0].Info.Semantic.Value != "{r label='é 図'}" || again[0].Content.Semantic.Value != "#| label: body\nprint(1)\n" || again[0].Markers[0].Role != "opening" || again[0].Containers[0].Kind != parser.BlockQuote {
		t.Fatal("projection mutation escaped", again, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := parsed.Document.FencedBlocks(context.Background(), foreign); err == nil {
		t.Fatal("foreign budget gained parse proof")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := constructed.FencedBlocks(context.Background(), budget); err == nil {
		t.Fatal("constructed AST gained parse proof")
	}
	span, _ := source.Span(0, 1)
	if span.SourceToken() != source.Token() || (parser.Span{}).SourceToken() != (parser.SourceToken{}) {
		t.Fatal("span owner identity")
	}
}

func TestFencedFactsStopNeverPublishesCompletePartialCollection(t *testing.T) {
	source := parser.NewSource([]byte("```{r}\nvalue\n```\n"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if facts, err := parsed.Document.FencedBlocks(ctx, budget); !errors.Is(err, context.Canceled) || len(facts) != 0 {
		t.Fatal(facts, err)
	}
	if err := budget.ChargeWork(budget.Limits().WorkUnits + 1); err == nil {
		t.Fatal("quota not exhausted")
	}
	var limit *parser.LimitExceeded
	if facts, err := parsed.Document.FencedBlocks(context.Background(), budget); !errors.As(err, &limit) || len(facts) != 0 {
		t.Fatal(facts, err)
	}
	limited, _ := parser.NewOperationBudget(parser.DefaultLimits())
	incomplete := parser.ParseWithBudget(ctx, source, limited)
	incomplete.Status = parser.Complete
	incomplete.AST = parser.Available
	if facts, err := incomplete.Document.FencedBlocks(context.Background(), limited); err == nil || len(facts) != 0 {
		t.Fatal("caller result fields minted witness", facts, err)
	}
}

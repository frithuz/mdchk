package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingSeparatorOriginalFacts(t *testing.T) {
	const text = "> - ##   雪\r\n\n#  <!-- x -->\n#   ###\n\nSetext\n===\n#  \x00"
	source := parser.NewSource([]byte(text))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	facts, err := parsed.Document.Headings(context.Background(), budget)
	if err != nil || len(facts) != 5 {
		t.Fatal(facts, err)
	}
	for i, want := range []struct {
		present, content bool
		start, end       int
		visible          string
	}{
		{true, true, 6, 9, "雪"}, {true, true, 16, 18, ""}, {false, false, 0, 0, ""}, {false, true, 0, 0, "Setext"}, {true, true, 50, 52, "�"},
	} {
		h := facts[i]
		if h.HasRawContent != want.content || h.OpeningSeparator.Present() != want.present || h.Visible != want.visible {
			t.Fatal(i, h)
		}
		if want.present {
			raw, err := source.Text(h.OpeningSeparator)
			if err != nil || h.OpeningSeparator.Start() != want.start || h.OpeningSeparator.End() != want.end || raw != text[want.start:want.end] || h.OpeningSeparator.SourceToken() != source.Token() {
				t.Fatal(i, h, raw, err)
			}
		}
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if partial, err := parsed.Document.Headings(context.Background(), foreign); err == nil || partial != nil {
		t.Fatal(partial, err)
	}
	if string(source.Bytes()) != text {
		t.Fatal("source mutated")
	}
}

type headingStopContext struct {
	context.Context
	calls, stop int
}

func (c *headingStopContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestHeadingSeparatorProjectionWithholdsInterruptedInventory(t *testing.T) {
	source := parser.NewSource([]byte("#  <!-- raw -->\n#  \x00"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	counter := &headingStopContext{Context: context.Background()}
	facts, err := parsed.Document.Headings(counter, budget)
	if err != nil || len(facts) != 2 {
		t.Fatal(facts, err)
	}
	// Stop during inspection and at the last prepublication check, after a first
	// record could already have been constructed. Neither is a complete inventory.
	for _, stop := range []int{counter.calls / 2, counter.calls} {
		partial, err := parsed.Document.Headings(&headingStopContext{Context: context.Background(), stop: stop}, budget)
		if !errors.Is(err, context.Canceled) || partial != nil {
			t.Fatal(partial, err, stop)
		}
	}
}

func TestHeadingClosedGapsOriginalFacts(t *testing.T) {
	const text = "#  a\x00b  #\n#   #\n#\t#\n# open\n"
	source := parser.NewSource([]byte(text))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	facts, err := parsed.Document.Headings(context.Background(), budget)
	if err != nil || len(facts) != 4 {
		t.Fatal(facts, err)
	}
	for i, want := range []struct {
		opening, closing string
		start, end       int
		raw              bool
	}{
		{"  ", "  ", 1, 3, true}, {"   ", "", 11, 14, false}, {"\t", "", 17, 18, false},
	} {
		h := facts[i]
		a, ae := source.Text(h.ClosedOpeningGap)
		if ae != nil || a != want.opening || h.ClosedOpeningGap.Start() != want.start || h.ClosedOpeningGap.End() != want.end || h.ClosedOpeningGap.SourceToken() != source.Token() || h.HasRawContent != want.raw {
			t.Fatal(i, h, a, ae)
		}
		if want.raw {
			b, be := source.Text(h.ClosedClosingGap)
			if be != nil || b != want.closing || h.ClosedClosingGap.Start() != 6 || h.ClosedClosingGap.End() != 8 {
				t.Fatal(h, b, be)
			}
		} else if h.OpeningSeparator.Present() || h.ClosedClosingGap.Present() {
			t.Fatal("empty gap duplicated or prior contract changed", h)
		}
	}
	if facts[3].ClosedOpeningGap.Present() || facts[3].ClosedClosingGap.Present() {
		t.Fatal("open heading projected closed gaps")
	}
	// Original token ownership remains enforced at source access.
	foreign := parser.NewSource([]byte(text))
	if _, err := foreign.Text(facts[0].ClosedClosingGap); err == nil {
		t.Fatal("foreign source accepted")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := constructed.Headings(context.Background(), budget); err == nil || got != nil {
		t.Fatal("constructed document granted gaps", got, err)
	}
	foreignBudget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if got, err := parsed.Document.Headings(context.Background(), foreignBudget); err == nil || got != nil {
		t.Fatal("foreign operation granted gaps", got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	unfinished := parser.ParseWithBudget(cancelled, source, foreignBudget)
	if unfinished.Document != nil {
		if got, err := unfinished.Document.Headings(context.Background(), foreignBudget); err == nil || got != nil {
			t.Fatal("unfinished parse granted gaps", got, err)
		}
	}

	counter := &headingStopContext{Context: context.Background()}
	if _, err := parsed.Document.Headings(counter, budget); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{counter.calls / 2, counter.calls} {
		got, err := parsed.Document.Headings(&headingStopContext{Context: context.Background(), stop: stop}, budget)
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal("interrupted inventory published", got, err)
		}
	}
}

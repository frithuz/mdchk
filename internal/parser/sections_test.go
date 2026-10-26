package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func sectionParse(t *testing.T, text string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	source := NewSource([]byte(text))
	budget, _ := NewOperationBudget(DefaultLimits())
	parsed := ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != Complete {
		t.Fatal(parsed)
	}
	return parsed.Document, budget, NewLineIndex(source)
}

func TestSectionsReserveOutputBeforePublication(t *testing.T) {
	d, b, index := sectionParse(t, "# A")
	// One pre-reserved traversal slot, one actual raw label and one output record.
	b.limits.TotalNodes = b.nodes + 2
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
	if b.nodes != b.limits.TotalNodes {
		t.Fatal("output record was not reserved after the label", b.nodes, b.limits.TotalNodes)
	}
}

func TestSectionsReserveStackBeforeGrowth(t *testing.T) {
	d, b, index := sectionParse(t, "one\n\ntwo\n\nthree\n\nfour")
	b.limits.TotalNodes = b.nodes + 1
	b.limits.DocumentNodes = b.limits.TotalNodes // initial slot fits; root fanout does not.
	got, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
	var limit *LimitExceeded
	if !errors.As(err, &limit) || got != nil {
		t.Fatal(got, err)
	}
}

func TestSectionsLongUnicodeWhitespaceUsesOriginalWork(t *testing.T) {
	for _, text := range []string{strings.Repeat("\u3000", 2000), strings.Repeat("&nbsp;", 2000)} {
		d, b, index := sectionParse(t, "# A\n\n"+text)
		b.limits.WorkUnits = b.work + 128
		got, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
		var limit *LimitExceeded
		if !errors.As(err, &limit) || got != nil || limit.Name != "work-units" {
			t.Fatal(got, err)
		}
	}
	d, b, index := sectionParse(t, "# A\n\n\u3000")
	b.limits.WorkUnits = b.work + 128
	got, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
}

func TestSectionsBorrowedTextDoesNotConsumeNodeRecords(t *testing.T) {
	d, b, index := sectionParse(t, "# A\n\n"+strings.Repeat("\u3000", 2000))
	b.limits.TotalNodes = b.nodes + 20
	b.limits.DocumentNodes = b.limits.TotalNodes
	got, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	got[0].Final = false
	again, err := d.EmptySections(context.Background(), b, index, ParseOptions{})
	if err != nil || len(again) != 1 || !again[0].Final || again[0].Label.SourceToken() != d.Source().Token() {
		t.Fatal(again, err)
	}
}

func TestCommentsImageAltRawState(t *testing.T) {
	for _, tc := range []struct {
		source    string
		protected bool
	}{
		{"![<script>](x)\n\n<!-- c -->", false},
		{"text<script>\n\n![</script>](x)\n\n<!-- c -->", true},
		{"# ![<script>](x)\n\n<!-- c -->", false},
		{"text<script>\n\n# ![</script>](x)\n\n<!-- c -->", true},
	} {
		d, b, _ := sectionParse(t, tc.source)
		facts, err := d.Comments(context.Background(), b)
		if err != nil || len(facts) != 1 || facts[0].Protected != tc.protected || len(facts[0].Payload) != 1 || facts[0].Payload[0].Semantic.Value != " c " {
			t.Fatal(facts, err)
		}
	}
}

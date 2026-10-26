package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingRawCloser(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, text, visible string
			scalar              rune
			start, end          int
			code, escaped       bool
		}{
			{"emphasis", "# `x` <script>*payload</script>*!", "x !", '!', 32, 33, false, false},
			{"strong", "# `x` <script>**payload</script>**!", "x !", '!', 34, 35, false, false},
			{"link", "# `x` <script>[payload</script>](u)!", "x !", '!', 35, 36, false, false},
			{"flat", "# `x` <script>payload</script>!", "x !", '!', 30, 31, false, false},
			{"inner-tail", "# `x`<script>*payload</script>Tail*!", "xTail!", '!', 35, 36, false, false},
			{"entity", "# `x`<script>*payload</script>*&#33;", "x!", '!', 31, 36, false, false},
			{"letter", "# A!<script>*payload</script>* B", "A! B", 'B', 31, 32, false, false},
			{"escape", "# A!<script>*payload</script>*\\?", "A!?", '?', 30, 32, false, true},
			{"image", "# ![A!](u)", "A!", '!', 5, 6, false, false},
			{"image-tags", "# ![A<script>B</script>!](u)", "AB!", '!', 23, 24, false, false},
			{"raw-image", "# `x`<script>![payload</script>](u)!", "x", 'x', 0, 0, true, false},
			{"raw-code", "# `x`<script>`</script>`!", "x", 'x', 0, 0, true, false},
			{"nested-no-tail", "# `x` <script>*payload</script>*", "x ", 'x', 0, 0, true, false},
			{"raw-comment", "# `x`<script><!-- </script> -->!", "x", 'x', 0, 0, true, false},
			{"raw-unclosed", "# `x`<script>payload!", "x", 'x', 0, 0, true, false},
			{"unicode-crlf", "> # `x`<script>*payload</script>*雪！\r\n", "x*雪！", '！', 36, 39, false, false},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				source := parser.NewSource([]byte(tc.text))
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				parsed := parser.ParseWithOptions(ctx, source, budget, parser.ParseOptions{GFM: flavor == "gfm"})
				if parsed.Status != parser.Complete {
					t.Fatal(parsed)
				}
				headings, err := parsed.Document.Headings(ctx, budget)
				if err != nil || len(headings) != 1 || headings[0].Visible != tc.visible {
					t.Fatalf("visible: %+v, %v; want %q", headings, err, tc.visible)
				}
				terminals, err := parsed.Document.HeadingTerminals(ctx, budget)
				if err != nil || len(terminals) != 1 {
					t.Fatal(terminals, err)
				}
				got := terminals[0]
				if got.Scalar != tc.scalar || got.Code != tc.code || got.Escaped != tc.escaped || got.Token.Present() == tc.code {
					t.Fatalf("terminal: %+v", got)
				}
				if !tc.code {
					if got.Token.Start() != tc.start || got.Token.End() != tc.end {
						t.Fatalf("range: %+v; want [%d,%d)", got, tc.start, tc.end)
					}
					token, err := source.Text(got.Token)
					if err != nil || token != tc.text[tc.start:tc.end] {
						t.Fatal("lost original source", token, err)
					}
				}
			})
		}
	}
}

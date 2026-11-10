package md032_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md032"
)

// md032Producer runs only MD032's part of rules.Corrections.
func md032Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD032" && d.FixAvailable {
			findings = append(findings, d)
		}
	}
	return md032.Correction(ctx, c, findings)
}

func TestMD032CorrectionOutputs(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"before", "Text\n- One\n", "Text\n\n- One\n"},
		{"after", "- One\n# H\n", "- One\n\n# H\n"},
		{"both sides", "Text\n- One\n# H\n", "Text\n\n- One\n\n# H\n"},
		{"crlf", "Text\r\n- One\r\n", "Text\r\n\r\n- One\r\n"},
		{"crlf after", "- One\r\n# H\r\n", "- One\r\n\r\n# H\r\n"},
		{"cr", "Text\r- One\r", "Text\r\r- One\r"},
		{"unicode", "雪の日\n- 雪 ❄\n# 終わり\n", "雪の日\n\n- 雪 ❄\n\n# 終わり\n"},
		{"blockquote", "> Text\n> - One\n", "> Text\n>\n> - One\n"},
		{"nested quote", "> > Text\n> > - One\n", "> > Text\n> >\n> > - One\n"},
		{"loose nested list item", "- a\n\n  - b\n  # H\n", "- a\n\n  - b\n\n  # H\n"},
		{"quote in list", "1. a\n   > q\n   > - b\n   > # H\n", "1. a\n   > q\n   >\n   > - b\n   >\n   > # H\n"},
		{"fenced neighbours", "```\nx\n```\n- One\n```\ny\n```\n", "```\nx\n```\n\n- One\n\n```\ny\n```\n"},
		{"html neighbour", "<div>\nx\n</div>\n\nText\n- One\n", "<div>\nx\n</div>\n\nText\n\n- One\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := spacingCorrected(t, tc.source, "commonmark", rules.Corrections, "MD032")
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("%q complete %v", view.Candidate.Bytes(), out.Complete())
			}
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD032" {
					t.Fatal("finding remains", d)
				}
			}
			again, _ := spacingCorrected(t, tc.want, "commonmark", rules.Corrections, "MD032")
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
				t.Fatalf("second run %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD032CorrectionGroups(t *testing.T) {
	view, _ := spacingCorrected(t, "Text\n- One\n# H\n", "commonmark", md032Producer, "MD032")
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 2 {
		t.Fatal(decisions)
	}
	byID := map[string]fix.Decision{}
	for _, d := range decisions {
		byID[d.Group.ID] = d
	}
	for _, want := range []struct {
		id    string
		start int
	}{{"MD032-before-5", 5}, {"MD032-after-11", 11}} {
		d := byID[want.id]
		if !d.Accepted || d.Group.ID != want.id || d.Group.Anchor.Start() != want.start || d.Group.Anchor.End() != want.start || len(d.Group.Edits) != 1 || string(d.Group.Edits[0].Replacement) != "\n" || len(d.Group.Edits[0].Expected) != 0 {
			t.Fatalf("%+v", d)
		}
	}
}

// A blank line inside a tight list's item would make the list loose, and a tab
// in the container prefix can't be classified, so both findings stay manual.
func TestMD032CorrectionDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		start        int
	}{
		{"tightness change", "- a\n  - b\n  # H\n", 12},
		{"tightness change before", "- a\n  ```\n  x\n  ```\n  - b\n", 22},
		{"tab prefix", ">\tText\n>\t- One\n", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := spacingCorrected(t, tc.source, "commonmark", rules.Corrections, "MD032")
			if string(view.Candidate.Bytes()) != tc.source || len(view.Attempts[0].Decisions) != 0 {
				t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Attempts)
			}
			found := false
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD032" {
					found = found || d.ByteRange.Start == tc.start
					if d.FixAvailable {
						t.Fatal("deferred correction advertised", d)
					}
				}
			}
			if !found {
				t.Fatal("finding missing", view.Analysis.Diagnostics)
			}
		})
	}
}

func TestMD032UnrecognizedLeadingBlockGetsNoGroup(t *testing.T) {
	source := "---\nfoo: [\n---\nText\n- One\n"
	view, _ := spacingCorrected(t, source, "commonmark", rules.Corrections, "MD032", "MD009")
	if string(view.Candidate.Bytes()) != source || len(view.Attempts[0].Decisions) != 0 {
		t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Attempts)
	}
	for _, d := range view.Analysis.Diagnostics {
		if d.Rule == "MD032" && d.FixAvailable {
			t.Fatal("guarded correction advertised", d)
		}
	}
}

func TestMD032Composition(t *testing.T) {
	source := "#  Title  #\n\nText  \n- One \n## H"
	want := "# Title #\n\nText  \n\n- One\n\n## H\n"
	view, out := spacingCorrected(t, source, "commonmark", rules.Corrections, "MD009", "MD019", "MD021", "MD032", "MD047")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || len(view.Analysis.Diagnostics) != 0 {
		t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Analysis.Diagnostics)
	}
}

// The checker recomputes the line from the before parse, so a forged
// replacement is unsafe and the round defers.
func TestMD032CheckerRejectsForgedLine(t *testing.T) {
	forged := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		groups, err := md032Producer(ctx, c)
		for i := range groups {
			groups[i].Edits[0].Replacement = []byte("> \n")
		}
		return groups, err
	}
	source := "> Text\n> - One\n"
	view, _ := spacingCorrected(t, source, "commonmark", forged, "MD032")
	if string(view.Candidate.Bytes()) != source {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
	for _, d := range view.Attempts[0].Decisions {
		if d.Accepted {
			t.Fatal("forged group accepted", d)
		}
	}
}

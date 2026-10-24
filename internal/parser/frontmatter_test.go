package parser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

type frontmatterFixture struct {
	ID      string   `json:"id"`
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats"`
	Input   string   `json:"input"`
	Want    *struct {
		Format  string  `json:"format"`
		Region  [2]int  `json:"region"`
		Opener  [2]int  `json:"opener"`
		Content [2]int  `json:"content"`
		Closer  *[2]int `json:"closer"`
	} `json:"want"`
}

func TestFrontmatterBoundaryFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/frontmatter/boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []frontmatterFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty boundary matrix")
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.ID == "" || seen[fixture.ID] {
			t.Fatalf("missing or duplicate fixture ID %q", fixture.ID)
		}
		seen[fixture.ID] = true
		// The lexical detector has no flavor input. Exercise the same source
		// alongside both production parsers to pin that neither enables it
		// implicitly; #167 will reuse these boundaries for opaque blocks.
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(flavor+"/"+fixture.ID, func(t *testing.T) {
				source := NewSource([]byte(fixture.Input))
				formats := append([]string(nil), fixture.Formats...)
				got, err := detectFrontmatter(NewLineIndex(source), fixture.Enabled, formats)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(formats, fixture.Formats) {
					t.Fatal("detector mutated selected formats")
				}
				if fixture.Want == nil {
					if got != (frontmatterBoundary{}) {
						t.Fatalf("unexpected header: %+v", got)
					}
				} else {
					want := fixture.Want
					if got.format != want.Format {
						t.Fatalf("format=%q, want %q", got.format, want.Format)
					}
					checkBoundarySpan(t, source, got.region, &want.Region)
					checkBoundarySpan(t, source, got.opener, &want.Opener)
					checkBoundarySpan(t, source, got.content, &want.Content)
					checkBoundarySpan(t, source, got.closer, want.Closer)
				}
				parse := ParseWithBudget
				if flavor == "gfm" {
					parse = ParseGFMWithBudget
				}
				budget, err := NewOperationBudget(DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				result := parse(context.Background(), source, budget)
				if result.Status != Complete || result.Failure != nil || result.Source != source {
					t.Fatalf("boundary helper changed production parser availability: %+v", result)
				}
				if fixture.Want != nil && fixture.Want.Format == "yaml" && fixture.Want.Opener[0] == 0 {
					children := result.Document.Root().Children()
					if len(children) == 0 || children[0].Kind() != ThematicBreak {
						t.Fatal("production parser implicitly enabled YAML recognition")
					}
				}
				if string(source.Bytes()) != fixture.Input {
					t.Fatal("source bytes changed")
				}
			})
		}
	}
}

func checkBoundarySpan(t *testing.T, source Source, got Span, want *[2]int) {
	t.Helper()
	if want == nil {
		if got.Present() {
			t.Fatalf("unexpected span: %+v", got)
		}
		return
	}
	if !got.Present() || got.Start() != want[0] || got.End() != want[1] {
		t.Fatalf("span=[%d,%d) present=%v, want %v", got.Start(), got.End(), got.Present(), *want)
	}
	text, err := source.Text(got)
	if err != nil || text != source.content[want[0]:want[1]] {
		t.Fatalf("span lost original snapshot bytes: %q, %v", text, err)
	}
	if _, err := NewSource(source.Bytes()).Text(got); !errors.Is(err, ErrRange) {
		t.Fatal("span lost snapshot ownership")
	}
}

func TestFrontmatterBoundaryCancellation(t *testing.T) {
	source := NewSource([]byte("---\nx\n---\n"))
	index := NewLineIndex(source)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	index.ctx = ctx
	got, err := detectFrontmatter(index, true, []string{"yaml"})
	if !errors.Is(err, context.Canceled) || got != (frontmatterBoundary{}) {
		t.Fatalf("cancelled boundary scan=%+v, %v", got, err)
	}
}

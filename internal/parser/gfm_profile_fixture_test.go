package parser_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"unicode/utf8"
)

// These are required source projections for future consumers, not constructed
// production trees or observations of a GFM parser. Complete schema/semantic
// validation lives in tools/gfm_profile.py and is an explicit CI gate.
type profileFixtureNode struct {
	Kind     string
	Span     consumerRange
	Content  []consumerRange
	Children []profileFixtureNode
	Facts    json.RawMessage
}

type profileFixtureScope struct {
	HTML  string
	Nodes []profileFixtureNode
}

type profileFixture struct {
	ID          string
	Source      string
	InputSHA256 string `json:"input_sha256"`
	Frontmatter bool
	Core        profileFixtureScope
	Integrated  profileFixtureScope
}

func TestGFMProfileFixtureSourceInventory(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Fixtures []profileFixture
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	fixtures := make(map[string]profileFixture)
	for _, fixture := range bundle.Fixtures {
		if _, duplicate := fixtures[fixture.ID]; duplicate {
			t.Fatal("duplicate fixture", fixture.ID)
		}
		fixtures[fixture.ID] = fixture
		if fmt.Sprintf("%x", sha256.Sum256([]byte(fixture.Source))) != fixture.InputSHA256 || fixture.Frontmatter {
			t.Fatal("source identity/frontmatter changed", fixture.ID)
		}
		checkRange := func(r consumerRange) {
			t.Helper()
			if r.Start < 0 || r.End < r.Start || r.End > len(fixture.Source) ||
				!utf8.ValidString(fixture.Source[:r.Start]) || !utf8.ValidString(fixture.Source[:r.End]) ||
				fixture.Source[r.Start:r.End] != r.Raw {
				t.Fatalf("%s invalid original UTF-8 byte range: %+v", fixture.ID, r)
			}
		}
		var visit func(profileFixtureNode)
		visit = func(n profileFixtureNode) {
			checkRange(n.Span)
			for _, r := range n.Content {
				checkRange(r)
			}
			for _, child := range n.Children {
				visit(child)
			}
		}
		for _, scope := range []profileFixtureScope{fixture.Core, fixture.Integrated} {
			if len(scope.Nodes) == 0 {
				t.Fatal("missing source expectations", fixture.ID)
			}
			for _, n := range scope.Nodes {
				visit(n)
			}
		}
	}
	for _, n := range []int{140, 141, 142, 145, 147, 198, 199, 200, 201, 202, 203, 204, 205,
		279, 280, 324, 398, 426, 434, 435, 436, 473, 474, 475, 477, 491, 492, 493,
		611, 615, 617, 620, 621, 622, 623, 624, 625, 626, 627, 628, 629, 630, 631, 632, 633, 634, 635, 649, 650, 657} {
		if _, present := fixtures[fmt.Sprintf("published-%d", n)]; !present {
			t.Fatal("missing required authored interpretation", n)
		}
	}
	if got := fixtures["published-620"].Core.HTML; got != "<p>http://example.com</p>\n" {
		t.Fatal("core URL interpretation changed", got)
	}
	if got := fixtures["published-620"].Integrated.HTML; got != "<p><a href=\"http://example.com\">http://example.com</a></p>\n" {
		t.Fatal("integrated URL interpretation changed", got)
	}
	upper := fixtures["task-uppercase-crlf"]
	if upper.Source != "> - [X] é\r\n" {
		t.Fatal("uppercase task source changed")
	}
	var task struct {
		Marker, Check, Separator consumerRange
		Checked                  bool
	}
	taskNode := upper.Integrated.Nodes[0].Children[0].Children[0]
	if err := json.Unmarshal(taskNode.Facts, &task); err != nil {
		t.Fatal(err)
	}
	if task.Marker != (consumerRange{Start: 4, End: 7, Raw: "[X]"}) ||
		task.Check != (consumerRange{Start: 5, End: 6, Raw: "X"}) ||
		task.Separator != (consumerRange{Start: 7, End: 8, Raw: " "}) || !task.Checked {
		t.Fatal("independent uppercase task source contract changed", task)
	}
	if got := fixtures["published-635"].Integrated.Nodes[0].Span; got != (consumerRange{Start: 0, End: 20, Raw: "xmpp:foo@bar.baz/txt"}) {
		t.Fatal("xmpp second-slash boundary changed", got)
	}
}

func TestGFMProfileReviewedOwnerAndScalarLiterals(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Fixtures []profileFixture }
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	fixtures := make(map[string]profileFixture)
	for _, fixture := range bundle.Fixtures {
		fixtures[fixture.ID] = fixture
	}
	for identity, end := range map[string]int{"published-140": 130, "published-141": 68, "published-145": 29} {
		fixture := fixtures[identity]
		if got := fixture.Integrated.Nodes[0]; got.Kind != "html-block" || got.Span.Start != 0 || got.Span.End != end {
			t.Fatal("raw HTML owner includes following prose", identity, got.Span)
		}
		for _, n := range fixture.Integrated.Nodes[1:] {
			var facts struct{ Protected consumerRange }
			if err := json.Unmarshal(n.Facts, &facts); err != nil {
				t.Fatal(err)
			}
			if facts.Protected.Start != 0 || facts.Protected.End != end {
				t.Fatal("filtered tag lost its exact raw owner", identity, facts)
			}
		}
	}
	outer := fixtures["published-280"].Integrated.Nodes[0].Children[0]
	if outer.Span.Start != 0 || outer.Span.End != 34 || outer.Children[0].Kind != "list" ||
		outer.Children[0].Span.Start != 12 || outer.Children[0].Span.End != 34 ||
		outer.Children[0].Children[0].Span.Start != 12 || outer.Children[0].Children[1].Span.Start != 24 {
		t.Fatal("nested task owners were replaced by physical-line indentation", outer)
	}
	for _, scope := range []profileFixtureScope{fixtures["modern-reference-fold"].Core, fixtures["modern-reference-fold"].Integrated} {
		paragraph := scope.Nodes[0]
		if paragraph.Span != (consumerRange{Start: 0, End: 6, Raw: "[ẞ]\n"}) ||
			len(paragraph.Content) != 1 || paragraph.Content[0] != (consumerRange{Start: 0, End: 5, Raw: "[ẞ]"}) {
			t.Fatal("reference definition became paragraph content", paragraph)
		}
	}
	for _, scope := range []profileFixtureScope{fixtures["modern-entity-one-pass"].Core, fixtures["modern-entity-one-pass"].Integrated} {
		if scope.HTML != "<p>&amp;copy; � \u0080</p>\n" {
			t.Fatal("numeric entity used browser remapping", scope.HTML)
		}
		var facts struct{ Value string }
		if err := json.Unmarshal(scope.Nodes[0].Facts, &facts); err != nil {
			t.Fatal(err)
		}
		scalars := []rune(facts.Value)
		if len(scalars) == 0 || scalars[len(scalars)-1] != '\u0080' {
			t.Fatal("numeric entity semantic scalar changed", facts.Value)
		}
	}
}

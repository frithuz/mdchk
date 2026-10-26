package resource

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestAnchorRawCloserVisibleIdentity(t *testing.T) {
	text := "# A!<script>*payload</script>* B\n\n# A!\n"
	_, index := readAnchorFixture(t, newAnchorFixture(t, text, "github", parser.ParseOptions{}))
	headings, err := index.Headings(context.Background())
	if err != nil || len(headings) != 2 {
		t.Fatal(headings, err)
	}
	for i, want := range []struct{ visible, id string }{{"A! B", "a-b"}, {"A!", "a"}} {
		got := headings[i]
		if got.Visible != want.visible || got.Base != parser.Some(want.id) || got.ID != parser.Some(want.id) || !got.Usable {
			t.Fatal(got, want)
		}
		matches, err := index.Lookup(context.Background(), want.id)
		if err != nil || len(matches) != 1 {
			t.Fatal(want.id, matches, err)
		}
	}
	// GFM tag filtering retains its existing uncertainty about raw HTML IDs.
	f := newAnchorFixture(t, text, "github", parser.ParseOptions{GFM: true})
	spec, err := f.service.Read(context.Background(), serviceRequest(t, f, "a.md", ParsedAnchors))
	if err != nil || spec.Anchors == Available || spec.Index.state != nil {
		t.Fatal("filtered raw HTML manufactured authoritative anchors", spec, err)
	}
}

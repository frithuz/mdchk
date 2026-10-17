package parser_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestScannerCorpusTargets(t *testing.T) {
	const base = "testdata/commonmark/0.31.2/"
	var registry struct {
		Version int `json:"schema_version"`
		Targets []struct {
			Capability string `json:"capability"`
			Section    string `json:"section"`
			Examples   []int  `json:"examples"`
		} `json:"targets"`
	}
	read := func(path string, value any) {
		t.Helper()
		data, err := os.ReadFile(base + path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatal(err)
		}
	}
	read("scanner.json", &registry)
	ids := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if registry.Version != 1 || len(registry.Targets) != 1 || registry.Targets[0].Capability != "scanner-indentation" || registry.Targets[0].Section != "Tabs" || !reflect.DeepEqual(registry.Targets[0].Examples, ids) {
		t.Fatal("scanner target registry changed identity/section/IDs")
	}
	var examples []struct {
		ID       int    `json:"example"`
		Section  string `json:"section"`
		Markdown string `json:"markdown"`
	}
	read("examples.json", &examples)
	// Independent spellings from the pinned upstream Tabs examples. These checks
	// catch lost tab conversion, Unicode or trailing endings in the retained input.
	want := []string{
		"\tfoo\tbaz\t\tbim\n", "  \tfoo\tbaz\t\tbim\n", "    a\ta\n    ὐ\ta\n",
		"  - foo\n\n\tbar\n", "- foo\n\n\t\tbar\n", ">\t\tfoo\n", "-\t\tfoo\n",
		"    foo\n\tbar\n", " - foo\n   - bar\n\t - baz\n", "#\tFoo\n", "*\t*\t*\t\n",
	}
	seen := make(map[int]bool)
	for _, example := range examples {
		if example.ID < 1 || example.ID > 652 || seen[example.ID] {
			t.Fatalf("invalid/duplicate upstream identity %d", example.ID)
		}
		seen[example.ID] = true
		if example.ID > 11 {
			continue
		}
		if example.Section != "Tabs" || example.Markdown != want[example.ID-1] {
			t.Fatalf("Tabs example %d differs from original input", example.ID)
		}
		source := parser.NewSource([]byte(example.Markdown))
		index := parser.NewLineIndex(source)
		var restored bytes.Buffer
		for n := 1; n <= index.LineCount(); n++ {
			line, err := index.Line(n)
			if err != nil {
				t.Fatal(err)
			}
			content, _ := source.Text(line.Content)
			ending, _ := source.Text(line.Ending)
			restored.WriteString(content)
			restored.WriteString(ending)
			cursor, err := index.Indentation(n)
			if err != nil || cursor.Remaining().Semantic != parser.Some(content) {
				t.Fatalf("unconsumed Tabs input changed: %d line %d", example.ID, n)
			}
			if _, err := cursor.Consume(4); err != nil {
				t.Fatal(err)
			}
		}
		if restored.String() != example.Markdown || string(source.Bytes()) != example.Markdown {
			t.Fatal("scanner changed retained upstream input")
		}
	}
	if len(seen) != 652 {
		t.Fatal("upstream inventory is incomplete")
	}
	// TestCoreCorpusAccounting separately checks all 652 supported/pending results.
	// Example 11 now passes via actual thematic parsing, not these helper checks.
}

func TestLineIndexConcurrentReaders(t *testing.T) {
	source := parser.NewSource([]byte("é\t\x00\r\nZ"))
	index := parser.NewLineIndex(source)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 20 {
				pos, err := index.Position(6)
				if err != nil || pos != (parser.Position{2, 1}) {
					t.Error("shared immutable index changed")
				}
				cursor, err := index.Indentation(1)
				if err != nil {
					t.Error(err)
					return
				}
				_, err = cursor.AdvanceMarker()
				if err != nil {
					t.Error(err)
				}
				if _, err := cursor.Consume(1); err != nil || cursor.Remaining().Semantic.Value != "  \ufffd" {
					t.Error("independent cursor phase or semantics changed")
				}
			}
		})
	}
	workers.Wait()
}

package parser_test

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/parser"
)

func ExampleParse() {
	for _, content := range []string{"", " \t\r\n", "hello\n"} {
		result := parser.Parse(context.Background(), parser.NewSource([]byte(content)))
		fmt.Println(result.Status, result.AST, result.Document.References().LookupAvailability)
		fmt.Println("paragraphs:", len(result.Document.Root().Children()))
		for _, deficit := range result.Uncertainty {
			fmt.Println(deficit.Region.Start(), deficit.Region.End(), deficit.Reason)
		}
	}
	// Output:
	// complete available available
	// paragraphs: 0
	// complete available available
	// paragraphs: 0
	// complete available available
	// paragraphs: 1
}

func ExampleParse_decoding() {
	result := parser.Parse(context.Background(), parser.NewSource([]byte("é&amp;β")))
	text := result.Document.Root().Children()[0].Children()[0]
	fmt.Println(result.Status, text.Properties().Literal.Value)
	for _, part := range text.Content() {
		raw, _ := result.Source.Text(part.Source)
		fmt.Printf("[%d,%d) %s %q -> %q\n", part.Source.Start(), part.Source.End(), part.Transformation, raw, part.Semantic.Value)
	}
	// Output:
	// complete é&β
	// [0,2) exact "é" -> "é"
	// [2,7) entity "&amp;" -> "&"
	// [7,9) exact "β" -> "β"
}

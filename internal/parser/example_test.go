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
	// incomplete partial available
	// paragraphs: 1
	// 0 6 syntax-unimplemented
}

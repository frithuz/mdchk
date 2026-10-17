package parser_test

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/parser"
)

func ExampleParse() {
	for _, content := range []string{"", "hello\n"} {
		result := parser.Parse(context.Background(), parser.NewSource([]byte(content)))
		fmt.Println(result.Status, result.AST, result.Document.References().LookupAvailability)
		for _, deficit := range result.Uncertainty {
			fmt.Println(deficit.Region.Start(), deficit.Region.End(), deficit.Reason)
		}
	}
	// Output:
	// complete available available
	// incomplete partial unavailable
	// 0 6 syntax-unimplemented
}

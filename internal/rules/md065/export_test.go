package md065

import "github.com/frithuz/mdchk/internal/parser"

// CompareHorizontalRuleBlankLines lets the guard tests call the comparison directly.
var CompareHorizontalRuleBlankLines = compareHorizontalRuleBlankLines

// BlankLineFacts lets the correction tests index constructed facts directly.
var BlankLineFacts = blankLineFacts

// Line returns the blank line every fact at the boundary derives.
func (b boundary) Line() []byte { return b.line }

// Facts returns the facts claiming the boundary.
func (b boundary) Facts() []parser.ThematicSpacingFact { return b.facts }

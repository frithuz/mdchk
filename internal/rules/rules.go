// Package rules supplies production read-only checks and correction providers.
// Source ownership, decoding, directives, coordinates and budgets remain engine
// concerns. Registrations returns a fresh slice, with no mutable global registry.
package rules

import "github.com/frithuz/mdchk/internal/engine"

func Registrations() []engine.Registration {
	return []engine.Registration{{ID: "MD047", Check: terminatingNewline}, {ID: "MD001", Check: headingIncrements}, {ID: "MD003", Check: headingStyle}, {ID: "MD004", Check: bulletMarkerStyle}, {ID: "MD005", Check: siblingListAlignment}, {ID: "MD007", Check: bulletNestingIndentation}, {ID: "MD029", Check: numberedListPrefixes}, {ID: "MD030", Check: listMarkerSpacing}, {ID: "MD032", Check: listBlankLines}, {ID: "MD018", Check: openingHashSpacing}, {ID: "MD019", Check: extraOpeningHashSpaces}, {ID: "MD020", Check: closingHashSpacing}, {ID: "MD021", Check: closedHeadingSpaces}, {ID: "MD022", Check: headingBlankLines}, {ID: "MD023", Check: indentedHeadings}, {ID: "MD024", Check: repeatedHeadingText}, {ID: "MD025", Check: multipleDocumentTitles}, {ID: "MD026", Check: headingEndPunctuation}, {ID: "MD036", Check: emphasisUsedAsHeading}, {ID: "MD041", Check: openingDocumentHeading}, {ID: "MD043", Check: requiredHeadingOutline}, {ID: "MD063", Check: headingCapitalization}, {ID: "MD080", Check: collidingHeadingAnchors}, {ID: "MD082", Check: sectionsWithoutContent}, {ID: "MD090", Check: thematicBreaksBeforeHeadings, Applies: thematicHeadingsApply}, {ID: "MD093", Check: inlineFormattingInHeadings}}
}

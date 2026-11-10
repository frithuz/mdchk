// Package rules supplies production read-only checks and correction providers.
// Source ownership, decoding, directives, coordinates and budgets remain engine
// concerns. Registrations returns a fresh slice, with no mutable global registry.
package rules

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/md001"
	"github.com/frithuz/mdchk/internal/rules/md003"
	"github.com/frithuz/mdchk/internal/rules/md004"
	"github.com/frithuz/mdchk/internal/rules/md005"
	"github.com/frithuz/mdchk/internal/rules/md007"
	"github.com/frithuz/mdchk/internal/rules/md009"
	"github.com/frithuz/mdchk/internal/rules/md010"
	"github.com/frithuz/mdchk/internal/rules/md012"
	"github.com/frithuz/mdchk/internal/rules/md013"
	"github.com/frithuz/mdchk/internal/rules/md018"
	"github.com/frithuz/mdchk/internal/rules/md019"
	"github.com/frithuz/mdchk/internal/rules/md020"
	"github.com/frithuz/mdchk/internal/rules/md021"
	"github.com/frithuz/mdchk/internal/rules/md022"
	"github.com/frithuz/mdchk/internal/rules/md023"
	"github.com/frithuz/mdchk/internal/rules/md027"
	"github.com/frithuz/mdchk/internal/rules/md028"
	"github.com/frithuz/mdchk/internal/rules/md029"
	"github.com/frithuz/mdchk/internal/rules/md030"
	"github.com/frithuz/mdchk/internal/rules/md032"
	"github.com/frithuz/mdchk/internal/rules/md047"
	"github.com/frithuz/mdchk/internal/rules/md064"
	"github.com/frithuz/mdchk/internal/rules/md069"
	"github.com/frithuz/mdchk/internal/rules/md076"
	"github.com/frithuz/mdchk/internal/rules/md077"
	"github.com/frithuz/mdchk/internal/rules/md085"
	"github.com/frithuz/mdchk/internal/rules/md089"
)

func Registrations() []engine.Registration {
	return []engine.Registration{
		md001.Registration(),
		md003.Registration(),
		md004.Registration(),
		md005.Registration(),
		md007.Registration(),
		md009.Registration(),
		md010.Registration(),
		{ID: "MD011", Check: swappedLinkDelimiters},
		md012.Registration(),
		md013.Registration(),
		{ID: "MD014", Check: shellPrompts},
		md018.Registration(),
		md019.Registration(),
		md020.Registration(),
		md021.Registration(),
		md022.Registration(),
		md023.Registration(),
		{ID: "MD024", Check: repeatedHeadingText},
		{ID: "MD025", Check: multipleDocumentTitles},
		{ID: "MD026", Check: headingEndPunctuation},
		md027.Registration(),
		md028.Registration(),
		md029.Registration(),
		md030.Registration(),
		{ID: "MD031", Check: fencedBlockBlankLines},
		md032.Registration(),
		{ID: "MD033", Check: embeddedHTML},
		{ID: "MD034", Check: bareURLs},
		{ID: "MD035", Check: horizontalRuleStyle},
		{ID: "MD036", Check: emphasisUsedAsHeading},
		{ID: "MD037", Check: emphasisWhitespace},
		{ID: "MD038", Check: codeSpanWhitespace},
		{ID: "MD039", Check: linkLabelWhitespace},
		{ID: "MD040", Check: fencedBlockLanguages},
		{ID: "MD041", Check: openingDocumentHeading},
		{ID: "MD042", Check: emptyLinkDestinations},
		{ID: "MD043", Check: requiredHeadingOutline},
		{ID: "MD044", Check: configuredNameCapitalization},
		{ID: "MD045", Check: imageAlternatives},
		{ID: "MD046", Check: codeBlockStyles},
		md047.Registration(),
		{ID: "MD048", Check: fencedBlockStyles},
		{ID: "MD049", Check: italicDelimiterStyle},
		{ID: "MD050", Check: boldDelimiterStyle},
		{ID: "MD051", Check: unresolvedFragmentTargets},
		{ID: "MD052", Check: undefinedReferences},
		{ID: "MD053", Check: unneededDefinitions},
		{ID: "MD054", Check: linkRepresentationStyle},
		{ID: "MD055", Check: outerTablePipes},
		{ID: "MD056", Check: mismatchedTableCells},
		{ID: "MD057", Check: missingLocalTargets},
		{ID: "MD058", Check: tableBlankLines},
		{ID: "MD059", Check: uninformativeLinkLabels},
		{ID: "MD060", Check: tableColumnLayout},
		{ID: "MD061", Check: prohibitedVocabulary},
		{ID: "MD062", Check: destinationWhitespace},
		{ID: "MD063", Check: headingCapitalization},
		md064.Registration(),
		{ID: "MD065", Check: horizontalRuleBlankLines},
		md069.Registration(),
		{ID: "MD070", Check: nestedMarkdownExamples},
		{ID: "MD071", Check: metadataBlankLine, Applies: metadataBlankLineApplies},
		{ID: "MD072", Check: metadataKeyOrdering},
		{ID: "MD073", Check: tableOfContents},
		{ID: "MD075", Check: headerlessPipeRows},
		md076.Registration(),
		md077.Registration(),
		{ID: "MD080", Check: collidingHeadingAnchors},
		{ID: "MD081", Check: excessiveParagraphEmphasis},
		{ID: "MD082", Check: sectionsWithoutContent},
		{ID: "MD083", Check: suspiciousSequences},
		{ID: "MD084", Check: hiddenCharacters},
		md085.Registration(),
		{ID: "MD086", Check: unclosedHTMLComments},
		{ID: "MD087", Check: engine.CheckUnusedSuppressions},
		{ID: "MD088", Check: asciiPunctuation},
		md089.Registration(),
		{ID: "MD091", Check: markdownInsideHTML},
		{ID: "MD092", Check: conflictMarkers},
		{ID: "MD093", Check: inlineFormattingInHeadings},
		{ID: "MD095", Check: lookalikeHostnames},
	}
}

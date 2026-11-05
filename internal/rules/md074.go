package rules

import (
	"context"
	"strings"

	"github.com/frithuz/mdchk/internal/integration"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// MkDocsRecord is one MD074 observation in MkDocs configuration source terms.
// Located is false only when no configuration source exists.
type MkDocsRecord struct {
	Message string
	Span    parser.Span
	Located bool
}

// MkDocsOutcome holds MD074 findings and reduced coverage. Findings are always
// located in the configuration source; coverage is context-unavailable.
type MkDocsOutcome struct {
	Findings []MkDocsRecord
	Coverage []MkDocsRecord
}

// MkDocsNavigation interprets an acquired MkDocs context for MD074. It reads
// nothing: every fact comes from the context's retained producer observations.
// Missing targets are findings only under a static context, and omissions only
// when the scope proves a complete inventory. Every other uncertainty becomes
// coverage. A returned error accompanies the records established before it.
func MkDocsNavigation(ctx context.Context, scope *integration.Scope, c integration.MkDocsContext) (MkDocsOutcome, error) {
	var out MkDocsOutcome
	policy := scope.MkDocsPolicy()
	config := c.ConfigSource()
	if config.Token() == (parser.SourceToken{}) {
		out.Coverage = append(out.Coverage, MkDocsRecord{Message: "MkDocs configuration is missing, unreadable or not a regular file: " + policy.ConfigPath + "."})
		return out, nil
	}
	finding := func(span parser.Span, message string) {
		out.Findings = append(out.Findings, MkDocsRecord{Message: message, Span: span, Located: true})
	}
	uncertain := func(span parser.Span, message string) {
		out.Coverage = append(out.Coverage, MkDocsRecord{Message: message, Span: span, Located: true})
	}
	problems, err := c.Problems(ctx, scope)
	if err != nil {
		return out, err
	}
	for _, problem := range problems {
		uncertain(problem.Location().Span(), problem.Message())
	}
	if c.Status() != integration.Available && len(problems) == 0 {
		whole, _ := config.Span(0, config.Len())
		uncertain(whole, "MkDocs navigation context is unavailable.")
	}
	targets, err := c.Targets(ctx, scope)
	if err != nil {
		return out, err
	}
	navigated := make(map[string]bool, len(targets))
	for _, target := range targets {
		span := target.Location().Span()
		destination := target.Destination()
		switch target.Class() {
		case resource.Ignored:
			continue
		case resource.Local:
		default:
			uncertain(span, "MkDocs navigation target cannot be resolved: "+destination+".")
			continue
		}
		if key, ok := target.Target().Key(); ok {
			navigated[key.String()] = true
		}
		existence, required := target.Existence(), target.RequiredType()
		switch {
		case existence == resource.Missing && c.Static():
			finding(span, "MkDocs navigation target does not exist: "+destination+".")
		case existence == resource.Missing:
			uncertain(span, "MkDocs navigation target is missing, but dynamic settings may generate it: "+destination+".")
		case existence == resource.Unknown:
			uncertain(span, "MkDocs navigation target existence is unknown: "+destination+".")
		case required == resource.RegularFile && existence == resource.Directory || required == resource.DirectoryType && existence == resource.File:
			uncertain(span, "MkDocs navigation target has an unexpected type: "+destination+".")
		}
	}
	navigation := c.Navigation()
	if !policy.CheckOmitted || navigation.Mode() != integration.ManualNav {
		return out, nil
	}
	definition := navigation.Definition().Span()
	allowed, err := scope.CanCheckOmissions(ctx, navigation, c.Inventory(), c.Static())
	if err != nil {
		return out, err
	}
	if !allowed {
		uncertain(definition, "Pages omitted from MkDocs navigation could not be checked.")
		return out, nil
	}
	pages, err := c.Inventory().Pages(ctx, scope)
	if err != nil {
		return out, err
	}
	root := c.DocsRoot().Name()
	for _, page := range pages {
		key := page.Key().String()
		if page.Eligibility() != integration.EligiblePage || navigated[key] {
			continue
		}
		if root != "" {
			key = strings.TrimPrefix(key, root+"/")
		}
		finding(definition, "Page is not included in MkDocs navigation: "+key+".")
	}
	return out, nil
}

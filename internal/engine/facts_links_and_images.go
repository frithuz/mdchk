// Rule fact facades for the links-and-images rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// SwappedLinks retains both caller and originating controls and returns only
// detached observations. It grants no alternate parse or correction authority.
// Serves MD011.
func (c RuleContext) SwappedLinks(ctx context.Context) (out parser.SwappedLinkInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.SwappedLinkInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.SwappedLinks(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// BareURLs retains both caller and originating controls and returns only
// detached observations. It grants no alternate parse or correction authority.
// Serves MD034.
func (c RuleContext) BareURLs(ctx context.Context) (out parser.BareURLInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.BareURLInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.BareURLs(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// LinkLabelWhitespace retains both caller and originating controls and returns only
// detached label-edge observations. It grants no alternate parse or correction authority.
// Serves MD039.
func (c RuleContext) LinkLabelWhitespace(ctx context.Context) (out parser.LinkLabelWhitespaceInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkLabelWhitespaceInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.LinkLabelWhitespace(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// EmptyLinkDestinations retains both caller and originating controls and returns only
// detached destination observations. It grants no alternate parse or correction authority.
// Serves MD042.
func (c RuleContext) EmptyLinkDestinations(ctx context.Context) (out parser.EmptyLinkDestinationsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.EmptyLinkDestinationsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.EmptyLinkDestinations(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// ImageAlternatives retains both caller and originating controls and returns only
// detached image alternative observations. It grants no alternate parse or correction authority.
// Serves MD045.
func (c RuleContext) ImageAlternatives(ctx context.Context) (out parser.ImageAlternativesInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.ImageAlternativesInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	decorative, ok := c.options["allow_decorative"].(bool)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.ImageAlternatives(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), decorative)
}

// FragmentLinks retains both caller and originating controls and returns only
// detached link destination observations. It grants no alternate parse or correction authority.
// Serves MD051 and MD057.
func (c RuleContext) FragmentLinks(ctx context.Context) (out parser.FragmentLinksInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FragmentLinksInventory{}
		}
	}()
	if _, ok := c.options["cross_file"].(bool); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.FragmentLinks(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// CrossFile reports the validated rule option; it does not grant resource access.
// Serves MD051 and MD057.
func (c RuleContext) CrossFile() bool {
	v, _ := c.options["cross_file"].(bool)
	return v
}

// UndefinedReferences retains both caller and originating controls and returns
// only detached observations. It grants no alternate parse or correction
// authority.
// Serves MD052.
func (c RuleContext) UndefinedReferences(ctx context.Context) (out parser.UndefinedReferenceInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.UndefinedReferenceInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.UndefinedReferences(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// UnneededDefinitions retains both caller and originating controls and returns
// only detached observations. ignored_labels is read from admitted immutable
// options; it grants no alternate parse or correction authority.
// Serves MD053.
func (c RuleContext) UnneededDefinitions(ctx context.Context) (out parser.UnneededDefinitionInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.UnneededDefinitionInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	list, ok := c.options["ignored_labels"].([]string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(list))+1, int64(len(list))); err != nil {
		return out, err
	}
	ignored := make([]string, len(list))
	copy(ignored, list)
	return c.document.UnneededDefinitions(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), ignored)
}

// LinkRepresentations retains both caller and originating controls and returns only
// detached link and image style observations. It grants no alternate parse or correction authority.
// Serves MD054.
func (c RuleContext) LinkRepresentations(ctx context.Context) (out parser.LinkRepresentationsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkRepresentationsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if _, ok := c.options["allowed_styles"].([]string); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.LinkRepresentations(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// LinkLabels retains both caller and originating controls and returns only
// detached visible link label observations. It grants no alternate parse or correction authority.
// Serves MD059.
func (c RuleContext) LinkLabels(ctx context.Context) (out parser.LinkLabelsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkLabelsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if _, ok := c.options["phrases"].([]string); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.LinkLabels(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// DestinationWhitespace retains both caller and originating controls and returns
// only detached observations. It grants no alternate parse or correction
// authority.
// Serves MD062.
func (c RuleContext) DestinationWhitespace(ctx context.Context) (out parser.DestinationWhitespaceInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.DestinationWhitespaceInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.DestinationWhitespace(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// LinkDestinations retains both caller and originating controls and returns
// only detached destination observations. It grants no alternate parse or
// correction authority.
// Serves MD095.
func (c RuleContext) LinkDestinations(ctx context.Context) (out parser.LinkDestinationsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkDestinationsInventory{}
		}
	}()
	if _, err = c.AllowedHosts(ctx); err != nil {
		return out, err
	}
	return c.document.LinkDestinations(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// AllowedHosts returns detached entry storage from admitted immutable options,
// in the spelling the user wrote. It does not canonicalize the entries.
// Serves MD095.
func (c RuleContext) AllowedHosts(ctx context.Context) (out []string, err error) {
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return nil, err
	}
	list, ok := c.options["allowed_hosts"].([]string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(list))+1, int64(len(list))); err != nil {
		return nil, err
	}
	out = make([]string, len(list))
	copy(out, list)
	return out, nil
}

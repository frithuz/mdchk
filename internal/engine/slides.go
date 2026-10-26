package engine

import (
	"context"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"strings"
	"unicode/utf8"
)

// SlideContext is explicit annotation of one selected document. Every field is a
// value; returned views cannot change matching, configuration or candidate policy.
// Known false is unavailable, not an ordinary-document determination.
type SlideContext struct {
	Known, Slides              bool
	Pattern                    string
	PatternIndex               int
	OriginPath, OriginProperty string
	OriginPosition             parser.Position
}

// SlideProvider annotates an input path under immutable adapter/configuration
// authority. Implementations perform no discovery or resource acquisition and
// must use the caller's original operation budget. The plan retains this policy
// for original, candidate and saved-view inputs with the same logical identity.
type SlideProvider interface {
	Slides(context.Context, *parser.OperationBudget, string) (SlideContext, error)
}

// WithSlideProvider derives a plan with an immutable per-path annotation policy.
// The provider must not expose mutable retained policy through returned views.
func (p *ExecutionPlan) WithSlideProvider(provider SlideProvider) *ExecutionPlan {
	if p == nil {
		return nil
	}
	q := *p
	q.slides = provider
	return &q
}

func (r *Registry) applicable(ctx context.Context, c RuleContext, m Metadata) (bool, error) {
	if m.AppliesTo != nil && !m.AppliesTo(c.Context()) {
		return false, nil
	}
	if applies := r.applies[m.ID]; applies != nil {
		return applies(ctx, c)
	}
	return true, nil
}

func (c SlideContext) validate() error {
	if !c.Known {
		return fmt.Errorf("slide context is unavailable")
	}
	if !c.Slides {
		if c != (SlideContext{Known: true}) {
			return fmt.Errorf("ordinary slide context has match data")
		}
		return nil
	}
	if c.Pattern == "" || c.PatternIndex < 0 {
		return fmt.Errorf("matched slide context requires a pattern")
	}
	for _, s := range []string{c.Pattern, c.OriginPath, c.OriginProperty} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return fmt.Errorf("invalid slide context text")
		}
	}
	if c.OriginPosition != (parser.Position{}) && (c.OriginPosition.Line < 1 || c.OriginPosition.Column < 1) {
		return fmt.Errorf("invalid slide origin position")
	}
	return nil
}

package config

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// SlidePolicy is immutable selected configuration policy. Matching annotates
// already-selected documents directly; it neither selects nor excludes entries.
type SlidePolicy struct {
	matcher  *Patterns
	patterns []string
	origin   Origin
	origins  []Origin
}

// ErrInvalidSlideOrigin identifies malformed selected-configuration provenance.
var ErrInvalidSlideOrigin = errors.New("invalid slide origin")

// NewSlidePolicy validates and owns the complete list under the supplied meter.
// Scoped integration callers retain their separate document/view authority.
func NewSlidePolicy(ctx context.Context, budget *parser.OperationBudget, patterns []string, origin Origin) (*SlidePolicy, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("slide policy requires operation controls")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(patterns) > MaxPatterns {
		_, err := CompilePatterns(patterns)
		return nil, err
	}
	var bytes int64
	for i, p := range patterns {
		bytes += int64(len(p))
		if len(p) > MaxPatternBytes || bytes > MaxPatternListBytes {
			return nil, &PatternError{Index: i, Pattern: p, Err: ErrPatternLimit}
		}
	}
	originBytes := int64(len(origin.Path)) + int64(len(origin.PropertyPath))
	if originBytes > budget.Limits().DocumentBytes {
		return nil, &parser.LimitExceeded{Name: "document-bytes", Observed: originBytes, Allowed: budget.Limits().DocumentBytes}
	}
	if err := budget.ChargeWork(8*(bytes+originBytes) + 4*int64(len(patterns)) + 1); err != nil {
		return nil, err
	}
	// Reserve the original work allowance before scanning any origin bytes.
	if !utf8.ValidString(origin.Path) || !utf8.ValidString(origin.PropertyPath) || strings.ContainsRune(origin.Path, 0) || strings.ContainsRune(origin.PropertyPath, 0) || origin.Position != nil && (origin.Position.Line < 1 || origin.Position.Column < 1) {
		return nil, ErrInvalidSlideOrigin
	}
	var segments int64
	for _, p := range patterns {
		segments += int64(strings.Count(p, "/") + 1)
	}
	if err := budget.ChargeNodes(2*segments + int64(len(patterns)) + 2); err != nil {
		return nil, err
	}
	matcher, err := CompilePatterns(patterns)
	if err != nil {
		return nil, err
	}
	p := ownedSlidePolicy(patterns, matcher, origin)
	if err := errors.Join(ctx.Err(), budget.ChargeWork(0)); err != nil {
		return nil, err
	}
	return p, nil
}
func ownedSlidePolicy(patterns []string, matcher *Patterns, origin Origin) *SlidePolicy {
	return &SlidePolicy{matcher: ownedPatterns(matcher), patterns: slices.Clone(patterns), origin: cloneOrigin(origin)}
}

// Match returns known ordinary or the first explicit direct pattern match.
// Outside-base names are not inspected. Failed work returns no known annotation.
func (p *SlidePolicy) Match(ctx context.Context, budget *parser.OperationBudget, name string, inBase bool) (out engine.SlideContext, err error) {
	if p == nil || p.matcher == nil || ctx == nil || budget == nil {
		return engine.SlideContext{}, errors.New("invalid slide annotation operation")
	}
	defer func() {
		err = errors.Join(err, ctx.Err(), budget.ChargeWork(0))
		if err != nil {
			out = engine.SlideContext{}
		}
	}()
	if err := errors.Join(ctx.Err(), budget.ChargeWork(1)); err != nil {
		return engine.SlideContext{}, err
	}
	out = engine.SlideContext{Known: true}
	if !inBase {
		return out, nil
	}
	i, matched, err := p.matcher.MatchFirstBounded(ctx, budget, name, false, true)
	if err != nil {
		return engine.SlideContext{}, err
	}
	if matched {
		out.Slides = true
		out.Pattern = p.patterns[i]
		out.PatternIndex = i
		origin := p.origin
		if len(p.origins) > i {
			origin = p.origins[i]
		}
		out.OriginPath = origin.Path
		out.OriginProperty = origin.PropertyPath
		if origin.Position != nil {
			out.OriginPosition = *origin.Position
		}
	}
	if err := errors.Join(ctx.Err(), budget.ChargeWork(0)); err != nil {
		return engine.SlideContext{}, err
	}
	return out, nil
}

type slideProvider struct {
	policy                 *SlidePolicy
	workingDirectory, base string
}

func (p slideProvider) Slides(ctx context.Context, budget *parser.OperationBudget, name string) (engine.SlideContext, error) {
	if p.base == "" {
		return p.policy.Match(ctx, budget, name, true)
	}
	if ctx == nil || budget == nil {
		return engine.SlideContext{}, errors.New("invalid slide path operation")
	}
	if err := errors.Join(ctx.Err(), budget.ChargeWork(int64(len(name)+len(p.workingDirectory)+len(p.base))+1)); err != nil {
		return engine.SlideContext{}, err
	}
	native := filepath.FromSlash(name)
	if filepath.Clean(native) != native {
		return engine.SlideContext{}, errors.New("slide input identity is not normalized")
	}
	if err := budget.ChargeNodes(3); err != nil {
		return engine.SlideContext{}, err
	}
	target := native
	if !filepath.IsAbs(target) {
		target = filepath.Join(p.workingDirectory, native)
	}
	rel, err := filepath.Rel(p.base, target)
	if err != nil {
		return p.policy.Match(ctx, budget, "", false)
	}
	inBase := rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	return p.policy.Match(ctx, budget, filepath.ToSlash(rel), inBase)
}

// WithDocumentPaths binds the adapter's native identity mapping once. Input report
// paths remain invocation-relative; patterns remain selected-config-relative.
// No filesystem or configuration is accessed, and candidate plans retain it.
func (p *ExecutionPlan) WithDocumentPaths(workingDirectory, base string) (*ExecutionPlan, error) {
	if p == nil || p.engine == nil || p.configuration == nil || !filepath.IsAbs(workingDirectory) || !filepath.IsAbs(base) {
		return nil, errors.New("slide path bases must be absolute")
	}
	q := *p
	policy := p.slides
	if policy == nil {
		return nil, errors.New("slide policy is unavailable")
	}
	q.engine = p.engine.WithSlideProvider(slideProvider{policy: policy, workingDirectory: workingDirectory, base: base})
	return &q, nil
}

package engine

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// DocumentContext carries explicit adapter-resolved context, not guessed
// filename dialects. The parser provider here recognizes CommonMark only;
// requesting another flavor leaves syntax-dependent rules unassessed.
type DocumentContext struct {
	Flavor  string
	Project string
}
type RuleSetting struct {
	Enabled         *bool
	AllowDirectives *bool
	Options         Options
}
type Settings map[string]RuleSetting

type Input struct {
	path            string
	source          parser.Source
	settings        Settings
	documentContext DocumentContext
}

// NewInput acquires an immutable original snapshot and resolved settings. This
// does not read a path, parse Markdown, or choose defaults for unknown rules.
func NewInput(path string, content []byte, settings Settings, dc DocumentContext) (Input, error) {
	if path == "" || !utf8.ValidString(path) {
		return Input{}, fmt.Errorf("logical path must be nonempty UTF-8")
	}
	if dc.Flavor == "" {
		dc.Flavor = "commonmark"
	}
	copySettings := make(Settings, len(settings))
	for id, s := range settings {
		s.Options = cloneOptions(s.Options)
		if s.Enabled != nil {
			v := *s.Enabled
			s.Enabled = &v
		}
		if s.AllowDirectives != nil {
			v := *s.AllowDirectives
			s.AllowDirectives = &v
		}
		copySettings[id] = s
	}
	return Input{path, parser.NewSource(content), copySettings, dc}, nil
}
func (in Input) Path() string          { return in.path }
func (in Input) Source() parser.Source { return in.source }

// RuleContext provides only immutable parser objects, copied options and a
// shared operation meter. Checkpoint charges additional rule/context work and
// observes cancellation; providers must use it for their own bounded loops.
// It must not be retained after the callback returns.
type RuleContext struct {
	input    Input
	index    parser.LineIndex
	document *parser.Document
	options  Options
	budget   *parser.OperationBudget
}

func (c RuleContext) Source() parser.Source      { return c.input.source }
func (c RuleContext) Path() string               { return c.input.path }
func (c RuleContext) Lines() parser.LineIndex    { return c.index }
func (c RuleContext) Document() *parser.Document { return c.document }
func (c RuleContext) Context() DocumentContext   { return c.input.documentContext }
func (c RuleContext) Options() Options           { return cloneOptions(c.options) }
func (c RuleContext) Checkpoint(ctx context.Context, units int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.budget.ChargeWork(units)
}

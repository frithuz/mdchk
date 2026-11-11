package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// TabCharacterItem adds correction facts to one reported TAB. Text means it sits
// in inline paragraph, heading or table-cell text, the only context a correction
// may edit. Oversized means spaces_per_tab exceeds parser.MaxTabSpaces.
type TabCharacterItem struct {
	parser.Span
	Text, Oversized, FixEligible bool
}

// TabCharacterFacts returns the reported TABs of a completed parse from this
// operation for a resolved MD010 plan, with resolved spaces_per_tab. The
// correction producer and checker use it on each round's own parse,
// independently of any analysis fix flag.
func TabCharacterFacts(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]TabCharacterItem, int, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, errors.New("nil tab character context")
	}
	rule, ok := plan.rules["MD010"]
	if !ok {
		return nil, 0, parser.ErrModel
	}
	spaces, code, err := tabCharacterOptions(rule.Options)
	if err != nil {
		return nil, 0, err
	}
	items, err := tabCharacterItems(ctx, parsed.Document, budget, index, plan.Context().ParseOptions(), spaces, code)
	return items, spaces, err
}

func tabCharacterOptions(options Options) (int, bool, error) {
	spaces, ok := options["spaces_per_tab"].(int)
	code, typed := options["code_blocks"].(bool)
	if !ok || !typed || spaces < 1 {
		return 0, false, parser.ErrModel
	}
	return spaces, code, nil
}

func tabCharacterItems(ctx context.Context, document *parser.Document, budget *parser.OperationBudget, index parser.LineIndex, options parser.ParseOptions, spaces int, code bool) ([]TabCharacterItem, error) {
	tabs, err := document.TabCharacters(ctx, budget, index, options, code)
	if err != nil {
		return nil, err
	}
	text, err := document.InlineTextTabs(ctx, budget, index, options)
	if err != nil {
		return nil, err
	}
	if err := budget.ChargeWork(int64(len(tabs)+len(text)) + 1); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(len(tabs))); err != nil {
		return nil, err
	}
	out := make([]TabCharacterItem, len(tabs))
	j := 0
	for i, tab := range tabs {
		for j < len(text) && text[j].Start() < tab.Start() {
			j++
		}
		inText := j < len(text) && text[j].Start() == tab.Start()
		out[i] = TabCharacterItem{Span: tab, Text: inText, Oversized: inText && spaces > parser.MaxTabSpaces}
	}
	return out, nil
}

// TabCharacters borrows admitted scalar options and returns detached
// original TAB violations under both caller and originating controls.
// Serves MD010.
func (c RuleContext) TabCharacters(ctx context.Context) (out []TabCharacterItem, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	spaces, code, err := tabCharacterOptions(c.options)
	if err != nil {
		return nil, err
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	out, err = tabCharacterItems(ruleOperation{ctx, c.operation}, c.document, c.budget, c.index, c.Context().ParseOptions(), spaces, code)
	if err != nil {
		return nil, err
	}
	for i := range out {
		eligible, err := c.tabFixEligible(ctx, out[i])
		if err != nil {
			return nil, err
		}
		out[i].FixEligible = eligible
	}
	return out, nil
}

// tabFixEligible resolves one TAB from retained facts and directive intervals
// only. Hidden and observational findings are never eligible, and neither is any
// TAB in a document opening with an unrecognized block.
func (c RuleContext) tabFixEligible(ctx context.Context, item TabCharacterItem) (bool, error) {
	if c.observational || !c.syntaxReliable || !c.directives.complete || c.document.UnrecognizedBlock() || !item.Text || item.Oversized {
		return false, nil
	}
	if err := c.auditCharge(ctx, directiveLookupWork(len(c.directives.intervals["MD010"]))+1, 0); err != nil {
		return false, err
	}
	state, err := c.directives.anchorState("MD010", item.Start())
	if err != nil {
		return false, err
	}
	return state.Enabled, nil
}

package parser

import (
	"context"
	"errors"
	"slices"
)

// TaskSource retains one actual task paragraph's marker facts and original
// visible contributions. It is owned data; Paragraph in Properties identifies
// the original paragraph envelope, independently of its logical Content.
type TaskSource struct {
	Properties TaskProperties
	Content    []Segment
}

// TaskSources collects source-ordered task paragraphs from a completed parse in
// this SAME operation. Core parsing has an affirmatively empty inventory. Model
// construction, partial parses and equal bytes in a foreign operation grant no
// authority. Traversal, stack storage and returned records share the operation.
func (d *Document) TaskSources(ctx context.Context, budget *OperationBudget) ([]TaskSource, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil task-source context or budget")
	}
	if d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("task sources require a completed parse in this operation")
	}
	if err := budget.ChargeNodes(1); err != nil {
		return nil, err
	}
	out := []TaskSource{}
	stack := []*Node{d.root}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := int64(1 + len(n.spec.Children) + len(n.htmlTail))
		if err := budget.ChargeWork(count); err != nil {
			return nil, err
		}
		if err := budget.ChargeNodes(count); err != nil {
			return nil, err
		}
		if n.Kind() == Paragraph && n.spec.Properties.Task != nil {
			task := *n.spec.Properties.Task
			records := int64(len(task.Segments) + len(n.spec.Content) + 1)
			if err := budget.ChargeWork(records*4 + 16); err != nil {
				return nil, err
			}
			if err := budget.ChargeNodes(records); err != nil {
				return nil, err
			}
			task.Segments = slices.Clone(task.Segments)
			out = append(out, TaskSource{task, slices.Clone(n.spec.Content)})
		}
		// Reverse append preserves source order without sorting or recursive descent.
		for i := len(n.htmlTail) - 1; i >= 0; i-- {
			stack = append(stack, n.htmlTail[i])
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.ChargeWork(0); err != nil {
		return nil, err
	}
	return out, nil
}

//go:build mdchk_conformance

package main

import (
	"context"
	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func execute(content []byte, scope string) (conformance.Result, any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), parser.DefaultLimits().OperationTime)
	defer cancel()
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		return conformance.Result{}, nil, err
	}
	parsed, err := parser.ParseConformance(ctx, parser.NewSource(content), budget, scope)
	if err != nil {
		return conformance.Result{}, nil, err
	}
	actual := conformance.EvaluateActual(ctx, parsed, budget)
	return actual.Result, actual, nil
}

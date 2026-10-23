//go:build !mdchk_conformance

package main

import (
	"context"
	"fmt"
	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func execute(content []byte, scope string) (conformance.Result, any, error) {
	if scope != "core" {
		return conformance.Result{}, nil, fmt.Errorf("scope requires mdchk_conformance test build")
	}
	r := conformance.Evaluate(parser.Parse(context.Background(), parser.NewSource(content)))
	return r, r, nil
}

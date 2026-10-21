package config

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// ApplyAdmittedBounded reserves semantic traversal and owned policy views from
// the caller's shared budget before validation and provider callbacks. Expanded
// alias values are visited individually, rather than estimating from file size.
func (s PolicyState) ApplyAdmittedBounded(ctx context.Context, budget *parser.OperationBudget, d *Definition, registry *engine.Registry, providers engine.ValidationProviders, c Capabilities) (PolicyState, *Resolution, error) {
	providers, err := reserveDefinition(ctx, budget, d, providers, 65536+8*s.SnapshotWork())
	if err != nil {
		return s, nil, err
	}
	charge := func(n int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(n)
	}
	if represent := c.RepresentIndexFile; represent != nil {
		c.RepresentIndexFile = func(name string) error {
			if err := charge(int64(len(name) + 1)); err != nil {
				return err
			}
			err := represent(name)
			if err != nil {
				return err
			}
			return charge(0)
		}
	}
	next, resolution, err := s.ApplyAdmitted(d, registry, providers, c)
	if err != nil {
		return s, nil, err
	}
	if err = charge(0); err != nil {
		return s, nil, err
	}
	return next, resolution, nil
}

func reserveDefinition(ctx context.Context, budget *parser.OperationBudget, d *Definition, providers engine.ValidationProviders, overhead int64) (engine.ValidationProviders, error) {
	charge := func(n int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(n)
	}
	if err := charge(overhead); err != nil {
		return providers, err
	}
	var reserve func(LocatedValue) error
	reserve = func(v LocatedValue) error {
		if v.node == nil {
			return nil
		}
		if err := charge(128 * int64(1+len(v.node.text)+len(v.node.spelling)+len(v.node.origin.PropertyPath)+len(v.node.entries)+len(v.node.elements))); err != nil {
			return err
		}
		for _, entry := range v.node.entries {
			if err := charge(128 * int64(len(entry.Key)+1)); err != nil {
				return err
			}
			if err := reserve(entry.Value); err != nil {
				return err
			}
		}
		for _, element := range v.node.elements {
			if err := reserve(element); err != nil {
				return err
			}
		}
		return nil
	}
	if d != nil {
		if err := reserve(d.Syntax()); err != nil {
			return providers, err
		}
	}
	if provider := providers.ValidateHostname; provider != nil {
		providers.ValidateHostname = func(host string) error {
			if err := charge(128 * int64(len(host)+1)); err != nil {
				return err
			}
			err := provider(host)
			if err != nil {
				return err
			}
			return charge(0)
		}
	}

	return providers, nil
}

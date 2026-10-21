package configscan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// ValidationOutcome describes a successfully acquired selected definition. An
// invalid definition has a source identity and error, but no usable resolution.
type ValidationOutcome struct {
	Source     config.SourceSnapshot
	Resolution *config.Resolution
	Err        error
}

// ValidationResult retains traversal completeness independently of validity.
// Definitions, source identities and configurations are owned operation views.
type ValidationResult struct {
	Scan     Result
	Outcomes []ValidationOutcome
}

// Validate scans only beneath Root. Every eligible definition is independently
// selected and admitted before its exclusions or permissions can affect children.
// Ancestor rule settings and Markdown flavors never merge into children.
func Validate(ctx context.Context, options Options, registry *engine.Registry, providers engine.ValidationProviders, capabilities config.Capabilities) ValidationResult {
	return validate(ctx, options, registry, providers, capabilities, nil)
}
func validate(ctx context.Context, options Options, registry *engine.Registry, providers engine.ValidationProviders, capabilities config.Capabilities, wrap func(scanAccess) scanAccess) ValidationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Budget == nil {
		options.Budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	}
	result := ValidationResult{}
	hooks := Hooks{}
	hooks.Exclude = func(path string, directory bool, state config.PolicyState) (Skip, bool, error) {
		if err := ctx.Err(); err != nil {
			return Skip{}, false, err
		}
		// Reserve origin/matcher snapshots before copying inherited state.
		if err := options.Budget.ChargeWork(state.SnapshotWork()); err != nil {
			return Skip{}, false, err
		}
		decision := Skip{Path: path, Reason: "configuration exclusion"}
		for _, scope := range state.Exclusions() {
			relative, err := filepath.Rel(filepath.Dir(scope.Origin.Path), path)
			if err != nil {
				return Skip{}, false, err
			}
			inBase := relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
			if relative == "." {
				relative = ""
			}
			// A bounded matcher meters each state and standard-library match before use.
			match, err := scope.Patterns.ExcludesBounded(ctx, options.Budget, filepath.ToSlash(relative), directory, inBase)
			if err != nil {
				if errors.Is(err, config.ErrPatternLimit) {
					err = &parser.LimitExceeded{Name: "configuration-pattern-work", Observed: config.MaxPatternMatchWork + 1, Allowed: config.MaxPatternMatchWork}
				}
				return Skip{}, false, err
			}
			if match {
				line := 0
				if scope.Origin.Position != nil {
					line = scope.Origin.Position.Line
				}
				if decision.Source == "" {
					decision.Source = scope.Origin.Path
					decision.Line = line
				}
				decision.Origins = append(decision.Origins, scope.Origin)
			}
		}
		return decision, len(decision.Origins) > 0, nil
	}
	hooks.Definition = func(d Definition, state config.PolicyState) (config.PolicyState, error) {
		sum := sha256.Sum256(d.Data)
		outcome := ValidationOutcome{Source: config.SourceSnapshot{Path: d.Path, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(d.Data))}}
		decoded, err := config.DecodeYAMLBounded(ctx, d.Path, d.Data, options.Budget)
		next := state
		if err == nil {
			next, outcome.Resolution, err = state.ApplyAdmittedBounded(ctx, options.Budget, decoded, registry, providers, capabilities)
		}
		if err == nil {
			err = ctx.Err()
			if err == nil {
				err = options.Budget.ChargeWork(0)
			}
		}
		if err != nil {
			next = state
			outcome.Resolution = nil
		}
		outcome.Err = err
		result.Outcomes = append(result.Outcomes, outcome)
		return next, err
	}
	result.Scan = scan(ctx, options, hooks, wrap)
	return result
}

// IsDefinitionFailure distinguishes configuration acquisition/validation from
// Git source or traversal IO without guessing from the file's basename.
func IsDefinitionFailure(err error) bool {
	var failure Failure
	return errors.As(err, &failure) && failure.Role == DefinitionSource
}

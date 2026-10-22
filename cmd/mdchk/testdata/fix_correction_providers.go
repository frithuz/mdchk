// Compiled test-only correction fixture. The real dispatcher, acquisition,
// validator, diff and reporters are used unchanged.
package cli

import (
	"bytes"
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"strings"
)

func configuredCorrections(registry *engine.Registry) (*fix.Validator, fix.Producer, error) {
	v, err := fix.NewValidator(registry, []fix.CheckerRegistration{{Rule: "MD001", Check: func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		a, b := c.Before.Current().Bytes(), c.After.Current().Bytes()
		if err := c.Before.Checkpoint(ctx, int64(len(a)+len(b))+1); err != nil {
			return fix.Unknown, err
		}
		if bytes.Contains(a, []byte("unsafe-fixture")) {
			return fix.Unsafe, nil
		}
		if bytes.Equal(bytes.TrimLeft(a, "\r\n"), bytes.TrimLeft(b, "\r\n")) {
			return fix.Preserved, nil
		}
		return fix.Unsafe, nil
	}}})
	produce := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		source := c.Current()
		text := string(source.Bytes())
		if err := c.Checkpoint(ctx, int64(len(text))+1); err != nil {
			return nil, err
		}
		if strings.Contains(text, "late-internal-fixture") && strings.HasPrefix(text, "\n") {
			return nil, errors.New("synthetic later producer failure")
		}
		if strings.Contains(text, "late-cancel-fixture") && strings.HasPrefix(text, "\n") {
			return nil, context.Canceled
		}
		if strings.Contains(text, "cancel-fixture") && !strings.Contains(text, "late-cancel-fixture") {
			return nil, context.Canceled
		}
		if strings.Contains(text, "late-quota-fixture") && strings.HasPrefix(text, "\n") {
			return nil, c.Checkpoint(ctx, 250000000)
		}
		end := 0
		replacement := "\n"
		if strings.HasPrefix(text, "\n") {
			if !strings.Contains(text, "cycle-fixture") && (!strings.Contains(text, "two-round-fixture") || strings.HasPrefix(text, "\n\n")) {
				return nil, nil
			}
			if !strings.Contains(text, "two-round-fixture") {
				end = 1
				replacement = ""
			}
		}
		sp, _ := source.Span(0, end)
		expected, _ := source.Text(sp)
		group := fix.Group{ID: "boundary", Rule: "MD001", Provenance: "compiled synthetic fixture", Anchor: sp, Edits: []fix.Edit{{Span: sp, Expected: []byte(expected), Replacement: []byte(replacement)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}
		if strings.Contains(text, "conflict-fixture") {
			other := group
			other.ID = "other"
			other.Edits = []fix.Edit{{Span: sp, Expected: []byte(expected), Replacement: []byte("\r")}}
			return []fix.Group{group, other}, nil
		}
		if strings.Contains(text, "duplicate-fixture") {
			other := group
			other.ID = "shared-duplicate"
			return []fix.Group{group, other}, nil
		}
		return []fix.Group{group}, nil
	}
	return v, produce, err
}

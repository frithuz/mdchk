package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// HeadingOutlineEntry is one owned descriptor in MD043's literal mini-language.
// Text borrows immutable string bytes; the returned entry slice is a snapshot.
type HeadingOutlineEntry struct {
	Level int
	Text  string
	Star  bool
}

func headingOutlineEntry(value string, checkpoint func(int64) error) (HeadingOutlineEntry, error) {
	for start := 0; start < len(value); start += 256 {
		end := start + min(256, len(value)-start)
		if checkpoint != nil {
			if err := checkpoint(int64(end-start) + 1); err != nil {
				return HeadingOutlineEntry{}, err
			}
		}
		for i := start; i < end; i++ {
			if value[i] == '\r' || value[i] == '\n' {
				return HeadingOutlineEntry{}, errors.New("expected a single-line heading descriptor")
			}
		}
	}
	if value == "*" {
		return HeadingOutlineEntry{Star: true}, nil
	}
	level := 0
	for level < len(value) && value[level] == '#' && level < 7 {
		level++
	}
	if level < 1 || level > 6 || level >= len(value) || value[level] != ' ' {
		return HeadingOutlineEntry{}, errors.New("expected '*' or 1–6 hashes followed by a space and literal text")
	}
	return HeadingOutlineEntry{Level: level, Text: value[level+1:]}, nil
}

// HeadingOutline compiles an independent descriptor slice only after reserving
// its storage in the actual completed source operation. It avoids Options' eager
// uncharged list clone and never exposes mutable internal option storage.
func (c RuleContext) HeadingOutline(ctx context.Context) (out []HeadingOutlineEntry, err error) {
	if err := c.HeadingOutlineCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.HeadingOutlineCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	values, ok := c.options["headings"].([]string)
	if !ok {
		return nil, parser.ErrModel
	}
	if err := c.HeadingOutlineCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	out = make([]HeadingOutlineEntry, len(values))
	for i, value := range values {
		if err := c.HeadingOutlineCheckpoint(ctx, 8, 0); err != nil {
			return nil, err
		}
		out[i], err = headingOutlineEntry(value, func(work int64) error {
			return c.HeadingOutlineCheckpoint(ctx, work, 0)
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// HeadingOutlineCheckpoint reserves MD043 comparison work and records without
// exposing a budget. Even an empty outline must retain actual parse authority.
func (c RuleContext) HeadingOutlineCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return c.document.HeadingOutlineCheckpoint(ruleOperation{ctx, c.operation}, c.budget, c.index, work, nodes)
}

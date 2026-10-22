package fix

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
)

// InsertionEnding chooses the nearest complete original physical terminator to
// a scalar insertion boundary. Distance is to the nearest edge; preceding wins
// ties. CRLF is indivisible. EOF therefore uses the last prior ending; an input
// with no ending uses LF. Every traversal uses the caller's residual operation.
func InsertionEnding(ctx context.Context, source parser.Source, lines parser.LineIndex, at int, checkpoint func(context.Context, int64) error) ([]byte, error) {
	if err := checkpoint(ctx, 1); err != nil {
		return nil, err
	}
	if lines.Source() != source {
		return nil, parser.ErrRange
	}
	all, _ := source.Span(0, source.Len())
	text, _ := source.Text(all)
	if !boundary(text, at) {
		return nil, parser.ErrPosition
	}
	bestDistance := source.Len() + 1
	chosen := "\n"
	for i := 1; i <= lines.LineCount(); i++ {
		if err := checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		line, err := lines.Line(i)
		if err != nil {
			return nil, err
		}
		end := line.Ending
		if end.Start() == end.End() {
			continue
		}
		distance := end.Start() - at
		if end.End() <= at {
			distance = at - end.End()
		}
		if distance < bestDistance {
			bestDistance = distance
			chosen, _ = source.Text(end)
		}
	}
	if err := checkpoint(ctx, int64(len(chosen))+1); err != nil {
		return nil, err
	}
	return []byte(chosen), nil
}

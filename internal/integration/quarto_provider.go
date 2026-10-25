package integration

import (
	"context"
	"errors"
	"slices"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// This marker is private to a single synchronous provider assembly. Other
// policy/slide work on the immutable scope remains independent of service Close.
type quartoAssemblyKey struct{}

// WithQuarto returns a new immutable scope bound to this operation's admitted
// parse service. Obtain Documents from the returned scope; documents from the
// previous policy-only scope cannot borrow its new producer binding. Binding
// adds no worker, cache, root, allowance or advertised CLI/MCP capability.
func (s *Scope) WithQuarto(ctx context.Context, service *resource.Service) (*Scope, error) {
	if s == nil || ctx == nil || !service.OwnsOperation(s.operation) {
		return nil, ErrContract
	}
	if err := service.Checkpoint(ctx, 1); err != nil {
		return nil, err
	}
	if err := s.operation.Budget().ChargeNodes(1); err != nil {
		return nil, err
	}
	out := *s
	out.quartoService = service
	return &out, nil
}

// QuartoProblem describes located interpretation uncertainty or an actual parser
// failure, not a rule diagnostic. Runtime stop errors are returned separately.
type QuartoProblem struct {
	Location Location
	Message  string
	Failure  parser.FailureCode
}

func (c QuartoContext) Problems(ctx context.Context) ([]QuartoProblem, error) {
	if c.document.scope == nil || c.status == "" {
		return nil, ErrContract
	}
	work := int64(len(c.problems)) + 1
	for _, problem := range c.problems {
		work += int64(len(problem.Message))
	}
	if err := c.document.scope.checkpoint(ctx, work, int64(len(c.problems))); err != nil {
		return nil, err
	}
	return slices.Clone(c.problems), nil
}

// Parsed retains already admitted parse facts independently of whether this
// cell query completed. It neither grants anchors nor changes Source/view.
func (c QuartoContext) Parsed() (resource.TargetParseResult, bool) {
	return c.parsed, c.parsed.Source().Token() != (parser.SourceToken{})
}

// Complete states that executable-cell and label interpretation completed.
// Known reliable Cells/declared labels may remain useful when this is false.
func (c QuartoContext) Complete() bool { return c.status == Available }

// KnownCellCount and KnownCell expose already established immutable records
// without allocating or rescanning after a later stop. Their presence does not
// establish collection completeness; callers still honor Status/Complete and
// meter any new analysis of the returned source/text.
func (c QuartoContext) KnownCellCount() int { return len(c.cells) }
func (c QuartoContext) KnownCell(index int) (Cell, bool) {
	if index < 0 || index >= len(c.cells) {
		return Cell{}, false
	}
	return c.cells[index], true
}

func (c Cell) Document() Document { return c.document }
func (c Cell) Status() Status     { return c.status }
func (c Cell) Info() Location     { return c.info }
func (c Cell) Fence() (byte, int) { return c.fence, c.width }
func (c Cell) Content(ctx context.Context) (parser.Value, error) {
	if c.document.scope == nil {
		return parser.Value{}, ErrContract
	}
	if err := c.document.scope.checkpoint(ctx, int64(len(c.content.Segments))+1, int64(len(c.content.Segments))); err != nil {
		return parser.Value{}, err
	}
	out := c.content
	out.Segments = slices.Clone(c.content.Segments)
	return out, nil
}
func (c Cell) Markers(ctx context.Context) ([]parser.Marker, error) {
	if c.document.scope == nil {
		return nil, ErrContract
	}
	if err := c.document.scope.checkpoint(ctx, int64(len(c.markers))+1, int64(len(c.markers))); err != nil {
		return nil, err
	}
	return slices.Clone(c.markers), nil
}
func (c Cell) Containers(ctx context.Context) ([]parser.HeadingContainer, error) {
	if c.document.scope == nil {
		return nil, ErrContract
	}
	if err := c.document.scope.checkpoint(ctx, int64(len(c.containers))+1, int64(len(c.containers))); err != nil {
		return nil, err
	}
	return slices.Clone(c.containers), nil
}

func (s *Scope) produceQuarto(ctx context.Context, document Document) (out QuartoContext, err error) {
	out = QuartoContext{document: document, status: Unavailable}
	ctx = context.WithValue(ctx, quartoAssemblyKey{}, s)
	// The final checkpoint retains independent Close/caller/op/quota causes even
	// when a bounded projection or option decoder observed one of them first.
	defer func() {
		if err != nil {
			out.status = Unavailable
		}
		if stopped := s.checkpoint(ctx, 0, 0); stopped != nil {
			err = errors.Join(err, stopped)
			out.status = Unavailable
		}
	}()
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return out, err
	}
	if !document.available {
		return out, nil
	}
	parsed, parseErr := s.quartoService.TargetParse(ctx, s.view, document.id)
	out.parsed = parsed
	if parsed.Source().Token() != document.source.Token() {
		if parseErr == nil {
			return QuartoContext{}, ErrContract
		}
		return out, parseErr
	}
	result, attempted := parsed.Parsed()
	if !attempted {
		return out, parseErr
	}
	if result.Failure != nil {
		location := Location{}
		if result.Failure.Location.Present() {
			location, _ = NewLocation(document.source, result.Failure.Location)
		}
		out.problems = []QuartoProblem{{Location: location, Message: result.Failure.Message, Failure: result.Failure.Code}}
		return out, parseErr
	}
	if result.Status != parser.Complete || result.Document == nil {
		for _, uncertainty := range result.Uncertainty {
			if err := s.checkpoint(ctx, int64(len(uncertainty.Reason))+1, 1); err != nil {
				return out, errors.Join(parseErr, err)
			}
			location, _ := NewLocation(document.source, uncertainty.Region)
			out.problems = append(out.problems, QuartoProblem{Location: location, Message: string(uncertainty.Reason)})
		}
		return out, parseErr
	}
	if parseErr != nil {
		return out, parseErr // a completed parse is not a completed new cell query
	}
	fences, err := result.Document.FencedBlocks(ctx, s.operation.Budget())
	if err != nil {
		return out, err
	}
	out.status = Available
	for _, fence := range fences {
		if !fence.RawInfo.Present() {
			continue
		}
		location, err := NewLocation(document.source, fence.RawInfo)
		if err != nil {
			return out, err
		}
		header, err := s.QuartoHeader(ctx, document, location, false)
		if err != nil {
			return out, err
		}
		if header.class == OrdinaryFence || header.class == EscapedExample {
			continue
		}
		if err := s.checkpoint(ctx, int64(len(header.declared)+len(fence.Content.Segments)+len(fence.Markers)+len(fence.Containers))+129, 2); err != nil {
			return out, err
		}
		envelope, _ := NewLocation(document.source, fence.Envelope)
		cell := Cell{document: document, envelope: envelope, language: header.language,
			info: location, fence: fence.Fence, width: fence.Width, content: fence.Content,
			markers: fence.Markers, containers: fence.Containers, status: Available}
		options, err := decodeQuartoOptions(ctx, s, document, fence.Content)
		if err != nil {
			var failure *quartoOptionsInternalError
			if errors.As(err, &failure) {
				// This one fixed-size error record was reserved before decoding.
				out.problems = append(out.problems, QuartoProblem{Location: envelope, Message: failure.Error(), Failure: parser.InternalFailure})
			}
			return out, err
		}
		cell.labels, err = s.ResolveLabels(ctx, document, header.label, options.label)
		if err != nil {
			return out, err
		}
		if err := s.checkpoint(ctx, int64(len(header.declared)+len(options.declared))+1, int64(len(header.declared)+len(options.declared))); err != nil {
			return out, err
		}
		cell.declared = append(slices.Clone(header.declared), options.declared...)
		for _, issue := range options.issues {
			if err := s.checkpoint(ctx, int64(len(issue.message))+1, 1); err != nil {
				return out, err
			}
			out.problems = append(out.problems, QuartoProblem{Location: issue.location, Message: issue.message})
		}
		if header.class == UnsupportedCell {
			if err := s.checkpoint(ctx, 64, 1); err != nil {
				return out, err
			}
			out.problems = append(out.problems, QuartoProblem{Location: location, Message: "Executable header interpretation is unavailable."})
		}
		if cell.labels.status == Unavailable && len(options.issues) == 0 && header.class == SupportedCandidate {
			if err := s.checkpoint(ctx, 64, 1); err != nil {
				return out, err
			}
			labelLocation := cell.labels.effective.location
			if !labelLocation.valid() {
				labelLocation = location
			}
			out.problems = append(out.problems, QuartoProblem{Location: labelLocation, Message: "Effective label interpretation is unavailable."})
		}
		if header.class != SupportedCandidate || cell.labels.status != Available || !cell.labels.DeclarationsComplete() || len(options.issues) != 0 {
			cell.status = Unavailable
			out.status = Unavailable
		}
		out.cells = append(out.cells, cell)
	}
	return out, nil
}

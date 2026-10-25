package resource

import (
	"errors"
	"fmt"
	"slices"

	"github.com/frithuz/mdchk/internal/result"
)

// FilesystemFailure retains the additional error returned alongside a recorded
// native observation. Reader, Request and its observation must share one exact
// operation/view. Different readers from that operation may share a report.
type FilesystemFailure struct {
	Reader  *FilesystemReader
	Request Request
	Err     error
}

// FilesystemReport owns a result's resource projection lifecycle. Construct one
// per logical report, append complete view batches, then Finish before calling
// the shared finalizer. It retains no caller Result pointer or reader-local state.
// Mutation is serialized by the caller. Do not mutate the transferred initial
// result or its slices while this owner is active. Copies share private state
// and the same consuming guard; the zero value is invalid.
type FilesystemReport struct{ state *filesystemReportState }

type filesystemReportState struct {
	operation *Operation
	out       result.Result
	finished  bool
	used      map[string]bool
	ordinals  map[string]int
	io, stops map[string]string
}

// NewFilesystemReport takes ownership of initial. Its existing causes occupy IDs
// but are not guessed to be native resource causes from reporting-path spelling.
// New resource causes and source coverage are owned by this explicit lifecycle.
func NewFilesystemReport(operation *Operation, initial result.Result) (*FilesystemReport, error) {
	if operation == nil || operation.ctx == nil || operation.budget == nil || operation.root.identity == nil {
		return nil, ErrContract
	}
	p := &filesystemReportState{operation: operation, out: initial, used: map[string]bool{}, ordinals: map[string]int{}, io: map[string]string{}, stops: map[string]string{}}
	for _, item := range initial.Errors {
		p.used[item.ID] = true
	}
	for _, item := range initial.Interruptions {
		p.used[item.ID] = true
	}
	return &FilesystemReport{state: p}, nil
}

// Append admits a complete view batch, then projects primary and additional
// causes together. All entries are validated before mutation. Each observation's
// coverage is indexed only within this batch; shared cause IDs persist across
// subsequent original/candidate/saved batches without rescanning growing output.
func (owner *FilesystemReport) Append(view View, observations []Observation, failures []FilesystemFailure) error {
	if owner == nil || owner.state == nil {
		return ErrContract
	}
	p := owner.state
	if p.finished || view.operation != p.operation || view.identity == nil {
		return ErrContract
	}
	seen := make(map[*requestIdentity]bool, len(observations))
	for _, observation := range observations {
		request := observation.request
		if request.identity == nil || !request.view.Same(view) || seen[request.identity] {
			return ErrContract
		}
		seen[request.identity] = true
	}
	for _, failure := range failures {
		if failure.Reader == nil || failure.Reader.operation != p.operation || !failure.Request.view.Same(view) || !seen[failure.Request.identity] {
			return ErrContract
		}
		if failure.Err != nil {
			fault := filesystemFault(failure.Err)
			if fault == nil || fault.IO == nil && fault.Stop == nil {
				return ErrContract
			}
			if fault.Stop != nil {
				if _, ok := InterruptionProblem(fault.Stop); !ok {
					return ErrContract
				}
			}
		}
	}
	coverage := make(map[*requestIdentity]int, len(observations))
	for _, observation := range observations {
		request := observation.request
		cause := p.problem(request.Key().String(), observation.problem)
		if observation.Treatment() == ContextIncomplete {
			location, snapshot := request.location, request.snapshot
			item := result.Coverage{Path: request.Link().Path(), Rule: request.Link().Rule(), Range: &location, Snapshot: &snapshot, Reason: result.ContextUnavailable, Message: "Required resource context is unavailable."}
			if cause != "" {
				item.CauseIDs = []string{cause}
			}
			coverage[request.identity] = len(p.out.Coverage)
			p.out.Coverage = append(p.out.Coverage, item)
		}
	}
	for _, failure := range failures {
		if failure.Err == nil {
			continue
		}
		fault := filesystemFault(failure.Err)
		key := failure.Request.Key().String()
		var ids []string
		if fault.IO != nil {
			kind := ReadFailure
			var decode *filesystemDecodeError
			if errors.As(fault.IO, &decode) {
				kind = DecodeFailure
			}
			ids = append(ids, p.problem(key, Problem{Kind: kind, Message: fmt.Sprintf("Resource acquisition failed: %v", fault.IO)}))
		}
		if fault.Stop != nil {
			problem, _ := InterruptionProblem(fault.Stop) // Validated before any mutation.
			ids = append(ids, p.problem(key, problem))
		}
		if index, found := coverage[failure.Request.identity]; found {
			for _, id := range ids {
				if !slices.Contains(p.out.Coverage[index].CauseIDs, id) {
					p.out.Coverage[index].CauseIDs = append(p.out.Coverage[index].CauseIDs, id)
				}
			}
		}
	}
	return nil
}

func filesystemFault(err error) *FilesystemReadError {
	var fault *FilesystemReadError
	if errors.As(err, &fault) {
		return fault
	}
	if _, stop := InterruptionProblem(err); stop {
		return &FilesystemReadError{Stop: err}
	}
	return &FilesystemReadError{IO: err}
}

func (p *filesystemReportState) problem(key string, problem Problem) string {
	identity := key + "\x00" + string(problem.Kind)
	switch problem.Kind {
	case ReadFailure, DecodeFailure, InternalFailure:
		if id := p.io[identity]; id != "" {
			return id
		}
		id := p.next("resource-native-error")
		p.io[identity] = id
		category := result.IO
		if problem.Kind == InternalFailure {
			category = result.Internal
		}
		p.out.Errors = append(p.out.Errors, result.Error{ID: id, Category: category, Code: string(problem.Kind), Scope: "operation", Path: key, Message: problem.Message})
		return id
	case Cancelled, Deadline, Limited:
		if id := p.stops[identity]; id != "" {
			return id
		}
		id := p.next("resource-native-stop")
		p.stops[identity] = id
		p.out.Interruptions = append(p.out.Interruptions, result.Interruption{ID: id, Reason: string(problem.Kind), Phase: "acquisition", Path: key, Message: problem.Message, Limit: copyProblem(problem).Limit})
		return id
	default:
		return ""
	}
}

func (p *filesystemReportState) next(prefix string) string {
	for n := p.ordinals[prefix] + 1; ; n++ {
		id := fmt.Sprintf("%s-%d", prefix, n)
		if !p.used[id] {
			p.used[id] = true
			p.ordinals[prefix] = n
			return id
		}
	}
}

// Finish transfers the result to its caller for ordinary shared finalization.
// The consumed owner, including aliases to it, rejects further Append/Finish.
func (owner *FilesystemReport) Finish() (result.Result, error) {
	if owner == nil || owner.state == nil {
		return result.Result{}, ErrContract
	}
	p := owner.state
	if p.finished {
		return result.Result{}, ErrContract
	}
	p.finished = true
	out := p.out
	p.out = result.Result{}
	return out, nil
}

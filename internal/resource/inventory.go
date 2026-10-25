package resource

import (
	"context"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// SuppliedReader is an immutable, operation-bound logical inventory. Construct
// it from DecodeSupplied's owned input and the same operator limits. All submitted
// files seed resources, independently of the operation's selected target subset.
// Concurrent reads share the operation budget; no observation/miss cache is kept.
// The zero value is invalid. Selection (including all-excluded usage errors),
// parsing, coordinated overlays and result finalization remain caller-owned.
type SuppliedReader struct {
	operation *Operation
	limits    InputLimits
	complete  bool
	entries   map[string]suppliedFact
	selected  map[Key]int
}

type suppliedFact struct {
	kind   EntryKind
	source parser.Source
	seed   bool
}

// NewSuppliedReader publishes only a complete index. Explicit declarations and
// distinct positive inferred parents share Identities; the virtual root is a
// separate constant fact. Known input/count excess is InputError; runtime stops
// retain their original identity. Rebuilding does not reset the shared budget.
func NewSuppliedReader(ctx context.Context, operation *Operation, input SuppliedInput, limits InputLimits) (*SuppliedReader, error) {
	if ctx == nil || operation == nil || operation.root.kind != SuppliedRoot || operation.root.identity == nil || limits.validate(operation.budget.Limits().DocumentBytes) != nil {
		return nil, ErrContract
	}
	r := &SuppliedReader{operation: operation, limits: limits, complete: input.complete}
	if err := r.checkpoint(ctx, 1); err != nil {
		return nil, err
	}
	if err := inputBound("files", "target-files", int64(len(input.files)), limits.TargetFiles); err != nil {
		return nil, err
	}
	if err := inputBound("resources", "resource-identities", int64(len(input.files))+int64(len(input.entries)), limits.Identities); err != nil {
		return nil, err
	}
	if err := operation.budget.ChargeNodes(3); err != nil {
		return nil, err
	}
	r.entries = make(map[string]suppliedFact)
	r.selected = make(map[Key]int)
	total := int64(0)
	add := func(key Key, fact suppliedFact) error {
		if err := r.checkpoint(ctx, int64(len(key.value))+1); err != nil {
			return err
		}
		if err := inputBound("resources", "path-bytes", int64(len(key.value)), limits.PathBytes); err != nil {
			return err
		}
		if err := inputBound("resources", "path-depth", int64(strings.Count(key.value, "/")+1), limits.PathDepth); err != nil {
			return err
		}
		if fact.source != (parser.Source{}) {
			if err := inputBound("resources", "document-bytes", int64(fact.source.Len()), operation.budget.Limits().DocumentBytes); err != nil {
				return err
			}
			total += int64(fact.source.Len())
			if err := inputBound("resources", "total-content-bytes", total, limits.TotalContentBytes); err != nil {
				return err
			}
		}
		if err := operation.budget.ChargeNodes(1); err != nil {
			return err
		}
		r.entries[key.value] = fact
		return nil
	}
	for _, file := range input.files {
		if err := add(file.Key, suppliedFact{kind: EntryFile, source: file.Source, seed: true}); err != nil {
			return nil, err
		}
	}
	for _, entry := range input.entries {
		if err := add(entry.key, suppliedFact{kind: entry.kind, source: entry.source}); err != nil {
			return nil, err
		}
	}
	// Walk immutable declarations, not the map being extended. Negative facts do
	// not establish their own parents; each inferred identity is admitted first.
	infer := func(name string) error {
		for {
			if err := r.checkpoint(ctx, int64(len(name))+1); err != nil {
				return err
			}
			slash := strings.LastIndexByte(name, '/')
			if slash < 0 {
				return nil
			}
			name = name[:slash]
			if _, known := r.entries[name]; known {
				continue
			}
			if err := inputBound("resources", "resource-identities", int64(len(r.entries))+1, limits.Identities); err != nil {
				return err
			}
			if err := operation.budget.ChargeNodes(1); err != nil {
				return err
			}
			r.entries[name] = suppliedFact{kind: EntryDirectory}
		}
	}
	for _, file := range input.files {
		if err := infer(file.Key.value); err != nil {
			return nil, err
		}
	}
	for _, entry := range input.entries {
		if entry.kind == EntryFile || entry.kind == EntryDirectory {
			if err := infer(entry.key.value); err != nil {
				return nil, err
			}
		}
	}
	for i, target := range operation.targets {
		if err := r.checkpoint(ctx, int64(len(target.ResourceKey.value))+int64(target.Source.Len())+1); err != nil {
			return nil, err
		}
		fact, ok := r.entries[target.ResourceKey.value]
		if !ok || !fact.seed || fact.source != target.Source {
			return nil, ErrContract
		}
		if err := operation.budget.ChargeNodes(1); err != nil {
			return nil, err
		}
		r.selected[target.ResourceKey] = i
	}
	if err := r.checkpoint(ctx, 0); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SuppliedReader) checkpoint(ctx context.Context, work int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.operation.Checkpoint(work)
}

// RootDirectory reports the virtual root without fabricating a Key or Request.
// Only a root-directory Location from this exact namespace grants the fact.
// A late stop can accompany Directory; callers retain the independent error.
func (r *SuppliedReader) RootDirectory(ctx context.Context, location Location) (Existence, error) {
	if r == nil || r.operation == nil || ctx == nil || !location.IsRootDirectory() || location.root != r.operation.root {
		return Unknown, ErrContract
	}
	if err := r.checkpoint(ctx, 1); err != nil {
		return Unknown, err
	}
	return Directory, r.checkpoint(ctx, 0)
}

// Read derives facts for an already-normalized request. It never touches a host
// filesystem, decodes a name, parses Markdown or selects a target. An error is
// an independent runtime stop and MUST be retained with SuppliedStop when
// projecting effects, even when the returned known fact satisfies the request.
func (r *SuppliedReader) Read(ctx context.Context, request Request) (ObservationSpec, error) {
	if r == nil || r.operation == nil || ctx == nil || request.view.operation != r.operation || request.view.identity == nil || request.identity == nil {
		return ObservationSpec{}, ErrContract
	}
	switch request.Class() {
	case Ignored:
		return ObservationSpec{Existence: Unknown, Content: NotRequested, Anchors: NotRequested}, r.checkpoint(ctx, 0)
	case BlockedReference:
		return suppliedUnavailable(Blocked, "Blocked local reference."), r.checkpoint(ctx, 0)
	case InvalidReference:
		return suppliedUnavailable(MalformedReference, "Invalid local reference."), r.checkpoint(ctx, 0)
	case Local:
	default:
		return ObservationSpec{}, ErrContract
	}
	if err := r.checkpoint(ctx, int64(len(request.Key().value))+1); err != nil {
		return suppliedStopped(Unknown, err), err
	}
	key := request.Key()
	if err := normalizationBound("path-bytes", int64(len(key.value)), r.limits.PathBytes); err != nil {
		return suppliedStopped(Unknown, err), err
	}
	if err := normalizationBound("path-depth", int64(strings.Count(key.value, "/")+1), r.limits.PathDepth); err != nil {
		return suppliedStopped(Unknown, err), err
	}
	gitMetadata := false
	for part := range strings.SplitSeq(key.value, "/") {
		gitMetadata = gitMetadata || part == ".git"
	}
	var fact suppliedFact
	selected := false
	if i, ok := r.selected[key]; ok {
		state := request.view.targets[i]
		if gitMetadata && state.target.ID != request.Link().Path() {
			return r.finish(ctx, suppliedUnavailable(Blocked, "Git metadata is not a resource."))
		}
		if state.unavailable {
			return r.finish(ctx, suppliedUnavailable(Blocked, "Selected saved resource is unavailable."))
		}
		fact, selected = suppliedFact{kind: EntryFile, source: state.target.Source}, true
	} else {
		if gitMetadata {
			return r.finish(ctx, suppliedUnavailable(Blocked, "Git metadata is not a resource."))
		}
		var err error
		fact, err = r.lookup(ctx, key.value)
		if err != nil {
			return suppliedStopped(Unknown, err), err
		}
	}
	switch fact.kind {
	case EntryDirectory:
		return r.finish(ctx, ObservationSpec{Existence: Directory, Content: Inapplicable, Anchors: Inapplicable})
	case EntryMissing:
		return r.finish(ctx, ObservationSpec{Existence: Missing, Content: Inapplicable, Anchors: Inapplicable})
	case EntryUnavailable:
		return r.finish(ctx, suppliedUnavailable(NotSupplied, "Supplied subtree is unavailable."))
	case EntryFile:
	default:
		return r.finish(ctx, suppliedUnavailable(PartialInventory, "Resource name is not supplied in this partial inventory."))
	}
	out := ObservationSpec{Existence: File, Content: NotRequested, Anchors: NotRequested}
	if request.Capability() == ExistenceOnly || request.RequiredType() == DirectoryType {
		return r.finish(ctx, out)
	}
	if request.Capability() == ParsedAnchors && !selected && !markdownResourceKey(key) {
		out.Anchors = Unavailable
		out.Problem = Problem{Kind: NotSupplied, Message: "Anchor interpretation is unavailable for this resource type."}
		return r.finish(ctx, out)
	}
	if fact.source == (parser.Source{}) {
		out.Content, out.Anchors = Unavailable, Unavailable
		out.Problem = Problem{Kind: NotSupplied, Message: "Resource content was not supplied."}
		return r.finish(ctx, out)
	}
	if err := normalizationBound("document-bytes", int64(fact.source.Len()), r.operation.budget.Limits().DocumentBytes); err != nil {
		return suppliedStopped(File, err), err
	}
	evidence, err := r.operation.ValidateContent(fact.source)
	if err != nil {
		return suppliedStopped(File, err), err
	}
	out.Content, out.Source, out.Validated = Available, fact.source, evidence
	return r.finish(ctx, out)
}

func (r *SuppliedReader) lookup(ctx context.Context, name string) (suppliedFact, error) {
	if fact, ok := r.entries[name]; ok {
		return fact, nil
	}
	parent := name
	for {
		if err := r.checkpoint(ctx, int64(len(parent))+1); err != nil {
			return suppliedFact{}, err
		}
		slash := strings.LastIndexByte(parent, '/')
		if slash < 0 {
			break
		}
		parent = parent[:slash]
		if fact, ok := r.entries[parent]; ok {
			switch fact.kind {
			case EntryMissing, EntryFile:
				return suppliedFact{kind: EntryMissing}, nil
			case EntryUnavailable:
				return fact, nil
			case EntryDirectory:
				// A known directory does not establish this child's name.
			}
		}
	}
	if r.complete {
		return suppliedFact{kind: EntryMissing}, nil
	}
	return suppliedFact{}, nil
}

func (r *SuppliedReader) finish(ctx context.Context, out ObservationSpec) (ObservationSpec, error) {
	// A stop cannot be put into a known Directory/Missing or already acquired
	// content spec. Return it independently; AppendEffects retains both facts.
	return out, r.checkpoint(ctx, 0)
}

func suppliedUnavailable(kind ProblemKind, message string) ObservationSpec {
	return ObservationSpec{Existence: Unknown, Content: Unavailable, Anchors: Unavailable, Problem: Problem{Kind: kind, Message: message}}
}

func suppliedStopped(existence Existence, err error) ObservationSpec {
	problem, _ := InterruptionProblem(err)
	return ObservationSpec{Existence: existence, Content: Interrupted, Anchors: NotRequested, Problem: problem}
}

// SuppliedStop carries the independent controlled error from Read alongside its
// recorded observation. It grants no additional lookup/target authority.
type SuppliedStop struct {
	Request Request
	Err     error
}

// AppendEffects validates a complete view batch before mutating out, then emits
// its context deficits and actual stops together. Call once per complete batch.
// It keeps no Result pointer or mutable reader projection state: different
// readers/views and fresh results may interleave. Each batch scans existing IDs
// once; it does not repeatedly rescan growing output per error. Caller serializes
// result mutation and retains ownership of its result and slices throughout.
func (r *SuppliedReader) AppendEffects(out *result.Result, view View, observations []Observation, stops []SuppliedStop) error {
	if r == nil || r.operation == nil || view.operation != r.operation || view.identity == nil || out == nil {
		return ErrContract
	}
	seen := make(map[*requestIdentity]bool, len(observations))
	for _, observation := range observations {
		if !observation.request.view.Same(view) || observation.request.identity == nil || seen[observation.request.identity] {
			return ErrContract
		}
		seen[observation.request.identity] = true
	}
	additional := make(map[*requestIdentity]Problem, len(stops))
	for _, stop := range stops {
		request := stop.Request
		problem, controlled := InterruptionProblem(stop.Err)
		if !controlled || !request.view.Same(view) || !seen[request.identity] {
			return ErrContract
		}
		if _, duplicate := additional[request.identity]; duplicate {
			return ErrContract
		}
		additional[request.identity] = problem
	}
	return view.appendEffects(out, observations, additional)
}

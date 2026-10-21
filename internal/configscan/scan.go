// Package configscan discovers configuration definitions and validates configuration definitions with scoped policy. Lint's single-definition selection does not use this walker.
package configscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/gitignore"
	"github.com/frithuz/mdchk/internal/parser"
)

type Limits struct{ Entries, PathBytes, Depth, SourceBytes, TotalSourceBytes int64 }

func DefaultLimits() Limits { return Limits{100000, 4096, 64, 1048576, 8388608} }
func (l Limits) validate() error {
	if l.Entries <= 0 || l.PathBytes <= 0 || l.Depth <= 0 || l.SourceBytes <= 0 || l.TotalSourceBytes < l.SourceBytes || l.Entries > 1<<30 || l.PathBytes > 1<<20 || l.Depth > 4096 || l.TotalSourceBytes > 1<<30 {
		return errors.New("invalid configuration scan limits")
	}
	return nil
}

// Options makes ambient Git source selection explicit and testable. Nil
// Options.Environment snapshots the process environment once at operation entry.
type Options struct {
	Root        string
	Limits      Limits
	Budget      *parser.OperationBudget
	Environment map[string]string
}
type Definition struct {
	Path string
	Data []byte
}
type Skip struct {
	Path, Reason, Source string
	Line                 int
	Origins              []config.Origin
}
type SourceRole string

const (
	DefinitionSource SourceRole = "definition"
	IgnoreSource     SourceRole = "ignore"
	TraversalSource  SourceRole = "traversal"
)

type Failure struct {
	Path, Operation string
	Role            SourceRole
	Err             error
}

func (e Failure) Error() string { return fmt.Sprintf("%s %s: %v", e.Operation, e.Path, e.Err) }
func (e Failure) Unwrap() error { return e.Err }

type Result struct {
	Definitions  []Definition
	Skipped      []Skip
	Errors       []Failure
	Interruption error
	Complete     bool
}

// Hooks is the later definition-validation seam. Prune runs before directory
// reads. Definition runs before child enumeration; only a successful return can
// install state. Invalid definitions therefore cannot acquire pruning authority.
// A semantic Definition error is retained without making a fully traversed scan
// incomplete. Cancellation, deadlines and typed limits stop discovery instead.
// Callbacks must treat their inputs as owned branch values (PolicyState does).
type Hooks struct {
	Prune      func(string, config.PolicyState) bool
	Exclude    func(string, bool, config.PolicyState) (Skip, bool, error)
	Definition func(Definition, config.PolicyState) (config.PolicyState, error)
}
type ignoreSource struct {
	path    string
	matcher *gitignore.Source
}
type scanner struct {
	ctx                        context.Context
	options                    Options
	access                     scanAccess
	result                     Result
	entries, total             int64
	records                    int64
	repository, gitdir, common string
	ignoreCase                 bool
}

func Scan(ctx context.Context, options Options, hooks Hooks) Result {
	return scan(ctx, options, hooks, nil)
}
func scan(ctx context.Context, options Options, hooks Hooks, wrap func(scanAccess) scanAccess) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &scanner{ctx: ctx, options: options, result: Result{Complete: true}}
	if options.Limits == (Limits{}) {
		s.options.Limits = DefaultLimits()
	}
	if err := s.options.Limits.validate(); err != nil {
		s.fail(options.Root, "options", err)
		return s.result
	}
	if s.options.Budget == nil {
		s.options.Budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	}
	if err := s.check(); err != nil {
		s.stop(err)
		return s.result
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		s.fail(options.Root, "root", err)
		return s.result
	}
	s.options.Root = root
	if s.options.Environment == nil {
		s.options.Environment = map[string]string{}
		for _, entry := range os.Environ() {
			k, v, ok := strings.Cut(entry, "=")
			if ok {
				s.options.Environment[k] = v
			}
		}
	}
	native, err := openAccess(root)
	if err != nil {
		s.fail(root, "root", err)
		return s.result
	}
	defer closeHandle(native.root)
	s.access = native
	if wrap != nil {
		s.access = wrap(s.access)
	}
	info, err := s.access.lstat(root)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("scan root must be a nonsymlink directory")
		}
		s.fail(root, "root", err)
		return s.result
	}
	sources, err := s.sources()
	if errors.Is(err, errRootIgnored) {
		return s.result
	}
	if err != nil {
		s.handle(root, "ignore sources", err)
		return s.result
	}
	s.walk(root, sources, config.PolicyState{}, hooks)
	slices.SortFunc(s.result.Definitions, func(a, b Definition) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(s.result.Skipped, func(a, b Skip) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(s.result.Errors, func(a, b Failure) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Operation, b.Operation)
	})
	if s.result.Interruption == nil {
		if err := s.check(); err != nil {
			s.stop(err)
		}
	}
	return s.result
}
func (s *scanner) check() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.options.Budget.ChargeWork(0)
}
func (s *scanner) stop(err error) { s.result.Complete = false; s.result.Interruption = err }
func (s *scanner) fail(path, op string, err error) {
	s.result.Complete = false
	s.result.Errors = append(s.result.Errors, Failure{Path: path, Operation: op, Role: TraversalSource, Err: err})
}
func (s *scanner) handle(path, op string, err error) {
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		s.stop(err)
	} else {
		var failure Failure
		if errors.As(err, &failure) {
			s.result.Complete = false
			s.result.Errors = append(s.result.Errors, failure)
		} else {
			s.fail(path, op, err)
		}
	}
}
func (s *scanner) logical(path string) string {
	base := s.repository
	if base == "" {
		base = s.options.Root
	}
	r, _ := filepath.Rel(base, path)
	if r == "." {
		return ""
	}
	return filepath.ToSlash(r)
}
func (s *scanner) read(path string) (body []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = Failure{Path: path, Operation: "read source", Role: IgnoreSource, Err: resultErr}
		}
	}()
	if err := s.check(); err != nil {
		return nil, err
	}
	f, err := s.access.open(path, false)
	if err != nil {
		return nil, err
	}
	defer closeHandle(f)
	allowed := min(s.options.Limits.SourceBytes, s.options.Limits.TotalSourceBytes-s.total)
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > allowed {
		name := "config-bytes"
		cap := s.options.Limits.SourceBytes
		observed := info.Size()
		if cap > allowed {
			name = "total-config-bytes"
			cap = s.options.Limits.TotalSourceBytes
			observed = s.total + info.Size()
		}
		return nil, &parser.LimitExceeded{Name: name, Observed: observed, Allowed: cap}
	}
	data := make([]byte, 0, min(info.Size(), allowed))
	buf := make([]byte, 4096)
	for {
		if err := s.check(); err != nil {
			return nil, err
		}
		n, e := f.Read(buf[:min(int64(len(buf)), allowed-int64(len(data))+1)])
		if n > 0 {
			if int64(len(data)+n) > allowed {
				return nil, &parser.LimitExceeded{Name: "config-bytes", Observed: int64(len(data) + n), Allowed: allowed}
			}
			if err := s.options.Budget.ChargeWork(int64(n)); err != nil {
				return nil, err
			}
			data = append(data, buf[:n]...)
			s.total += int64(n)
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	current, err := s.access.lstat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) || !os.SameFile(after, current) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("source changed during acquisition")
	}
	if err := s.check(); err != nil {
		return nil, err
	}
	return data, nil
}
func (s *scanner) load(path, base string) (ignoreSource, bool, error) {
	data, err := s.read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ignoreSource{}, false, nil
	}
	if err != nil {
		return ignoreSource{}, false, err
	}
	matcher, err := gitignore.ParseBounded(s.ctx, data, base, s.ignoreCase, s.options.Budget, s.options.Budget.Limits().TotalNodes-s.records)
	if err == nil {
		observed := s.records + matcher.Records()
		if observed > s.options.Budget.Limits().TotalNodes {
			return ignoreSource{}, false, &parser.LimitExceeded{Name: "total-nodes", Observed: observed, Allowed: s.options.Budget.Limits().TotalNodes}
		}
		s.records = observed
	}
	return ignoreSource{path, matcher}, true, err
}
func (s *scanner) ignored(path string, dir bool, sources []ignoreSource) (Skip, bool, error) {
	decision := Skip{}
	ignored := false
	for _, source := range sources {
		d, err := source.matcher.Match(s.ctx, s.options.Budget, s.logical(path), dir)
		if err != nil {
			return Skip{}, false, err
		}
		if d.Matched {
			ignored = d.Ignored
			decision = Skip{Path: path, Reason: "gitignore", Source: source.path, Line: d.Line}
		}
	}
	return decision, ignored, nil
}
func (s *scanner) walk(path string, sources []ignoreSource, state config.PolicyState, hooks Hooks) {
	if s.result.Interruption != nil {
		return
	}
	if err := s.check(); err != nil {
		s.stop(err)
		return
	}
	if hooks.Exclude != nil {
		skip, excluded, err := hooks.Exclude(path, true, state)
		if err != nil {
			s.handle(path, "configuration exclusion", err)
			return
		}
		if excluded {
			s.result.Skipped = append(s.result.Skipped, skip)
			return
		}
	}
	if hooks.Prune != nil && hooks.Prune(path, state) {
		s.result.Skipped = append(s.result.Skipped, Skip{Path: path, Reason: "configuration exclusion"})
		return
	}
	f, err := s.access.open(path, true)
	if err != nil {
		s.handle(path, "enumerate", err)
		return
	}
	defer closeHandle(f)
	directoryInfo, err := f.Stat()
	if err != nil {
		s.fail(path, "enumerate", err)
		return
	}
	// Read one entry at a time. A giant directory is never materialized before
	// its finite entry cap. Sorting only the admitted bounded names is deterministic.
	entries := []fs.DirEntry{}
	for {
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		batch, e := f.ReadDir(1)
		if len(batch) > 0 {
			s.entries++
			if s.entries > s.options.Limits.Entries {
				s.stop(&parser.LimitExceeded{Name: "directory-entries", Observed: s.entries, Allowed: s.options.Limits.Entries})
				return
			}
			if err := s.options.Budget.ChargeWork(int64(len(batch[0].Name()) + 1)); err != nil {
				s.stop(err)
				return
			}
			entries = append(entries, batch[0])
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			s.fail(path, "enumerate", e)
			return
		}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	if err := s.check(); err != nil {
		s.stop(err)
		return
	}
	current, e := s.access.lstat(path)
	if e != nil {
		s.handle(path, "enumerate", e)
		return
	}
	if !os.SameFile(directoryInfo, current) {
		s.fail(path, "enumerate", errors.New("directory changed during enumeration"))
		return
	}
	if err := s.check(); err != nil {
		s.stop(err)
		return
	}

	presentNames := map[string]bool{}
	markerName := false
	for _, entry := range entries {
		presentNames[entry.Name()] = true
		markerName = markerName || strings.EqualFold(entry.Name(), ".git")
	}
	if path != s.options.Root && markerName {
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		_, e := s.access.lstat(filepath.Join(path, ".git"))
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		if e == nil {
			s.result.Skipped = append(s.result.Skipped, Skip{Path: path, Reason: "nested repository"})
			return
		}
		if !errors.Is(e, fs.ErrNotExist) {
			s.handle(path, "repository marker", e)
			return
		}
	}
	for _, name := range []string{".gitignore", config.Filename} {
		if presentNames[name] {
			if err := s.pathLimit(filepath.Join(path, name)); err != nil {
				s.stop(err)
				return
			}
		}
	}
	if err := s.check(); err != nil {
		s.stop(err)
		return
	}
	var local ignoreSource
	present := false
	if presentNames[".gitignore"] {
		local, present, err = s.load(filepath.Join(path, ".gitignore"), s.logical(path))
	}
	if err != nil {
		s.handle(filepath.Join(path, ".gitignore"), "read ignore", err)
		return
	}
	if present {
		sources = append(slices.Clone(sources), local)
	}
	definition := filepath.Join(path, config.Filename)
	eligible := presentNames[config.Filename]
	if eligible && hooks.Exclude != nil {
		skip, excluded, e := hooks.Exclude(definition, false, state)
		if e != nil {
			s.handle(definition, "configuration exclusion", e)
			return
		}
		if excluded {
			s.result.Skipped = append(s.result.Skipped, skip)
			eligible = false
		}
	}
	if eligible {
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		info, e := s.access.lstat(definition)
		if e != nil {
			s.definitionFailure(definition, "stat definition", e)
		} else if info.Mode()&fs.ModeSymlink == 0 {
			skip, ignored, e := s.ignored(definition, false, sources)
			if e != nil {
				s.handle(definition, "match", e)
				return
			}
			if ignored {
				s.result.Skipped = append(s.result.Skipped, skip)
			} else {
				data, e := s.read(definition)
				if e != nil {
					s.definitionFailure(definition, "read definition", e)
				} else {
					if err := s.check(); err != nil {
						s.stop(err)
						return
					}
					d := Definition{definition, data}
					s.result.Definitions = append(s.result.Definitions, d)
					if hooks.Definition != nil {
						if err := s.check(); err != nil {
							s.stop(err)
							return
						}
						next, e := hooks.Definition(Definition{d.Path, slices.Clone(d.Data)}, state)
						if e != nil {
							var limit *parser.LimitExceeded
							if errors.As(e, &limit) || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
								s.stop(e)
							} else {
								s.result.Errors = append(s.result.Errors, Failure{Path: definition, Operation: "definition", Role: DefinitionSource, Err: e})
							}
						} else {
							state = next
						}
					}
				}
			}
		}
	}
	if s.result.Interruption != nil {
		return
	}
	if err := s.check(); err != nil {
		s.stop(err)
		return
	}
	marker, _ := s.access.lstat(filepath.Join(path, ".git"))
	for _, entry := range entries {
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		current, e := s.access.lstat(path)
		if e != nil {
			s.fail(path, "enumerate", e)
			return
		}
		if !os.SameFile(directoryInfo, current) {
			s.fail(path, "enumerate", errors.New("directory changed during enumeration"))
			return
		}
		if s.result.Interruption != nil {
			return
		}
		child := filepath.Join(path, entry.Name())
		if err := s.pathLimit(child); err != nil {
			s.stop(err)
			return
		}
		if err := s.check(); err != nil {
			s.stop(err)
			return
		}
		info, e := s.access.lstat(child)
		if e != nil {
			s.fail(child, "stat", e)
			continue
		}
		if entry.Name() == ".git" || marker != nil && os.SameFile(marker, info) {
			s.result.Skipped = append(s.result.Skipped, Skip{Path: child, Reason: "git metadata"})
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			s.result.Skipped = append(s.result.Skipped, Skip{Path: child, Reason: "symlink"})
			continue
		}
		if !info.IsDir() {
			continue
		}
		skip, ignored, e := s.ignored(child, true, sources)
		if e != nil {
			s.handle(child, "match", e)
			continue
		}
		if ignored {
			s.result.Skipped = append(s.result.Skipped, skip)
			continue
		}
		s.walk(child, sources, state, hooks)
	}
	if err := s.check(); err != nil {
		s.stop(err)
	}
}
func (s *scanner) pathLimit(path string) error {
	rel, _ := filepath.Rel(s.options.Root, path)
	logical := filepath.ToSlash(rel)
	if int64(len(logical)) > s.options.Limits.PathBytes {
		return &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(logical)), Allowed: s.options.Limits.PathBytes}
	}
	depth := int64(strings.Count(rel, string(filepath.Separator)) + 1)
	if depth > s.options.Limits.Depth {
		return &parser.LimitExceeded{Name: "path-depth", Observed: depth, Allowed: s.options.Limits.Depth}
	}
	return nil
}

func (s *scanner) definitionFailure(path, op string, err error) {
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		s.stop(err)
		return
	}
	s.result.Complete = false
	s.result.Errors = append(s.result.Errors, Failure{Path: path, Operation: op, Role: DefinitionSource, Err: err})
}

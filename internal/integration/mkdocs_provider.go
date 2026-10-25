package integration

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

type mkDocsAssemblyKey struct{}

// WithMkDocs returns an immutable binding to the exact operation's service.
// Construct documents/contexts from the returned scope. No worker or allowance
// is created; project assembly runs synchronously outside the resource worker.
func (s *Scope) WithMkDocs(ctx context.Context, service *resource.Service) (*Scope, error) {
	if s == nil || service == nil || !service.OwnsOperation(s.operation) {
		return nil, ErrContract
	}
	if err := service.Checkpoint(ctx, 1); err != nil {
		return nil, err
	}
	if err := s.operation.Budget().ChargeNodes(1); err != nil {
		return nil, err
	}
	out := *s
	out.mkdocsService = service
	return &out, nil
}

// MkDocsProblem is source-bound interpretation uncertainty, separate from actual
// adapter I/O and canonical controls returned as errors.
type MkDocsProblem struct {
	location Location
	message  string
}

func (p MkDocsProblem) Location() Location { return p.location }
func (p MkDocsProblem) Message() string    { return p.message }

// MkDocsTarget retains one literal authored path and its final existence query.
// Known positive facts survive unrelated config/inventory uncertainty. Missing
// is an observation; Static is separately required for negative interpretation.
type MkDocsTarget struct {
	location    Location
	destination string
	class       resource.ReferenceClass
	target      resource.Location
	existence   resource.Existence
	required    resource.RequiredType
	problem     resource.Problem
}

func (t MkDocsTarget) Location() Location                  { return t.location }
func (t MkDocsTarget) Destination() string                 { return t.destination }
func (t MkDocsTarget) Class() resource.ReferenceClass      { return t.class }
func (t MkDocsTarget) Target() resource.Location           { return t.target }
func (t MkDocsTarget) Existence() resource.Existence       { return t.existence }
func (t MkDocsTarget) RequiredType() resource.RequiredType { return t.required }
func (t MkDocsTarget) Problem() resource.Problem           { return mkDocsCopyProblem(t.problem) }
func (c MkDocsContext) DocsRoot() DocsRoot                 { return c.docsRoot }
func (c MkDocsContext) Static() bool                       { return c.static }
func (c MkDocsContext) Complete() bool                     { return c.complete }
func (c MkDocsContext) ConfigComplete() bool               { return c.configComplete }
func (c MkDocsContext) TargetsComplete() bool              { return c.targetsComplete }
func (c MkDocsContext) ConfigObservation() resource.ObservationSpec {
	out := c.configObservation
	out.Problem = mkDocsCopyProblem(out.Problem)
	return out
}
func (c MkDocsContext) Problems(ctx context.Context, s *Scope) ([]MkDocsProblem, error) {
	if c.scope == nil || s != c.scope {
		return nil, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(c.problems))+1, int64(len(c.problems))); err != nil {
		return nil, err
	}
	return append([]MkDocsProblem(nil), c.problems...), nil
}
func (c MkDocsContext) Targets(ctx context.Context, s *Scope) ([]MkDocsTarget, error) {
	if c.scope == nil || s != c.scope {
		return nil, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(c.targets))+1, int64(len(c.targets))); err != nil {
		return nil, err
	}
	return append([]MkDocsTarget(nil), c.targets...), nil
}

func (s *Scope) assembleMkDocs(ctx context.Context, out MkDocsContext) (MkDocsContext, error) {
	ctx = context.WithValue(ctx, mkDocsAssemblyKey{}, s)
	key, _ := resource.NewKey(s.mkdocs.ConfigPath)
	acquired, observation, err := s.mkdocsService.AcquireSource(ctx, s.view, key)
	out.configObservation = observation
	if acquired.Source().Token() == (parser.SourceToken{}) {
		return out, err
	}
	out.config = acquired.Source()
	decoded, decodeErr := decodeMkDocsYAML(ctx, s, out.config)
	err = errors.Join(err, decodeErr)
	out.problems = decoded.problems
	if decoded.root == nil {
		return out, err
	}
	whole, _ := out.config.Span(0, out.config.Len())
	block := Location{out.config, whole}
	problem := func(loc Location, message string) error {
		if e := s.checkpoint(ctx, int64(len(message))+1, 1); e != nil {
			return e
		}
		out.problems = append(out.problems, MkDocsProblem{loc, message})
		return nil
	}
	docs := parser.Optional[string]{}
	if value, ok := decoded.root.field("docs_dir"); ok {
		if value.kind != "string" {
			return out, errors.Join(err, problem(value.location, "docs_dir must be a literal relative string."))
		}
		docs = parser.Optional[string]{Present: true, Value: value.text}
	}
	root, e := s.PlanDocsRoot(ctx, docs)
	if e != nil {
		if errors.Is(e, ErrContract) {
			return out, errors.Join(err, problem(block, "docs_dir must stay within the authorized resource root."))
		}
		return out, errors.Join(err, e)
	}
	out.docsRoot = root
	directory, e := acquired.DirectoryLocation(ctx, root.name)
	if e != nil {
		return out, errors.Join(err, e)
	}
	out.directory = directory
	directoryObservation, directoryErr := acquired.Inspect(ctx, directory, resource.DirectoryType)
	err = errors.Join(err, directoryErr)
	if directoryObservation.Existence != resource.Directory {
		return out, errors.Join(err, problem(block, "Documentation root is unavailable or is not a directory."))
	}
	shape := NavAbsent
	definition := block
	var entries []NavItem
	nav, present := decoded.root.field("nav")
	navComplete := true
	if present {
		definition = nav.location
		switch nav.kind {
		case "null":
			shape = NavNull
		case "list":
			shape = NavList
		default:
			shape = NavUnsupported
			navComplete = false
			if e := problem(nav.location, "nav must be a literal list or null."); e != nil {
				return out, errors.Join(err, e)
			}
		}
	}
	var projectNav func(*mkDocsValue, int) ([]NavItem, error)
	projectNav = func(value *mkDocsValue, depth int) ([]NavItem, error) {
		if e := s.checkpoint(ctx, int64(len(value.values))+1, int64(len(value.values))); e != nil {
			return nil, e
		}
		if int64(depth) > s.operation.Budget().Limits().SyntaxDepth {
			return nil, &parser.LimitExceeded{Name: "syntax-depth", Observed: int64(depth), Allowed: s.operation.Budget().Limits().SyntaxDepth}
		}
		items := make([]NavItem, 0, len(value.values))
		for _, child := range value.values {
			item := NavItem{Kind: NavPath, Location: child.location}
			switch child.kind {
			case "string":
				item.Destination = child.text
			case "map":
				if len(child.fields) != 1 {
					navComplete = false
					if e := problem(child.location, "Each nav mapping must have one title."); e != nil {
						return items, e
					}
					continue
				}
				field := child.fields[0]
				item.Title = field.key
				item.Location = field.value.location
				switch field.value.kind {
				case "string":
					item.Destination = field.value.text
				case "list":
					item.Kind = NavSection
					children, e := projectNav(field.value, depth+1)
					item.Children = children
					if e != nil {
						return items, e
					}
				default:
					navComplete = false
					if e := problem(field.value.location, "Nav targets must be literal paths or sections."); e != nil {
						return items, e
					}
					continue
				}
			default:
				navComplete = false
				if e := problem(child.location, "Unsupported navigation entry."); e != nil {
					return items, e
				}
				continue
			}
			items = append(items, item)
		}
		return items, nil
	}
	if shape == NavList {
		entries, e = projectNav(nav, 0)
		if e != nil {
			return out, errors.Join(err, e)
		}
	}
	navigation, e := s.Navigation(ctx, definition, shape, entries)
	if e != nil {
		return out, errors.Join(err, e)
	}
	out.navigation = navigation
	// Ordinary unrelated safe data is accepted. Dynamic projection settings are
	// retained as uncertainty without inspecting includes, Python or the host.
	static := len(decoded.problems) == 0 && navComplete
	for _, name := range []string{"INHERIT", "hooks"} {
		if value, ok := decoded.root.field(name); ok {
			static = false
			if e := problem(value.location, "Dynamic MkDocs setting is unsupported: "+name); e != nil {
				return out, errors.Join(err, e)
			}
		}
	}
	if plugins, ok := decoded.root.field("plugins"); ok {
		if plugins.kind != "list" {
			static = false
			if e := problem(plugins.location, "plugins must be a literal list."); e != nil {
				return out, errors.Join(err, e)
			}
		} else {
			for _, plugin := range plugins.values {
				safe := plugin.kind == "string" && plugin.text == "search"
				if plugin.kind == "map" && len(plugin.fields) == 1 {
					field := plugin.fields[0]
					safe = field.key == "search"
					if field.value.kind == "map" {
						if enabled, present := field.value.field("enabled"); present {
							if enabled.kind == "bool" && !enabled.boolean {
								safe = true
							} else if enabled.kind != "bool" {
								safe = false
							}
						}
					} else if field.value.kind != "null" {
						safe = false
					}
				}
				if !safe {
					static = false
					if e := problem(plugin.location, "Active or malformed plugin has unknown navigation effects."); e != nil {
						return out, errors.Join(err, e)
					}
				}
			}
		}
	}
	out.static = static
	out.targetsComplete = true
	var inspect func([]NavItem) error
	inspect = func(items []NavItem) error {
		for _, item := range items {
			if item.Kind == NavSection {
				if e := inspect(item.Children); e != nil {
					return e
				}
				continue
			}
			if e := s.checkpoint(ctx, 1, 1); e != nil {
				return e
			}
			target := MkDocsTarget{location: item.Location, destination: item.Destination, existence: resource.Unknown}
			normalized, e := acquired.Resolve(ctx, item.Location.span, directory, item.Destination, s.mkdocs.CheckAbsolute)
			if e != nil {
				return e
			}
			target.class = normalized.Class()
			target.target = normalized.Location()
			target.required = normalized.RequiredType()
			target.problem = normalized.Problem()
			if target.class == resource.Local {
				fact, e := acquired.Inspect(ctx, target.target, target.required)
				target.existence = fact.Existence
				target.problem = fact.Problem
				err = errors.Join(err, e)
				if fact.Existence == resource.Directory {
					index, indexErr := acquired.IndexLocation(ctx, target.target)
					target.target = index
					target.required = resource.RegularFile
					if indexErr != nil {
						err = errors.Join(err, indexErr)
					} else {
						indexed, indexErr := acquired.Inspect(ctx, index, resource.RegularFile)
						target.existence = indexed.Existence
						target.problem = indexed.Problem
						err = errors.Join(err, indexErr)
					}
				}
			}

			if target.class == resource.BlockedReference || target.class == resource.InvalidReference || target.class == resource.Local && (target.existence == resource.Unknown || target.required == resource.RegularFile && target.existence == resource.Directory || target.required == resource.DirectoryType && target.existence == resource.File) {
				out.targetsComplete = false
			}
			out.targets = append(out.targets, target)
			if e := s.checkpoint(ctx, 0, 0); e != nil {
				return e
			}
		}
		return nil
	}
	if e := inspect(entries); e != nil {
		return out, errors.Join(err, e)
	}
	out.status = Available
	out.configComplete = len(out.problems) == 0 && navComplete
	out.complete = out.configComplete && out.targetsComplete
	if s.mkdocs.CheckOmitted && shape == NavList {
		names, inventoryErr := acquired.Enumerate(ctx, directory)
		out.names = names
		err = errors.Join(err, inventoryErr)
		facts, factsErr := names.Facts(ctx, acquired)
		err = errors.Join(err, factsErr)
		inventory, e := s.mkDocsEligible(ctx, decoded.root, out.config, root, facts, names.Complete() && inventoryErr == nil && factsErr == nil && static)
		out.inventory = inventory
		if e != nil {
			var pattern *mkDocsPatternError
			if errors.As(e, &pattern) {
				if pErr := problem(block, e.Error()); pErr != nil {
					err = errors.Join(err, pErr)
				}
			} else {
				err = errors.Join(err, e)
			}
		}
		if !out.inventory.complete {
			out.complete = false
		}
	}
	return out, err
}

func (s *Scope) mkDocsEligible(ctx context.Context, config *mkDocsValue, source parser.Source, root DocsRoot, names []resource.NameFact, complete bool) (EligibleInventory, error) {
	out := EligibleInventory{scope: s, source: source, complete: complete}
	specs := make([]*mkDocsPatterns, 3)
	for i, name := range []string{"exclude_docs", "draft_docs", "not_in_nav"} {
		var lines []string
		if i == 0 {
			lines = []string{".*", "/templates/"}
		}
		if value, present := config.field(name); present && value.kind != "null" {
			if value.kind != "string" {
				out.complete = false
				return out, &mkDocsPatternError{reason: name + " must be a multiline string"}
			}
			lines = append(lines, value.text)
		}
		patterns, e := newMkDocsPatterns(ctx, s, lines)
		if e != nil {
			out.complete = false
			return out, e
		}
		specs[i] = patterns
	}
	if e := s.checkpoint(ctx, int64(len(names))+1, int64(len(names))*2+1); e != nil {
		out.complete = false
		return out, e
	}
	out.entries = make([]EligiblePageFact, 0, len(names))
	destinations := map[string]int{}
	for _, name := range names {
		if name.Existence() != resource.File {
			continue
		}
		key := name.Key()
		relative := key.String()
		if root.name != "" {
			relative = strings.TrimPrefix(relative, root.name+"/")
		}
		if e := s.checkpoint(ctx, 4*int64(len(relative))+1, 0); e != nil {
			out.complete = false
			return out, e
		}
		extension := path.Ext(relative)
		documentation := extension == ".markdown" || extension == ".mdown" || extension == ".mkdn" || extension == ".mkd" || extension == ".md"
		// Non-document names still participate in same-directory index.html
		// destination conflicts, although they never become omission pages.
		indexAsset := !documentation && path.Base(relative) == "index.html"
		if !documentation && !indexAsset {
			continue
		}
		eligibility := EligiblePage
		for i, spec := range specs {
			matched, e := spec.Match(ctx, s, relative)
			if e != nil {
				out.complete = false
				return out, e
			}
			if matched {
				eligibility = []Eligibility{ExcludedPage, DraftPage, NotInNavPage}[i]
				break
			}
		}
		current := -1
		if documentation {
			current = len(out.entries)
			out.entries = append(out.entries, EligiblePageFact{key, eligibility})
		}
		stem := strings.TrimSuffix(path.Base(relative), extension)
		if stem == "README" {
			stem = "index"
		}
		destination := path.Join(path.Dir(relative), stem)
		// MkDocs keeps the FIRST destination owner in files_by_dest and applies
		// each later pair only after inclusion. With three colliding suffixes,
		// removing that first file does not silently remove the later two.
		if first, ok := destinations[destination]; ok {
			if (eligibility == EligiblePage || eligibility == NotInNavPage) && first >= 0 && (out.entries[first].eligibility == EligiblePage || out.entries[first].eligibility == NotInNavPage) {
				out.entries[first].eligibility = ConflictingReadme
			}
		} else {
			destinations[destination] = current
		}

	}
	return out, nil
}

func mkDocsCopyProblem(p resource.Problem) resource.Problem {
	if p.Limit != nil {
		limit := *p.Limit
		p.Limit = &limit
	}
	return p
}

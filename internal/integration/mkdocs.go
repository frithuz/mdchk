package integration

import (
	"context"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

const MkDocsVersion = "1.6.1"

// MkDocsPolicy is explicit effective MD074 policy, not a second integration
// enable switch. ConfigPath is a canonical operation-root-relative resource key.
type MkDocsPolicy struct {
	Enabled       bool
	ConfigPath    string
	CheckAbsolute bool
	CheckOmitted  bool
}

func (p MkDocsPolicy) normalized() (MkDocsPolicy, error) {
	if p.ConfigPath == "" {
		p.ConfigPath = "mkdocs.yml"
	}
	if _, err := resource.NewKey(p.ConfigPath); err != nil {
		return MkDocsPolicy{}, ErrContract
	}
	return p, nil
}
func (s *Scope) MkDocsPolicy() MkDocsPolicy {
	if s == nil {
		return MkDocsPolicy{}
	}
	return s.mkdocs
}

// DocsRoot is a lexical plan below the *existing* operation root, not a new Root
// or proof that the directory exists/is readable. Config acquisition and native
// confinement remain the concrete producer's duty.
type DocsRoot struct {
	scope *Scope
	name  string
}

func (r DocsRoot) Name() string { return r.name }
func (r DocsRoot) ResourceRoot() resource.Root {
	if r.scope == nil {
		return resource.Root{}
	}
	return r.scope.operation.Root()
}

// WithinDocsRoot is the lexical subroot guard after shared URL normalization.
// A true result proves neither existence nor native permission.
func (s *Scope) WithinDocsRoot(ctx context.Context, root DocsRoot, key resource.Key) (bool, error) {
	if s == nil || root.scope != s || key.String() == "" {
		return false, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(root.name)+len(key.String()))+1, 0); err != nil {
		return false, err
	}
	return root.name == "" || key.String() == root.name || strings.HasPrefix(key.String(), root.name+"/"), nil
}
func (s *Scope) PlanDocsRoot(ctx context.Context, directory parser.Optional[string]) (DocsRoot, error) {
	if s == nil || ctx == nil {
		return DocsRoot{}, ErrContract
	}
	docsDir := directory.Value
	if !directory.Present {
		docsDir = "docs"
	}
	limits := resource.DefaultPathLimits()
	if int64(len(docsDir)) > limits.Bytes {
		return DocsRoot{}, &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(docsDir)), Allowed: limits.Bytes}
	}
	if err := s.checkpoint(ctx, int64(len(docsDir)+len(s.mkdocs.ConfigPath))*4+1, int64(len(docsDir))+2); err != nil {
		return DocsRoot{}, err
	}
	if !utf8.ValidString(docsDir) || strings.Contains(docsDir, "\\") || strings.HasPrefix(docsDir, "/") || drivePath(docsDir) || strings.Contains(docsDir, "://") {
		return DocsRoot{}, ErrContract
	}
	for _, r := range docsDir {
		if unicode.IsControl(r) {
			return DocsRoot{}, ErrContract
		}
	}
	// docs_dir is a literal filesystem configuration value, not a URL. It is
	// never percent-decoded or interpreted relative to a requesting Markdown.
	name := path.Clean(path.Join(path.Dir(s.mkdocs.ConfigPath), docsDir))
	if name == ".." || strings.HasPrefix(name, "../") {
		return DocsRoot{}, ErrContract
	}
	if name == "." {
		name = ""
	}
	if name != "" {
		if _, err := resource.NewKey(name); err != nil {
			return DocsRoot{}, ErrContract
		}
	}
	if int64(len(name)) > limits.Bytes {
		return DocsRoot{}, &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(name)), Allowed: limits.Bytes}
	}
	depth := int64(strings.Count(name, "/") + 1)
	if name == "" {
		depth = 0
	}
	if depth > limits.Depth {
		return DocsRoot{}, &parser.LimitExceeded{Name: "path-depth", Observed: depth, Allowed: limits.Depth}
	}
	return DocsRoot{s, name}, nil
}
func drivePath(text string) bool {
	return len(text) >= 2 && (text[0] >= 'A' && text[0] <= 'Z' || text[0] >= 'a' && text[0] <= 'z') && text[1] == ':'
}

// LeadingSlashDecision is solely the integration's extra source-path policy. Generic
// URL/native classification, once-decoding, traversal, exact names and one index
// still belong to the shared resource normalizer in the acquisition producer.
type LeadingSlashDecision struct {
	skipped     bool
	destination string
}

func (d LeadingSlashDecision) Skipped() bool       { return d.skipped }
func (d LeadingSlashDecision) Destination() string { return d.destination }
func (s *Scope) LeadingSlash(ctx context.Context, root DocsRoot, destination string) (LeadingSlashDecision, error) {
	if s == nil || root.scope != s {
		return LeadingSlashDecision{}, ErrContract
	}
	if int64(len(destination)) > s.operation.Budget().Limits().DocumentBytes {
		return LeadingSlashDecision{}, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(len(destination)), Allowed: s.operation.Budget().Limits().DocumentBytes}
	}
	if err := s.checkpoint(ctx, int64(len(destination))+1, 1); err != nil {
		return LeadingSlashDecision{}, err
	}
	if !utf8.ValidString(destination) {
		return LeadingSlashDecision{}, ErrContract
	}
	out := LeadingSlashDecision{destination: destination}
	// Protocol-relative URLs are generic remote references, never a source
	// root request. Leave them unchanged for the shared normalizer to ignore.
	if strings.HasPrefix(destination, "/") && !strings.HasPrefix(destination, "//") {
		if !s.mkdocs.CheckAbsolute {
			out.skipped = true
		} else {
			out.destination = destination[1:]
		}
	}
	return out, nil
}

type NavMode string

const (
	AutomaticNav NavMode = "automatic"
	ManualNav    NavMode = "manual"
	UnknownNav   NavMode = "unknown"
)

type NavShape string

const (
	NavAbsent      NavShape = "absent"
	NavNull        NavShape = "null"
	NavList        NavShape = "list"
	NavUnsupported NavShape = "unsupported"
)

type NavItemKind string

const (
	NavPath    NavItemKind = "path"
	NavSection NavItemKind = "section"
)

// NavItem is a semantic policy input, not a decoded YAML-node authority token.
// #176 must prove its exact location from the acquired config's bounded parse.
type NavItem struct {
	Kind        NavItemKind
	Title       string
	Destination string
	Location    Location
	Children    []NavItem
}
type NavigationDecision struct {
	scope      *Scope
	mode       NavMode
	source     parser.Source
	definition Location
	entries    []NavItem
}

func (d NavigationDecision) Mode() NavMode         { return d.mode }
func (d NavigationDecision) Source() parser.Source { return d.source }
func (d NavigationDecision) Definition() Location  { return d.definition }
func (d NavigationDecision) Entries(ctx context.Context, s *Scope) ([]NavItem, error) {
	if s == nil || d.scope != s || d.mode == "" {
		return nil, ErrContract
	}
	return copyNav(ctx, s, d.entries, 0)
}
func (s *Scope) Navigation(ctx context.Context, definition Location, shape NavShape, entries []NavItem) (NavigationDecision, error) {
	if s == nil || !definition.valid() {
		return NavigationDecision{}, ErrContract
	}
	source := definition.source
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return NavigationDecision{}, err
	}
	if int64(source.Len()) > s.operation.Budget().Limits().DocumentBytes {
		return NavigationDecision{}, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(source.Len()), Allowed: s.operation.Budget().Limits().DocumentBytes}
	}
	out := NavigationDecision{scope: s, source: source, definition: definition}
	switch shape {
	case NavAbsent, NavNull:
		if len(entries) != 0 {
			return NavigationDecision{}, ErrContract
		}
		out.mode = AutomaticNav
	case NavUnsupported:
		if len(entries) != 0 {
			return NavigationDecision{}, ErrContract
		}
		out.mode = UnknownNav
	case NavList:
		out.mode = ManualNav
		var err error
		out.entries, err = copyNav(ctx, s, entries, 0)
		if err != nil {
			return NavigationDecision{}, err
		}
		var check func([]NavItem) bool
		check = func(items []NavItem) bool {
			for _, item := range items {
				if item.Location.source.Token() != source.Token() || item.Location.span.Start() < definition.span.Start() || item.Location.span.End() > definition.span.End() || !check(item.Children) {
					return false
				}
			}
			return true
		}
		if !check(out.entries) {
			return NavigationDecision{}, ErrContract
		}
	default:
		return NavigationDecision{}, ErrContract
	}
	return out, nil
}
func copyNav(ctx context.Context, s *Scope, entries []NavItem, depth int) ([]NavItem, error) {
	if int64(depth) > s.operation.Budget().Limits().SyntaxDepth {
		return nil, &parser.LimitExceeded{Name: "syntax-depth", Observed: int64(depth), Allowed: s.operation.Budget().Limits().SyntaxDepth}
	}
	if err := s.checkpoint(ctx, int64(len(entries))+1, int64(len(entries))); err != nil {
		return nil, err
	}
	out := make([]NavItem, len(entries))
	for i, item := range entries {
		if int64(len(item.Title)) > s.operation.Budget().Limits().DocumentBytes || int64(len(item.Destination)) > s.operation.Budget().Limits().DocumentBytes {
			return nil, ErrContract
		}
		if err := s.checkpoint(ctx, int64(len(item.Title)+len(item.Destination))+1, 0); err != nil {
			return nil, err
		}
		if !item.Location.valid() || !utf8.ValidString(item.Title) || !utf8.ValidString(item.Destination) || item.Kind != NavPath && item.Kind != NavSection || item.Kind == NavPath && len(item.Children) != 0 || item.Kind == NavSection && item.Destination != "" {
			return nil, ErrContract
		}
		out[i] = item
		var err error
		out[i].Children, err = copyNav(ctx, s, item.Children, depth+1)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Plugin is a policy input from the future safe-data producer. Disabled is true
// only for a literal Boolean enabled:false, never the spelling "false".
type Plugin struct {
	Name     string
	Disabled bool
}

func (s *Scope) StaticNavigation(ctx context.Context, plugins []Plugin, hooks, inherit, customTags, dynamic bool) (bool, error) {
	if s == nil {
		return false, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(plugins))+1, 0); err != nil {
		return false, err
	}
	static := !hooks && !inherit && !customTags && !dynamic
	for _, plugin := range plugins {
		if err := s.checkpoint(ctx, int64(len(plugin.Name))+1, 0); err != nil {
			return false, err
		}
		if plugin.Name == "" || !utf8.ValidString(plugin.Name) {
			return false, ErrContract
		}
		if !plugin.Disabled && plugin.Name != "search" {
			static = false
		}
	}
	return static, nil
}

type Eligibility string

const (
	EligiblePage      Eligibility = "included"
	ExcludedPage      Eligibility = "excluded"
	DraftPage         Eligibility = "draft"
	NotInNavPage      Eligibility = "not-in-nav"
	ConflictingReadme Eligibility = "index-readme-conflict"
)

// EligibleInventory has no public constructor or completeness setter. A complete
// name list/cache/map is not proof of MkDocs build eligibility. #176 owns the
// concrete eligibility producer, including Gitignore-like settings/defaults.
type EligibleInventory struct {
	scope    *Scope
	source   parser.Source
	complete bool
	entries  []EligiblePageFact
}
type EligiblePageFact struct {
	key         resource.Key
	eligibility Eligibility
}

func (p EligiblePageFact) Key() resource.Key        { return p.key }
func (p EligiblePageFact) Eligibility() Eligibility { return p.eligibility }
func (i EligibleInventory) Pages(ctx context.Context, s *Scope) ([]EligiblePageFact, error) {
	if s == nil || i.scope != s {
		return nil, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(i.entries))+1, int64(len(i.entries))); err != nil {
		return nil, err
	}
	return append([]EligiblePageFact(nil), i.entries...), nil
}
func (s *Scope) CanCheckOmissions(ctx context.Context, navigation NavigationDecision, inventory EligibleInventory, static bool) (bool, error) {
	if s == nil || navigation.scope != s || navigation.mode == "" {
		return false, ErrContract
	}
	if err := s.checkpoint(ctx, 1, 0); err != nil {
		return false, err
	}
	if navigation.mode == AutomaticNav || !s.mkdocs.Enabled || !s.mkdocs.CheckOmitted {
		return false, nil
	}
	return navigation.mode == ManualNav && static && inventory.scope == s && inventory.source.Token() == navigation.source.Token() && inventory.complete, nil
}

type MkDocsContext struct {
	scope             *Scope
	status            Status
	config            parser.Source
	navigation        NavigationDecision
	inventory         EligibleInventory
	docsRoot          DocsRoot
	directory         resource.Location
	static            bool
	complete          bool
	configComplete    bool
	targetsComplete   bool
	problems          []MkDocsProblem
	targets           []MkDocsTarget
	configObservation resource.ObservationSpec
	names             resource.NameInventory
}

func (c MkDocsContext) Status() Status                 { return c.status }
func (c MkDocsContext) ConfigSource() parser.Source    { return c.config }
func (c MkDocsContext) Navigation() NavigationDecision { return c.navigation }
func (c MkDocsContext) Inventory() EligibleInventory   { return c.inventory }
func (s *Scope) MkDocs(ctx context.Context) (MkDocsContext, error) {
	if s == nil {
		return MkDocsContext{}, ErrContract
	}
	out := MkDocsContext{scope: s, status: Unavailable}
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return out, err
	}
	if !s.mkdocs.Enabled {
		out.status = Inactive
	}
	if !s.mkdocs.Enabled || s.mkdocsService == nil {
		return out, nil
	}
	return s.assembleMkDocs(ctx, out)
}

// KnownCount reports retained entries without allocation or new analysis,
// including after operation/service stop.
func (d NavigationDecision) KnownCount() int { return len(d.entries) }
func (d NavigationDecision) KnownEntry(index int) (NavItem, bool) {
	if index < 0 || index >= len(d.entries) {
		return NavItem{}, false
	}
	item := d.entries[index]
	item.Children = nil
	return item, true
}
func (c MkDocsContext) KnownTargetCount() int { return len(c.targets) }
func (c MkDocsContext) KnownTarget(index int) (MkDocsTarget, bool) {
	if index < 0 || index >= len(c.targets) {
		return MkDocsTarget{}, false
	}
	return c.targets[index], true
}
func (c MkDocsContext) NameInventory() resource.NameInventory { return c.names }
func (i EligibleInventory) KnownCount() int                   { return len(i.entries) }
func (i EligibleInventory) KnownPage(index int) (EligiblePageFact, bool) {
	if index < 0 || index >= len(i.entries) {
		return EligiblePageFact{}, false
	}
	return i.entries[index], true
}

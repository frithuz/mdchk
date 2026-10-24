package resource

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// PathLimits are operator-owned bounds on logical names, not URL/tool settings.
// The existing operation document-byte limit also bounds semantic destinations.
// Normalization uses the same work/node/time budget as acquisition and parsing.
type PathLimits struct{ Bytes, Depth int64 }

func DefaultPathLimits() PathLimits {
	input := DefaultInputLimits()
	return PathLimits{Bytes: input.PathBytes, Depth: input.PathDepth}
}

// NamePolicy is an active project adapter's pure representability predicate for
// an entire canonical key, including every component. It must be deterministic,
// bounded by the key length, safe for concurrent calls and perform no I/O.
// Actual native spelling/type/confinement checks remain the adapter's job.
// Supplied namespaces never call this predicate or inherit server-OS aliases.
type NamePolicy func(Key) bool

// Normalizer binds the single destination pipeline to an immutable source view.
// It performs no acquisition, target registration, network access or writes.
// Instances support concurrent use if their project NamePolicy does too.
type Normalizer struct {
	view      View
	limits    PathLimits
	represent NamePolicy
}

func NewNormalizer(view View, limits PathLimits, represent NamePolicy) (*Normalizer, error) {
	if view.operation == nil || view.identity == nil || limits.Bytes <= 0 || limits.Bytes > 1<<30 || limits.Depth <= 0 || limits.Depth > 1<<20 || view.Root().kind == ProjectRoot && represent == nil {
		return nil, ErrContract
	}
	if err := view.operation.Checkpoint(1); err != nil {
		return nil, err
	}
	if err := view.operation.budget.ChargeNodes(1); err != nil {
		return nil, err
	}
	return &Normalizer{view: view, limits: limits, represent: represent}, nil
}

// Location distinguishes the internal root directory from an ordinary literal
// key. Its zero value is neither. It carries namespace identity, not a host path.
// Root directories cannot be submitted as file keys or converted into Key(".").
type Location struct {
	root          Root
	key           Key
	rootDirectory bool
}

func (l Location) Root() Root            { return l.root }
func (l Location) IsRootDirectory() bool { return l.root.identity != nil && l.rootDirectory }
func (l Location) Key() (Key, bool) {
	return l.key, l.root.identity != nil && !l.rootDirectory && l.key.value != ""
}

type FragmentStatus string

const (
	FragmentAbsent    FragmentStatus = "absent"
	FragmentValid     FragmentStatus = "valid"
	FragmentInvalid   FragmentStatus = "invalid"
	FragmentUnchecked FragmentStatus = "unchecked"
)

// Fragment keeps presence and once-decoded text distinct. Invalid fragments do
// not invalidate a valid file path. Unchecked means an ignored/blocked raw path
// needed no fragment interpretation. The original Link still owns source spans.
type Fragment struct {
	status    FragmentStatus
	raw, text string
}

func (f Fragment) Status() FragmentStatus { return f.status }
func (f Fragment) Raw() string            { return f.raw }
func (f Fragment) Text() string           { return f.text }
func (f Fragment) RequiresAnchor() bool   { return f.status == FragmentValid && f.text != "" }

// NormalizedLink is an immutable decision, not a lookup result. In particular,
// Local does not establish existence, and a directory hint retains its type
// requirement even when its spelling matches the selected regular source file.
type NormalizedLink struct {
	normalizer Normalizer
	link       Link
	class      ReferenceClass
	location   Location
	directory  bool
	fragment   Fragment
	same       parser.Source
	problem    Problem
}

func (n NormalizedLink) Link() Link            { return n.link }
func (n NormalizedLink) Class() ReferenceClass { return n.class }
func (n NormalizedLink) Location() Location    { return n.location }
func (n NormalizedLink) DirectoryHint() bool   { return n.directory }
func (n NormalizedLink) Fragment() Fragment    { return n.fragment }
func (n NormalizedLink) SameSource() (parser.Source, bool) {
	return n.same, n.same != (parser.Source{})
}
func (n NormalizedLink) RequiredType() RequiredType {
	if n.directory {
		return DirectoryType
	}
	return AnyType
}
func (n NormalizedLink) Problem() Problem { return copyProblem(n.problem) }

// Normalize starts with the parser-semantic destination in Link, not raw Markdown
// or an already-decoded URL.Path. It retains Link and the exact selected snapshot.
// Deterministic malformed/blocked paths are decisions; execution stops are errors
// with their original identity. No partial decision is returned after a stop.
func (n *Normalizer) Normalize(link Link) (NormalizedLink, error) {
	if n == nil || n.view.operation == nil || link.path == "" || !link.span.Present() {
		return NormalizedLink{}, ErrContract
	}
	target, available, selected := n.view.Target(link.path)
	if !selected || !available || target.Source != link.source {
		return NormalizedLink{}, ErrContract
	}
	o := n.view.operation
	if err := o.Checkpoint(0); err != nil {
		return NormalizedLink{}, err
	}
	if err := normalizationBound("document-bytes", int64(len(link.destination)), o.budget.Limits().DocumentBytes); err != nil {
		return NormalizedLink{}, err
	}
	// Reserve byte inspections, decoding/copies, segment/name checks and joining
	// before doing them. The 16x bound also covers source-key and policy scans.
	if err := o.Checkpoint(16*(int64(len(link.destination))+int64(len(target.ResourceKey.value))) + 1); err != nil {
		return NormalizedLink{}, err
	}
	if err := o.budget.ChargeNodes(4); err != nil {
		return NormalizedLink{}, err
	}
	out := NormalizedLink{normalizer: *n, link: link, class: Local, fragment: Fragment{status: FragmentAbsent}}
	rawPath, rawFragment, hasFragment := strings.Cut(link.destination, "#")
	rawPath, _, _ = strings.Cut(rawPath, "?")
	if hasFragment {
		out.fragment = Fragment{status: FragmentUnchecked, raw: rawFragment}
	}
	// Classification uses raw component spelling, before any unescape can turn
	// encoded data into a scheme or native path prefix.
	out.class = classifyPath(rawPath)
	if out.class != Local {
		if out.class == BlockedReference {
			out.problem = Problem{Kind: Blocked, Message: "Unsupported local resource path."}
		}
		return n.finish(out)
	}
	if hasFragment {
		text, err := url.PathUnescape(rawFragment)
		if err != nil || !utf8.ValidString(text) {
			out.fragment.status = FragmentInvalid
		} else {
			out.fragment.status, out.fragment.text = FragmentValid, text
		}
	}
	decoded, err := url.PathUnescape(rawPath)
	if err != nil || !utf8.ValidString(decoded) {
		out.class, out.problem = InvalidReference, Problem{Kind: MalformedReference, Message: "Invalid resource path encoding."}
		return n.finish(out)
	}
	for _, scalar := range decoded {
		if unicode.IsControl(scalar) {
			out.class, out.problem = InvalidReference, Problem{Kind: MalformedReference, Message: "Control characters are not permitted in resource paths."}
			return n.finish(out)
		}
	}
	if encodedSeparator(rawPath) {
		out.class, out.problem = BlockedReference, Problem{Kind: Blocked, Message: "Encoded resource path separator is not permitted."}
		return n.finish(out)
	}
	if rawPath == "" {
		// This selected-source identity is valid even outside the resource root.
		// Never derive root membership from the report path or native cwd.
		out.same = target.Source
		if target.ResourceKey.value != "" {
			if err := n.checkKey(target.ResourceKey); err != nil {
				return NormalizedLink{}, err
			}
			out.location = Location{root: o.root, key: target.ResourceKey}
		}
		return n.finish(out)
	}
	if target.ResourceKey.value == "" {
		out.class, out.problem = BlockedReference, Problem{Kind: Blocked, Message: "Source has no cross-file resource namespace."}
		return n.finish(out)
	}
	if err := n.checkKey(target.ResourceKey); err != nil {
		return NormalizedLink{}, err
	}
	last := decoded[strings.LastIndexByte(decoded, '/')+1:]
	out.directory = strings.HasSuffix(decoded, "/") || last == "." || last == ".."
	parts, err := n.resolve(target.ResourceKey, decoded)
	if err != nil {
		return NormalizedLink{}, err
	}
	if parts == nil {
		out.class, out.problem = BlockedReference, Problem{Kind: Blocked, Message: "Resource path escapes its root."}
		return n.finish(out)
	}
	if len(parts) == 0 {
		out.location = Location{root: o.root, rootDirectory: true}
		return n.finish(out)
	}
	name := strings.Join(parts, "/")
	key, err := NewKey(name)
	if err != nil {
		out.class, out.problem = InvalidReference, Problem{Kind: MalformedReference, Message: "Invalid logical resource name."}
		return n.finish(out)
	}
	if key == target.ResourceKey {
		// Known selected identity needs no fresh native lookup. A directory
		// hint still requires DirectoryType and cannot reuse file content as
		// directory success; consumers retain the established type mismatch.
		out.location = Location{root: o.root, key: key}
		if !out.directory {
			out.same = target.Source
		}
		return n.finish(out)
	}
	if !n.representable(key) {
		out.class, out.problem = BlockedReference, Problem{Kind: Blocked, Message: "Resource name is not representable by the active adapter."}
		return n.finish(out)
	}
	out.location = Location{root: o.root, key: key}
	return n.finish(out)
}

func (n *Normalizer) finish(out NormalizedLink) (NormalizedLink, error) {
	if err := n.view.operation.Checkpoint(0); err != nil {
		return NormalizedLink{}, err
	}
	return out, nil
}
func (n *Normalizer) representable(key Key) bool {
	return n.view.Root().kind == SuppliedRoot || n.represent(key)
}
func normalizationBound(name string, observed, allowed int64) error {
	if observed > allowed {
		return &parser.LimitExceeded{Name: name, Observed: observed, Allowed: allowed}
	}
	return nil
}
func (n *Normalizer) checkKey(key Key) error {
	if err := normalizationBound("path-bytes", int64(len(key.value)), n.limits.Bytes); err != nil {
		return err
	}
	return normalizationBound("path-depth", int64(strings.Count(key.value, "/")+1), n.limits.Depth)
}

// resolve maintains an explicit confined segment stack. Nil means root escape;
// an owned nonnil empty stack denotes the virtual root. No path.Clean clamps it.
func (n *Normalizer) resolve(source Key, decoded string) ([]string, error) {
	o := n.view.operation
	parent := source.value
	if slash := strings.LastIndexByte(parent, '/'); slash >= 0 {
		parent = parent[:slash]
	} else {
		parent = ""
	}
	count := strings.Count(parent, "/") + 1
	if parent == "" {
		count = 0
	}
	if err := o.budget.ChargeNodes(int64(2*count) + 1); err != nil {
		return nil, err
	}
	parts := make([]string, 0, count)
	if parent != "" {
		parts = append(parts, strings.Split(parent, "/")...)
	}
	bytes := len(parent)
	for part := range strings.SplitSeq(decoded, "/") {
		if err := o.Checkpoint(0); err != nil {
			return nil, err
		}
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) == 0 {
				return nil, nil
			}
			bytes -= len(parts[len(parts)-1])
			parts = parts[:len(parts)-1]
			if len(parts) > 0 {
				bytes--
			}
		default:
			newBytes := bytes + len(part)
			if len(parts) > 0 {
				newBytes++
			}
			if err := normalizationBound("path-bytes", int64(newBytes), n.limits.Bytes); err != nil {
				return nil, err
			}
			if err := normalizationBound("path-depth", int64(len(parts)+1), n.limits.Depth); err != nil {
				return nil, err
			}
			if err := o.budget.ChargeNodes(1); err != nil {
				return nil, err
			}
			parts, bytes = append(parts, part), newBytes
		}
	}
	return parts, nil
}

func classifyPath(path string) ReferenceClass {
	if drivePrefix(path) || strings.HasPrefix(path, "\\") {
		return BlockedReference
	}
	if strings.HasPrefix(path, "//") {
		return Ignored
	}
	if strings.HasPrefix(path, "/") {
		return BlockedReference
	}
	// An RFC scheme begins with ASCII alpha and continues with alpha/digit/+/-/.
	if colon := strings.IndexByte(path, ':'); colon > 0 {
		scheme := path[:colon]
		valid := asciiAlpha(scheme[0])
		for i := 1; valid && i < len(scheme); i++ {
			b := scheme[i]
			valid = asciiAlpha(b) || b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.'
		}
		if valid {
			if strings.EqualFold(scheme, "file") {
				return BlockedReference
			}
			return Ignored
		}
	}
	if strings.Contains(path, "\\") {
		return BlockedReference
	}
	return Local
}
func asciiAlpha(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
func encodedSeparator(path string) bool {
	for i := 0; i+2 < len(path); i++ {
		if path[i] == '%' && (path[i+1] == '2' && (path[i+2] == 'f' || path[i+2] == 'F') || path[i+1] == '5' && (path[i+2] == 'c' || path[i+2] == 'C')) {
			return true
		}
	}
	return false
}

// IndexLookup is one nonrecursive configured basename attempt after an adapter
// has established a directory. It has no DirectoryIndex method or fallback list.
// A directory-valued index fails its RegularFile requirement under MD057.
type IndexLookup struct {
	location Location
	class    ReferenceClass
	problem  Problem
	same     parser.Source
}

func (i IndexLookup) Location() Location         { return i.location }
func (i IndexLookup) Class() ReferenceClass      { return i.class }
func (i IndexLookup) Problem() Problem           { return copyProblem(i.problem) }
func (i IndexLookup) RequiredType() RequiredType { return RegularFile }
func (i IndexLookup) SameSource() (parser.Source, bool) {
	return i.same, i.same != (parser.Source{})
}

// DirectoryIndex plans exactly one file key. The caller must first establish that
// the original location is a directory; a hint alone is not existence evidence.
// Planning performs no lookup and never turns unknown directory facts into absence.
func (n NormalizedLink) DirectoryIndex() (IndexLookup, error) {
	if n.normalizer.view.operation == nil || n.class != Local || n.location.root.identity == nil {
		return IndexLookup{}, ErrContract
	}
	normalizer := n.normalizer
	o := normalizer.view.operation
	index := o.settings.IndexFile()
	if err := o.Checkpoint(16*(int64(len(n.location.key.value))+int64(len(index))) + 1); err != nil {
		return IndexLookup{}, err
	}
	if err := o.budget.ChargeNodes(1); err != nil {
		return IndexLookup{}, err
	}
	name := index
	if !n.location.rootDirectory {
		if err := normalizationBound("path-bytes", int64(len(n.location.key.value))+1+int64(len(index)), normalizer.limits.Bytes); err != nil {
			return IndexLookup{}, err
		}
		name = n.location.key.value + "/" + index
	}
	key, err := NewKey(name)
	if err != nil {
		return IndexLookup{}, ErrContract
	}
	if err := normalizer.checkKey(key); err != nil {
		return IndexLookup{}, err
	}
	target, available, selected := normalizer.view.Target(n.link.path)
	if !selected || !available || target.Source != n.link.source {
		return IndexLookup{}, ErrContract
	}
	out := IndexLookup{class: Local}
	if key == target.ResourceKey {
		// The directory and regular-file requirements remain separate. This
		// exact selected index needs no fresh native name/content acquisition.
		out.location, out.same = Location{root: o.root, key: key}, target.Source
	} else if !normalizer.representable(key) {
		out.class, out.problem = BlockedReference, Problem{Kind: Blocked, Message: "Index name is not representable by the active adapter."}
	} else {
		out.location = Location{root: o.root, key: key}
	}
	if err := o.Checkpoint(0); err != nil {
		return IndexLookup{}, err
	}
	return out, nil
}

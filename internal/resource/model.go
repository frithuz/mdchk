// Package resource defines read-only resource contracts, destination normalization
// and native filesystem acquisition. It does not fetch URLs, parse Markdown,
// execute lint rules, discover lint targets or write resources.
package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

var ErrContract = errors.New("invalid resource contract")

type RootKind string

const (
	ProjectRoot  RootKind = "project"
	SuppliedRoot RootKind = "supplied"
)

type rootIdentity struct{ _ byte }

// Root identifies an authorized namespace, not a native path or open handle.
// A selected config, report base or explicit target cannot enlarge it.
type Root struct {
	kind     RootKind
	identity *rootIdentity
}

func NewRoot(kind RootKind) (Root, error) {
	if kind != ProjectRoot && kind != SuppliedRoot {
		return Root{}, ErrContract
	}
	return Root{kind, &rootIdentity{}}, nil
}
func (r Root) Kind() RootKind { return r.kind }

// Key is a literal canonical root-relative name. Construction validates spelling
// but never decodes percent escapes, normalizes Unicode/case, or resolves a URL.
type Key struct{ value string }

func NewKey(value string) (Key, error) {
	if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\\") || drivePrefix(value) {
		return Key{}, ErrContract
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return Key{}, ErrContract
		}
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == "" || part == "." || part == ".." {
			return Key{}, ErrContract
		}
	}
	return Key{value}, nil
}
func (k Key) String() string { return k.value }

// Settings are immutable operation policy. Parser revision and anchor policy
// participate in observation/view identity; a path alone is not a cache key.
// Parsed-evidence producers must use ParseOptions; Record cannot authenticate
// parse-policy provenance from the parser result alone.
type Settings struct {
	parsing                 parser.ParseOptions
	index, anchor, revision string
	valid                   bool
}

func NewSettings(parsing parser.ParseOptions, index, anchor, revision string) (Settings, error) {
	if index == "" || index == "." || index == ".." || strings.ContainsAny(index, "/\\:*?[]#") || !utf8.ValidString(index) || revision == "" || !utf8.ValidString(revision) {
		return Settings{}, ErrContract
	}
	for _, r := range index {
		if unicode.IsControl(r) {
			return Settings{}, ErrContract
		}
	}
	if anchor != "github" && anchor != "explicit" {
		return Settings{}, ErrContract
	}
	return Settings{parsing, index, anchor, revision, true}, nil
}
func (s Settings) ParseOptions() parser.ParseOptions { return s.parsing }
func (s Settings) IndexFile() string                 { return s.index }
func (s Settings) AnchorStyle() string               { return s.anchor }
func (s Settings) ParserRevision() string            { return s.revision }

// Link retains the requesting source and semantic destination independently of
// its eventual key. Span is original-byte provenance, not a decoded URL range.
type Link struct {
	path, rule, destination string
	source                  parser.Source
	span                    parser.Span
}

func NewLink(path, rule, destination string, source parser.Source, span parser.Span) (Link, error) {
	if path == "" || rule == "" || !utf8.ValidString(path) || !validRule(rule) || !utf8.ValidString(destination) {
		return Link{}, ErrContract
	}
	if _, err := source.Text(span); err != nil {
		return Link{}, ErrContract
	}
	return Link{path, rule, destination, source, span}, nil
}
func (l Link) Path() string          { return l.path }
func (l Link) Rule() string          { return l.rule }
func (l Link) Destination() string   { return l.destination }
func (l Link) Source() parser.Source { return l.source }
func (l Link) Span() parser.Span     { return l.span }

type Capability string

const (
	ExistenceOnly Capability = "existence"
	TextContent   Capability = "content"
	ParsedAnchors Capability = "anchors"
)

type RequiredType string

const (
	AnyType       RequiredType = "any"
	RegularFile   RequiredType = "file"
	DirectoryType RequiredType = "directory"
)

// ReferenceClass is the normalizer's classification seam. No URL interpretation
// is performed here; a malformed fragment need not prevent a separate file query.
type ReferenceClass string

const (
	Local            ReferenceClass = "local"
	Ignored          ReferenceClass = "ignored-nonlocal"
	BlockedReference ReferenceClass = "blocked-local"
	InvalidReference ReferenceClass = "invalid-reference"
)

type RequestSpec struct {
	Key        Key
	Link       Link
	Capability Capability
	Required   RequiredType
	Class      ReferenceClass
}
type requestIdentity struct{ _ byte }

type Request struct {
	spec     RequestSpec
	view     View
	identity *requestIdentity
	location result.Range
	snapshot result.Snapshot
}

func (r Request) Key() Key                   { return r.spec.Key }
func (r Request) Link() Link                 { return r.spec.Link }
func (r Request) Capability() Capability     { return r.spec.Capability }
func (r Request) RequiredType() RequiredType { return r.spec.Required }
func (r Request) Class() ReferenceClass      { return r.spec.Class }
func (r Request) View() View                 { return r.view }

// Reader is the domain consumer's read-only adapter seam. Implementations must
// honor operation context/budget and return facts, never register targets/writes.
// Concrete native/supplied adapters and Service provide acquisition and caching;
// runtime consumer integration remains downstream.
type Reader interface {
	Read(context.Context, Request) (ObservationSpec, error)
}

func projectLink(link Link, phase Phase) (result.Range, result.Snapshot, error) {
	index := parser.NewLineIndex(link.source)
	start, err := index.Position(link.span.Start())
	if err != nil {
		return result.Range{}, result.Snapshot{}, err
	}
	end, err := index.Position(link.span.End())
	if err != nil {
		return result.Range{}, result.Snapshot{}, err
	}
	digest := sha256.Sum256(link.source.Bytes())
	return result.Range{Start: start, End: end}, result.Snapshot{View: string(phase), SHA256: hex.EncodeToString(digest[:]), SizeBytes: link.source.Len()}, nil
}

func drivePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}
func validRule(value string) bool {
	if len(value) != 5 || value[:2] != "MD" {
		return false
	}
	for i := 2; i < 5; i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

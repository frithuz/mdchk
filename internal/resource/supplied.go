package resource

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/strictjson"
)

// InputLimits are operator-owned preflight bounds, never resource tool fields.
// DocumentBytes comes from the existing shared OperationBudget. RawBytes bounds
// this resource argument; the transport must also bound the whole request.
// Identities here counts explicit files/entries; inferred/queried identities are
// additionally counted by the future inventory/acquisition adapters.
type InputLimits struct {
	RawBytes, TotalContentBytes, TargetFiles, Identities, PathBytes, PathDepth int64
}

func DefaultInputLimits() InputLimits { return InputLimits{16777216, 8388608, 5000, 25000, 4096, 64} }
func (l InputLimits) validate(documentBytes int64) error {
	for _, n := range []int64{l.RawBytes, l.TotalContentBytes, l.TargetFiles, l.Identities, l.PathBytes, l.PathDepth} {
		if n <= 0 || n > 1<<30 {
			return ErrContract
		}
	}
	if l.RawBytes > int64(int(^uint(0)>>1)) || documentBytes > l.TotalContentBytes || l.TargetFiles > l.Identities {
		return ErrContract
	}
	return nil
}

// InputError is a known malformed/oversize supplied-input preflight failure.
// Runtime cancellation/quota errors are returned directly, without this wrapper.
// Path is a property location, not an invented source filename/span. Limit is
// populated for known size/count violations; callers own usage/protocol mapping.
type InputError struct {
	Path, Message string
	Limit         *parser.LimitExceeded
}

func (e *InputError) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Message) }
func (e *InputError) Unwrap() error {
	if e.Limit != nil {
		return e.Limit
	}
	return ErrContract
}
func inputError(path, message string) error { return &InputError{Path: path, Message: message} }
func inputBound(path, name string, n, max int64) error {
	if n <= max {
		return nil
	}
	limit := &parser.LimitExceeded{Name: name, Observed: n, Allowed: max}
	return &InputError{Path: path, Message: limit.Error(), Limit: limit}
}

// SuppliedFile is a submitted standalone target snapshot before exclusions.
// It grants no selected-target authority; callers must pass every supplied file
// for overlap/content accounting, including files later excluded from linting.
type SuppliedFile struct {
	Key    Key
	Source parser.Source
}
type EntryKind string

const (
	EntryFile        EntryKind = "file"
	EntryDirectory   EntryKind = "directory"
	EntryMissing     EntryKind = "missing"
	EntryUnavailable EntryKind = "unavailable"
)

// SuppliedEntry is immutable validated input, not an acquired observation.
// Content's bool distinguishes an absent file payload from a real empty file.
type SuppliedEntry struct {
	key    Key
	kind   EntryKind
	source parser.Source
}

func (e SuppliedEntry) Key() Key        { return e.key }
func (e SuppliedEntry) Kind() EntryKind { return e.kind }
func (e SuppliedEntry) Content() (parser.Source, bool) {
	return e.source, e.source != (parser.Source{})
}

// SuppliedInput contains only owned input facts. Zero means omitted partial input.
// It has no filesystem fallback, lookup, implied directories or target selection.
type SuppliedInput struct {
	present, complete bool
	entries           []SuppliedEntry
	files             []SuppliedFile
}

func (s SuppliedInput) Present() bool            { return s.present }
func (s SuppliedInput) Complete() bool           { return s.complete }
func (s SuppliedInput) Entries() []SuppliedEntry { return slices.Clone(s.entries) }
func (s SuppliedInput) Files() []SuppliedFile    { return slices.Clone(s.files) }

//go:embed supplied.schema.json
var suppliedSchema string

// SuppliedSchema returns an owned JSON Schema 2020-12 object schema suitable for
// embedding in a future tool schema. Cross-record conflicts, raw Unicode/name
// validation and operator bounds additionally require DecodeSupplied.
func SuppliedSchema() []byte { return []byte(suppliedSchema) }

// DecodeSupplied validates a raw resources argument and all submitted files.
// A nil raw slice means omission; JSON null or an empty nonnil slice is invalid.
// It borrows inputs only during the call; returned slices/sources are immutable
// snapshots. It uses the caller's existing budget, never creates/resets one.
// Reject before analysis; transport framing/raw argument preservation is #189.
func DecodeSupplied(ctx context.Context, budget *parser.OperationBudget, limits InputLimits, raw []byte, files []SuppliedFile) (SuppliedInput, error) {
	if ctx == nil || budget == nil || budget.Limits().Validate() != nil || limits.validate(budget.Limits().DocumentBytes) != nil {
		return SuppliedInput{}, ErrContract
	}
	check := func(nodes, work int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := budget.ChargeWork(work); err != nil {
			return err
		}
		return budget.ChargeNodes(nodes)
	}
	if err := check(0, 0); err != nil {
		return SuppliedInput{}, err
	}
	if err := inputBound("resources", "request-bytes", int64(len(raw)), limits.RawBytes); err != nil {
		return SuppliedInput{}, err
	}
	if err := inputBound("files", "target-files", int64(len(files)), limits.TargetFiles); err != nil {
		return SuppliedInput{}, err
	}
	var root strictjson.Value
	if raw != nil {
		var err error
		root, err = strictjson.DecodeMetered(raw, int(limits.RawBytes), check)
		if err != nil {
			var syntax *strictjson.Error
			if errors.As(err, &syntax) {
				path := "resources"
				if syntax.Path != "" {
					path += "." + syntax.Path
				}
				return SuppliedInput{}, inputError(path, syntax.Message)
			}
			return SuppliedInput{}, err
		}
	}
	input := SuppliedInput{present: raw != nil}
	var entries []strictjson.Value
	if raw != nil {
		if root.Kind != "object" {
			return SuppliedInput{}, inputError("resources", "expected an object")
		}
		found := false
		for _, field := range root.Object {
			switch field.Name {
			case "complete":
				if field.Value.Kind != "boolean" {
					return SuppliedInput{}, inputError("resources.complete", "expected a Boolean")
				}
				input.complete = field.Value.Scalar.(bool)
			case "entries":
				if field.Value.Kind != "array" {
					return SuppliedInput{}, inputError("resources.entries", "expected an array")
				}
				entries = field.Value.Array
				found = true
			default:
				return SuppliedInput{}, inputError("resources."+field.Name, "unknown field")
			}
		}
		if !found {
			return SuppliedInput{}, inputError("resources.entries", "required field")
		}
	}
	if err := inputBound("resources.entries", "resource-identities", int64(len(files))+int64(len(entries)), limits.Identities); err != nil {
		return SuppliedInput{}, err
	}
	if err := check(int64(len(files))+int64(len(entries))+1, 1); err != nil {
		return SuppliedInput{}, err
	}
	input.files = slices.Clone(files)
	input.entries = make([]SuppliedEntry, 0, len(entries))
	// Map only declared identities to reject contradictions; no inferred parent or
	// lookup inventory is built. Ancestor walks are bounded by path depth/bytes.
	declared := make(map[string]EntryKind, len(files)+len(entries))
	origins := make(map[string]string, len(files)+len(entries))
	names := make([]string, 0, len(files)+len(entries))
	total := int64(0)
	validatePath := func(path, name string) (Key, error) {
		if err := inputBound(path, "path-bytes", int64(len(name)), limits.PathBytes); err != nil {
			return Key{}, err
		}
		if err := check(0, int64(len(name))*3+1); err != nil {
			return Key{}, err
		}
		if err := inputBound(path, "path-depth", int64(strings.Count(name, "/")+1), limits.PathDepth); err != nil {
			return Key{}, err
		}
		key, err := NewKey(name)
		if err != nil {
			return Key{}, inputError(path, "expected a canonical literal relative name")
		}
		return key, nil
	}
	add := func(path string, key Key, kind EntryKind, source parser.Source) error {
		name := key.String()
		if _, ok := declared[name]; ok {
			return inputError(path, "duplicate supplied path or overlap with files")
		}
		if source != (parser.Source{}) {
			if err := inputBound(path, "document-bytes", int64(source.Len()), budget.Limits().DocumentBytes); err != nil {
				return err
			}
			if int64(source.Len()) > limits.TotalContentBytes-total {
				return inputBound(path, "total-content-bytes", total+int64(source.Len()), limits.TotalContentBytes)
			}
			if err := check(0, int64(source.Len())+1); err != nil {
				return err
			}
			if !utf8.Valid(source.Bytes()) {
				return inputError(path, "content must be UTF-8")
			}
			total += int64(source.Len())
		}
		names = append(names, name)
		declared[name] = kind
		origins[name] = path
		return nil
	}
	for i, file := range files {
		path := "files[" + strconv.Itoa(i) + "]"
		key, err := validatePath(path+".path", file.Key.String())
		if err != nil {
			return SuppliedInput{}, err
		}
		if file.Source == (parser.Source{}) {
			return SuppliedInput{}, inputError(path+".content", "required supplied source")
		}
		if err := add(path, key, EntryFile, file.Source); err != nil {
			return SuppliedInput{}, err
		}
	}
	for i, value := range entries {
		path := "resources.entries[" + strconv.Itoa(i) + "]"
		if value.Kind != "object" {
			return SuppliedInput{}, inputError(path, "expected an object")
		}
		var name, kind, content string
		var hasPath, hasKind, hasContent bool
		for _, field := range value.Object {
			if field.Value.Kind != "string" {
				return SuppliedInput{}, inputError(path+"."+field.Name, "expected a string")
			}
			text := field.Value.Scalar.(string)
			switch field.Name {
			case "path":
				name = text
				hasPath = true
			case "kind":
				kind = text
				hasKind = true
			case "content":
				content = text
				hasContent = true
			default:
				return SuppliedInput{}, inputError(path+"."+field.Name, "unknown field")
			}
		}
		if !hasPath {
			return SuppliedInput{}, inputError(path+".path", "required field")
		}
		if !hasKind {
			return SuppliedInput{}, inputError(path+".kind", "required field")
		}
		key, err := validatePath(path+".path", name)
		if err != nil {
			return SuppliedInput{}, err
		}
		ek := EntryKind(kind)
		switch ek {
		case EntryFile:
		case EntryDirectory, EntryMissing, EntryUnavailable:
			if hasContent {
				return SuppliedInput{}, inputError(path+".content", "only file entries may supply content")
			}
		default:
			return SuppliedInput{}, inputError(path+".kind", "unknown entry kind")
		}
		source := parser.Source{}
		if hasContent {
			if err := inputBound(path+".content", "document-bytes", int64(len(content)), budget.Limits().DocumentBytes); err != nil {
				return SuppliedInput{}, err
			}
			if err := inputBound(path+".content", "total-content-bytes", total+int64(len(content)), limits.TotalContentBytes); err != nil {
				return SuppliedInput{}, err
			}
			if err := check(1, int64(len(content))+1); err != nil {
				return SuppliedInput{}, err
			}
			source = parser.NewSource([]byte(content))
		}
		if err := add(path, key, ek, source); err != nil {
			return SuppliedInput{}, err
		}
		input.entries = append(input.entries, SuppliedEntry{key, ek, source})
	}
	for _, name := range names {
		parent := name
		for {
			slash := strings.LastIndexByte(parent, '/')
			if slash < 0 {
				break
			}
			parent = parent[:slash]
			if err := check(0, int64(len(parent))+1); err != nil {
				return SuppliedInput{}, err
			}
			if kind, ok := declared[parent]; ok && kind != EntryDirectory {
				return SuppliedInput{}, inputError(origins[name], "declared ancestor is not an accessible directory")
			}
		}
	}
	if err := check(0, 0); err != nil {
		return SuppliedInput{}, err
	}
	return input, nil
}

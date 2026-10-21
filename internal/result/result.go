// Package result defines the shared schema-version-1 operation envelope.
// The engine produces its read-only lint subset; adapters choose presentation
// and process exits. See docs/contracts/results/result.schema.json.
package result

import (
	"encoding/json"

	"github.com/frithuz/mdchk/internal/parser"
)

// Position uses the parser's original-byte, one-based Unicode-scalar mapping.
type Position = parser.Position

// Range ends are exclusive. MarshalJSON preserves the shared wire spelling
// without introducing a second coordinate model in the parser.
type Range struct{ Start, End Position }

func (r Range) MarshalJSON() ([]byte, error) {
	type position struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	}
	return json.Marshal(struct {
		Start position `json:"start"`
		End   position `json:"end"`
	}{position{r.Start.Line, r.Start.Column}, position{r.End.Line, r.End.Column}})
}

type Snapshot struct {
	View      string `json:"view"`
	SHA256    string `json:"sha256"`
	SizeBytes int    `json:"size_bytes"`
}
type ByteRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type Diagnostic struct {
	ID           string    `json:"id"`
	Path         string    `json:"path"`
	Rule         string    `json:"rule"`
	Severity     string    `json:"severity"`
	Message      string    `json:"message"`
	Range        Range     `json:"range"`
	ByteRange    ByteRange `json:"byte_range"`
	Snapshot     Snapshot  `json:"snapshot"`
	FixAvailable bool      `json:"fix_available"`
}
type ErrorCategory string

const (
	Usage         ErrorCategory = "usage"
	Configuration ErrorCategory = "configuration"
	IO            ErrorCategory = "io"
	Internal      ErrorCategory = "internal"
)

type Error struct {
	ID         string        `json:"id"`
	Category   ErrorCategory `json:"category"`
	Code       string        `json:"code"`
	Message    string        `json:"message"`
	Scope      string        `json:"scope"`
	Path       string        `json:"path,omitempty"`
	Rule       string        `json:"rule,omitempty"`
	Range      *Range        `json:"range,omitempty"`
	Snapshot   *Snapshot     `json:"snapshot,omitempty"`
	ByteOffset *int          `json:"byte_offset,omitempty"`
}

// Coverage records unassessed work, never a successful empty check. The open
// reason vocabulary and cause links are those of schema v1.
type Coverage struct {
	ID       string    `json:"id"`
	Reason   string    `json:"reason"`
	Message  string    `json:"message"`
	Path     string    `json:"path,omitempty"`
	Rule     string    `json:"rule,omitempty"`
	Range    *Range    `json:"range,omitempty"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	CauseIDs []string  `json:"cause_ids,omitempty"`
}
type LimitObservation struct {
	Name     string `json:"name"`
	Observed int64  `json:"observed"`
	Allowed  int64  `json:"allowed"`
	Unit     string `json:"unit"`
}
type Interruption struct {
	ID      string            `json:"id"`
	Reason  string            `json:"reason"`
	Phase   string            `json:"phase"`
	Message string            `json:"message"`
	Path    string            `json:"path,omitempty"`
	Limit   *LimitObservation `json:"limit,omitempty"`
}
type File struct {
	Path           string    `json:"path"`
	Analysis       string    `json:"analysis"`
	WriteStatus    string    `json:"write_status"`
	ReturnStatus   string    `json:"return_status"`
	InputSnapshot  *Snapshot `json:"input_snapshot,omitempty"`
	ResultSnapshot *Snapshot `json:"result_snapshot,omitempty"`
	ErrorIDs       []string  `json:"error_ids"`
	CoverageIDs    []string  `json:"coverage_ids"`
	ChangeIDs      []string  `json:"change_ids"`
}
type SkippedFile struct {
	Path    string `json:"path"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// Omission summarizes whole records that could not be returned within the
// bounded reporting reserve. Count is absent only when the count is unknown.
type Omission struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Count   *int   `json:"count,omitempty"`
}

type EffectiveLimit struct {
	Name   string `json:"name"`
	Value  int64  `json:"value"`
	Unit   string `json:"unit"`
	Source string `json:"source"`
}

// Result is the existing shared v1 envelope. Reserved collections are empty for
// lint; their operation-specific Go record types will be supplied by their
// owning fix/inspection adapters, against the same normative schema. RawMessage
// here is not an extension bag: producers must validate against that schema.
type Result struct {
	SchemaVersion         int               `json:"schema_version"`
	Operation             string            `json:"operation"`
	Mode                  string            `json:"mode"`
	Status                string            `json:"status"`
	Complete              bool              `json:"complete"`
	CheckedFiles          []string          `json:"checked_files"`
	SkippedFiles          []SkippedFile     `json:"skipped_files"`
	Files                 []File            `json:"files"`
	Diagnostics           []Diagnostic      `json:"diagnostics"`
	Coverage              []Coverage        `json:"coverage"`
	Errors                []Error           `json:"errors"`
	Interruptions         []Interruption    `json:"interruptions"`
	EffectiveLimits       []EffectiveLimit  `json:"effective_limits"`
	Changes               []json.RawMessage `json:"changes"`
	DeferredFixes         []json.RawMessage `json:"deferred_fixes"`
	Notes                 []json.RawMessage `json:"notes"`
	Omissions             []Omission        `json:"omissions"`
	Documents             []json.RawMessage `json:"documents"`
	SuppressedDiagnostics []json.RawMessage `json:"suppressed_diagnostics"`
}

func NewLint(mode string) Result {
	return Result{SchemaVersion: 1, Operation: "lint", Mode: mode, Status: "clean", Complete: true, CheckedFiles: []string{}, SkippedFiles: []SkippedFile{}, Files: []File{}, Diagnostics: []Diagnostic{}, Coverage: []Coverage{}, Errors: []Error{}, Interruptions: []Interruption{}, EffectiveLimits: []EffectiveLimit{}, Changes: []json.RawMessage{}, DeferredFixes: []json.RawMessage{}, Notes: []json.RawMessage{}, Omissions: []Omission{}, Documents: []json.RawMessage{}, SuppressedDiagnostics: []json.RawMessage{}}
}

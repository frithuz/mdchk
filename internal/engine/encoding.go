package engine

import (
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Fixed retention bounds for invalid-utf8 records. Every file keeps its first
// record even after the operation allowance is spent, so the worst case is 14,999
// records across the 5,000 targets target-files admits. They bound retention, not
// a resource, and are not configurable limits.
const (
	MaxFileEncodingErrors      = 1000
	MaxOperationEncodingErrors = 10000
)

// encodingRecordWork reserves one record's message, locations and finalization.
const encodingRecordWork = 128

// EncodingValidation describes the mandatory UTF-8 gate on an original source
// snapshot. Empty Errors means decoding succeeded, not that Markdown was parsed
// or any rule ran. Errors holds the retained invalid maximal subparts in source
// order, and Omission accounts for the rest. Index can be reused by subsequent
// source analysis.
type EncodingValidation struct {
	Index    parser.LineIndex
	Snapshot result.Snapshot
	Errors   []result.Error
	Omission *result.Omission
}

// ValidateEncoding runs before parsing, metadata, directives, or rule callbacks.
// MD094 controls attribution only: disabling it never bypasses this gate.
// Source bytes are neither normalized nor replaced. Callers own acquisition and
// admission bounds before this source-sized scan, and the charge for it. Only the
// per-file cap applies, since no operation budget is shared.
func ValidateEncoding(path string, source parser.Source, md094Enabled bool) EncodingValidation {
	validation, _ := validateEncoding(path, source, md094Enabled, snapshot(source), nil)
	return validation
}

// validateEncoding charges the second pass over an invalid source to budget, and
// takes its records from the allowance budget shares across the operation's
// analyses. A nil budget charges nothing and has the whole allowance. Files reach
// this in path order, so the same targets retain the same records.
func validateEncoding(path string, source parser.Source, md094Enabled bool, snap result.Snapshot, budget *parser.OperationBudget) (EncodingValidation, error) {
	index := parser.NewLineIndex(source)
	validation := EncodingValidation{Index: index, Snapshot: snap}
	if index.EncodingFailure() == nil {
		return validation, nil
	}
	retain := MaxFileEncodingErrors
	if budget != nil {
		retain = int(max(1, min(MaxFileEncodingErrors, MaxOperationEncodingErrors-budget.EncodingErrorsRetained())))
		if err := budget.ChargeWork(int64(source.Len()) + encodingRecordWork*int64(min(retain, source.Len()))); err != nil {
			return validation, err
		}
	}
	parts, total := index.InvalidSubparts(retain)
	if budget != nil {
		budget.RetainEncodingErrors(int64(len(parts)))
	}
	rule := ""
	if md094Enabled {
		rule = "MD094"
	}
	validation.Errors = make([]result.Error, len(parts))
	raw := source.Bytes()
	for i, part := range parts {
		offset := part.Start
		location := result.Range{Start: part.Position, End: part.Position}
		validation.Errors[i] = result.Error{Category: result.IO, Code: "invalid-utf8", Scope: "operation", Message: invalidUTF8Message(raw[part.Start:part.End], offset), Path: path, Rule: rule, ByteOffset: &offset, ByteRange: &result.ByteRange{Start: part.Start, End: part.End}, Range: &location, Snapshot: &snap}
	}
	if count := total - len(parts); count > 0 {
		validation.Omission = &result.Omission{Section: "/errors", Reason: "output-limit", Path: path, Count: &count, Message: "invalid UTF-8 records past the retention cap omitted"}
	}
	return validation, nil
}

func invalidUTF8Message(sequence []byte, offset int) string {
	var b strings.Builder
	b.WriteString("Invalid UTF-8 byte sequence")
	for _, c := range sequence {
		fmt.Fprintf(&b, " 0x%02X", c)
	}
	fmt.Fprintf(&b, " at byte offset %d.", offset)
	return b.String()
}

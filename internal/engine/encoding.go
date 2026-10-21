package engine

import (
	"fmt"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// EncodingValidation describes the mandatory UTF-8 gate on an original source
// snapshot. A nil Error means decoding succeeded, not that Markdown was parsed
// or any rule ran. Index can be reused by subsequent source analysis.
type EncodingValidation struct {
	Index    parser.LineIndex
	Snapshot result.Snapshot
	Error    *result.Error
}

// ValidateEncoding runs before parsing, metadata, directives, or rule callbacks.
// MD094 controls attribution only: disabling it never bypasses this gate.
// Source bytes are neither normalized nor replaced. Callers own acquisition and
// admission bounds before this source-sized scan.
func ValidateEncoding(path string, source parser.Source, md094Enabled bool) EncodingValidation {
	return validateEncoding(path, source, md094Enabled, snapshot(source))
}

func validateEncoding(path string, source parser.Source, md094Enabled bool, snap result.Snapshot) EncodingValidation {
	index := parser.NewLineIndex(source)
	validation := EncodingValidation{Index: index, Snapshot: snap}
	if failure := index.EncodingFailure(); failure != nil {
		offset := failure.Location.Start()
		position, _ := index.FailurePosition()
		location := result.Range{Start: position, End: position}
		rule := ""
		if md094Enabled {
			rule = "MD094"
		}
		validation.Error = &result.Error{Category: result.IO, Code: "invalid-utf8", Scope: "operation", Message: fmt.Sprintf("Invalid UTF-8 at byte offset %d.", offset), Path: path, Rule: rule, ByteOffset: &offset, Range: &location, Snapshot: &snap}
	}
	return validation
}

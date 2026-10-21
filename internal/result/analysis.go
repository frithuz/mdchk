package result

import "slices"

// AnalysisRecord is an owned immutable analysis observation retained for shared
// consumers. It has no lint wire representation; concrete producers expose only
// defensive accessors. Serialized records must use the normative envelope.
type AnalysisRecord interface{ SourcePath() string }

func (r *Result) RetainAnalysis(record AnalysisRecord) { r.analysis = append(r.analysis, record) }
func (r Result) AnalysisRecords() []AnalysisRecord     { return slices.Clone(r.analysis) }

// Note and SuppressedDiagnostic are declared v1 records. Lint intentionally
// leaves directive_id absent because lint does not publish directive records;
// exact ownership remains in its immutable analysis observations.
type Note struct {
	ID         string     `json:"id"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Path       string     `json:"path,omitempty"`
	Rule       string     `json:"rule,omitempty"`
	Range      *Range     `json:"range,omitempty"`
	ByteRange  *ByteRange `json:"byte_range,omitempty"`
	Snapshot   *Snapshot  `json:"snapshot,omitempty"`
	RelatedIDs []string   `json:"related_ids,omitempty"`
}
type SuppressedDiagnostic struct {
	Diagnostic   Diagnostic  `json:"diagnostic"`
	SuppressedBy Suppression `json:"suppressed_by"`
}
type Suppression struct {
	Kind        string `json:"kind"`
	DirectiveID string `json:"directive_id,omitempty"`
}

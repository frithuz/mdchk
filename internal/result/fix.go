package result

// Change describes one original-to-final validated correction. CLI previews
// publish identities and optional line hunks, never candidate content or writes.
type Change struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Disposition string   `json:"disposition"`
	Before      Snapshot `json:"before"`
	Candidate   Snapshot `json:"candidate"`
	Rules       []string `json:"rules"`
	EditCount   int      `json:"edit_count"`
	Diff        *Diff    `json:"diff,omitempty"`
}
type Diff struct {
	Format       string     `json:"format"`
	ContextLines int        `json:"context_lines"`
	Hunks        []DiffHunk `json:"hunks"`
}
type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}
type DiffLine struct {
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	LineEnding string `json:"line_ending"`
}
type DeferredFix struct {
	ID               string   `json:"id"`
	Path             string   `json:"path"`
	Rule             string   `json:"rule"`
	GroupID          string   `json:"group_id"`
	Reason           string   `json:"reason"`
	Message          string   `json:"message"`
	Range            *Range   `json:"range,omitempty"`
	Snapshot         Snapshot `json:"snapshot"`
	ConflictGroupIDs []string `json:"conflict_group_ids,omitempty"`
	CauseIDs         []string `json:"cause_ids,omitempty"`
}

func NewFix(dryRun bool) Result {
	r := NewLint("cli")
	r.Operation = "fix"
	r.DryRun = &dryRun
	return r
}

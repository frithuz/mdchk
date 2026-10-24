// Package frontmatter loads operation-independent test fixtures. It is imported
// only by tests; it does not implement recognition or operation behavior.
package frontmatter

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed operations.json
var fixtures embed.FS

type Selection struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats,omitempty"`
}

type Header struct {
	Format     string  `json:"format"`
	Region     [2]int  `json:"region"`
	Opener     [2]int  `json:"opener"`
	Content    [2]int  `json:"content"`
	Closer     *[2]int `json:"closer"`
	BodyOffset int     `json:"body_offset"`
}

type Case struct {
	ID                 string     `json:"id"`
	Source             string     `json:"source"`
	Frontmatter        *Selection `json:"frontmatter"`
	Header             *Header    `json:"header"`
	Complete           bool       `json:"complete"`
	Diagnostics        int        `json:"diagnostics"`
	DiagnosticPosition [2]int     `json:"diagnostic_position"`
	Fix                bool       `json:"fix"`
	Candidate          string     `json:"candidate"`
}

// Load returns fresh fixture values, including independent slices and pointers.
// Callers supply flavor and operation; neither is encoded in expected facts.
func Load() ([]Case, error) {
	raw, err := fixtures.ReadFile("operations.json")
	if err != nil {
		return nil, err
	}
	var rows []Case
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("empty frontmatter operation ledger")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.ID == "" || seen[row.ID] {
			return nil, fmt.Errorf("missing or duplicate frontmatter fixture %q", row.ID)
		}
		seen[row.ID] = true
	}
	return rows, nil
}

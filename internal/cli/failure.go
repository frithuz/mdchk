package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func failureResult(err error, code string, category result.ErrorCategory) result.Result {
	out := result.NewLint("cli")
	base := result.Error{Category: category, Code: code, Scope: "operation", Message: err.Error()}
	if category != result.Configuration {
		out.Errors = append(out.Errors, base)
		engine.FinalizeLintResult(&out)
		return out
	}
	// The config layer owns acquisition identity. Never reopen a configuration
	// here: validation may have failed against bytes no longer present on disk.
	snapshots := map[string]result.Snapshot{}
	var source *config.SourceError
	if errors.As(err, &source) {
		for _, s := range source.Sources() {
			snapshots[s.Path] = result.Snapshot{View: "original", SHA256: s.SHA256, SizeBytes: int(s.SizeBytes)}
		}
	}
	return configurationFailure(err, base, snapshots)
}

func configurationFailure(err error, base result.Error, snapshots map[string]result.Snapshot) result.Result {
	out := result.NewLint("cli")
	logical := func(path string) string {
		cwd, e := os.Getwd()
		if e == nil {
			if rel, e := filepath.Rel(cwd, path); e == nil {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(path)
	}
	origins := []config.Origin{}
	var decode *config.DecodeError
	var validation *config.ValidationError
	var policy *config.PolicyError
	switch {
	case errors.As(err, &policy):
		origins = append(origins, policy.Origin)
		origins = append(origins, policy.Restrictions...)
	case errors.As(err, &decode):
		origins = append(origins, decode.Origin)
		if decode.Previous != nil {
			origins = append(origins, *decode.Previous)
		}
	case errors.As(err, &validation):
		origins = append(origins, validation.Origin)
	}
	if len(origins) == 0 {
		out.Errors = append(out.Errors, base)
	}
	for _, o := range origins {
		record := base
		if o.Path != "" {
			record.Path = logical(o.Path)
		}
		if snapshot, ok := snapshots[o.Path]; ok {
			record.Snapshot = &snapshot
			if o.Position != nil {
				record.Range = &result.Range{Start: *o.Position, End: *o.Position}
			}
		}
		out.Errors = append(out.Errors, record)
	}
	engine.FinalizeLintResult(&out)
	return out
}
func reportFailure(err error, code string, category result.ErrorCategory, format string, stdout io.Writer, limits map[string]int64) int {
	return reportResult(failureResult(err, code, category), format, stdout, limits)
}
func (inv *lintInvocation) fail(err error, code string, category result.ErrorCategory) int {
	out := failureResult(err, code, category)
	inv.result = &out
	return lintExit(out)
}

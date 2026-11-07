package report

import "github.com/frithuz/mdchk/internal/result"

const (
	ExitUsage      = 11
	ExitConfig     = 12
	ExitIO         = 13
	ExitIncomplete = 15
)

func LintExit(out result.Result) int {
	status := 0
	if len(out.Diagnostics) > 0 {
		status = 10
	}
	if !out.Complete {
		status = ExitIncomplete
	}
	// Usage errors here are preflight/empty selection only. Runtime failures
	// retain semantic precedence, including IO over an empty selection.
	for _, e := range out.Errors {
		if e.Category == result.Configuration {
			status = ExitConfig
		}
		if e.Category == result.Usage {
			status = ExitUsage
		}
	}
	for _, e := range out.Errors {
		if e.Category == result.IO {
			status = ExitIO
		}
	}
	for _, e := range out.Errors {
		if e.Category == result.Internal {
			status = 14
		}
	}
	return status
}

func InspectionOutcome(out *result.Result) {
	out.Operation = "inspect"
	out.Status = "inspected"
	if len(out.Errors) > 0 || len(out.Coverage) > 0 {
		out.Complete = false
	}
	if len(out.Omissions) > 0 {
		out.Complete = false
	}
	if len(out.Interruptions) > 0 || result.HasReportingOmission(out.Omissions) {
		out.Complete = false
		out.Status = "incomplete"
	}
	for _, e := range out.Errors {
		if e.Scope == "operation" {
			out.Complete = false
			out.Status = "error"
		}
	}
}

func InspectionExit(out result.Result) int {
	status := 0
	if len(out.Interruptions) > 0 || result.HasReportingOmission(out.Omissions) {
		status = ExitIncomplete
	}
	for _, category := range []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal} {
		for _, e := range out.Errors {
			if e.Scope == "operation" && e.Category == category {
				switch category {
				case result.Usage:
					status = ExitUsage
				case result.Configuration:
					status = ExitConfig
				case result.IO:
					status = ExitIO
				case result.Internal:
					status = 14
				}
			}
		}
	}
	return status
}

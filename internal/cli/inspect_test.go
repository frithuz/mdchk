package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestInspectionCancellationDeliveryAndInternalPrecedence(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, "doc.md"), "text\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if status := RunContext(ctx, []string{"inspect", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 15 {
		t.Fatalf("cancel status=%d %s", status, stdout.String())
	}
	out := validateEmitted(t, t.TempDir(), stdout.Bytes())
	if out.Operation != "inspect" || out.Status != "incomplete" || len(out.Interruptions) != 1 || len(out.CheckedFiles) != 0 || stderr.Len() != 0 {
		t.Fatal(out)
	}
	for _, format := range []string{"text", "json"} {
		for _, timed := range []bool{false, true} {
			for _, category := range []result.ErrorCategory{result.IO, result.Internal} {
				r := newInspectResult()
				r.Errors = append(r.Errors, result.Error{ID: "cause", Scope: "operation", Category: category, Code: "fixture", Message: "failure"})
				finalizeInspection(&r)
				limits := map[string]int64{}
				if timed {
					limits["reporting-time"] = 1
				}
				var delivered bytes.Buffer
				status := reportInspection(r, nil, format, &delivered, limits)
				want := 13
				if category == result.Internal {
					want = 14
				}
				if status != want {
					t.Fatal(status, want)
				}
				if status := reportInspection(r, nil, format, inspectionShortWriter{}, limits); status != want {
					t.Fatal("shortwrite precedence", status, want)
				}
				if format == "json" {
					validateEmitted(t, t.TempDir(), delivered.Bytes())
				}
			}
		}
	}
	// Output failures on a completed observational inspection are operational IO.
	r := newInspectResult()
	r.Errors = append(r.Errors, result.Error{Scope: "observation", Category: result.IO, Code: "invalid-utf8", Message: "decoding observation"})
	finalizeInspection(&r)
	if status := reportInspection(r, nil, "json", inspectionShortWriter{}, nil); status != 13 {
		t.Fatal(status)
	}
}

func TestInspectionReporterOwnsViewsAndExactOmissions(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), strings.Repeat("paragraph\n\n", 100))
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"inspect", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String())
	}
	r := validateEmitted(t, t.TempDir(), stdout.Bytes())
	// Exercise the actual adapter-owned typed projection by decoding its declared
	// shape; wire kinds, not arbitrary Go implementation values, drive rendering.
	before, _ := json.Marshal(r)
	var reduced bytes.Buffer
	var typed inspection.Projection
	_ = json.Unmarshal(r.Inspection, &typed)
	typedBefore, _ := json.Marshal(typed)
	if status := reportInspection(r, &typed, "json", &reduced, map[string]int64{"response-bytes": 65536}); status != 15 {
		t.Fatal(status)
	}
	got := validateEmitted(t, t.TempDir(), reduced.Bytes())
	if len(got.Files) != 1 || got.Files[0].InputSnapshot == nil || len(reduced.Bytes()) > 65536 {
		t.Fatal(got)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("reporter mutated its input envelope")
	}
	typedAfter, _ := json.Marshal(typed)
	if !reflect.DeepEqual(typedBefore, typedAfter) {
		t.Fatal("reporter mutated its typed projection")
	}
	count := map[string]int{}
	for _, o := range got.Omissions {
		if o.Count == nil {
			t.Fatal("known omission count missing")
		}
		count[o.Section] += *o.Count
	}
	var q inspection.Projection
	_ = json.Unmarshal(got.Inspection, &q)
	if q.AST.Data == nil && count["/inspection/ast"] != 1 {
		t.Fatal(count)
	}
	if q.Rules.Data == nil && count["/inspection/rules"] != len(*typed.Rules.Data) {
		t.Fatal(count)
	}
}

type inspectionShortWriter struct{}

func (inspectionShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestInspectionCrossingSuppressionAndSharedExecution(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			root := project(t)
			var yaml strings.Builder
			yaml.WriteString("version: 1\nflavor: commonmark\nrules:\n")
			for _, r := range engine.Catalog() {
				enabled := r.ID == "MD001"
				fmt.Fprintf(&yaml, "  %s: {enabled: %t", r.ID, enabled)
				if r.ID == "MD001" && blocked {
					yaml.WriteString(", allow_directives: false")
				}
				yaml.WriteString("}\n")
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), yaml.String())
			source := "<!-- mdchk:disable MD001 -->\nfirst\nselected\nlast\n"
			put(t, filepath.Join(root, "doc.md"), source)
			called := 0
			registry, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called++
				if err := c.Checkpoint(ctx, 1); err != nil {
					return engine.RuleOutcome{Err: err}
				}
				span, _ := c.Source().Span(strings.Index(source, "first"), len(source))
				return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "crossing synthetic finding"}}}
			}}})
			if err != nil {
				t.Fatal(err)
			}
			opts, err := parseInspect([]string{"doc.md", "--lines=3", "--format=json"})
			if err != nil {
				t.Fatal(err)
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			out, p := executeInspect(context.Background(), opts, "test", root, budget, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, DirectiveProcessing: true})
			if called != 1 || inspectionExit(out) != 0 || p.File.SelectedLines.Start != 3 || p.RuleCounts.Availability != "available" {
				t.Fatal(called, out.Status, p)
			}
			if blocked {
				if len(out.Diagnostics) != 1 || !out.Diagnostics[0].ExtendsRegion || len(out.SuppressedDiagnostics) != 0 {
					t.Fatal(out.Diagnostics, out.SuppressedDiagnostics)
				}
			} else {
				if len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 {
					t.Fatal(out.Diagnostics, out.SuppressedDiagnostics)
				}
				var hidden result.SuppressedDiagnostic
				_ = json.Unmarshal(out.SuppressedDiagnostics[0], &hidden)
				if hidden.SuppressedBy.DirectiveID != "directive-1" || !hidden.Diagnostic.ExtendsRegion || !(*p.Directives.Data)[0].ContextOnly {
					t.Fatal(hidden, p.Directives)
				}
			}
			var output bytes.Buffer
			if status := reportInspection(out, p, "json", &output, nil); status != 0 {
				t.Fatal(status)
			}
			validateEmitted(t, t.TempDir(), output.Bytes())
		})
	}
}

func TestInspectionTinyReportingDeadlineRetainsObservationSemantics(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, "doc.md"), "text\n")
	put(t, filepath.Join(root, ".mdchk.yaml"), "unknown: [")
	var produced, stderr bytes.Buffer
	if status := Run([]string{"inspect", "doc.md", "--format=json"}, &produced, &stderr, "test"); status != 0 {
		t.Fatal(status)
	}
	r := validateEmitted(t, t.TempDir(), produced.Bytes())
	var p inspection.Projection
	_ = json.Unmarshal(r.Inspection, &p)

	for _, format := range []string{"text", "json"} {
		for _, n := range []int64{1, 1000} {
			var output bytes.Buffer
			if status := reportInspection(r, &p, format, &output, map[string]int64{"reporting-time": n}); status != 15 && !(n == 1000 && status == 0) {
				t.Fatalf("deadline %d format %s status %d: %s", n, format, status, output.String())
			}
			if format == "json" {
				out := validateEmitted(t, t.TempDir(), output.Bytes())
				if (out.Status != "incomplete" && out.Status != "inspected") || len(out.Errors) != 1 || out.Errors[0].Scope != "observation" || out.Errors[0].Category != result.Configuration {
					t.Fatal(out)
				}
			}
		}
	}
}

func TestInspectionManifestAdmissionPrecedesCallbacks(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\n")
	put(t, filepath.Join(root, "doc.md"), "text\n")
	called := 0
	registry, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { called++; return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := parseInspect([]string{"doc.md", "--format=json", "--limit=response-bytes=64KiB"})
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, p := executeInspect(context.Background(), opts, "test", root, budget, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, DirectiveProcessing: true})
	if called != 0 || inspectionExit(out) != 15 || out.Files[0].Analysis != "not-started" || p.AST.Availability != "unavailable" || p.File.SHA256 == "" {
		t.Fatal(called, out, p)
	}
	var output bytes.Buffer
	if status := reportInspection(out, p, "json", &output, opts.limits); status != 15 {
		t.Fatal(status)
	}
	validateEmitted(t, t.TempDir(), output.Bytes())
}

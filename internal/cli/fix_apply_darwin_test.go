//go:build darwin && arm64 && cgo

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

type applyFixture struct {
	root      string
	inv       *lintInvocation
	budget    *parser.OperationBudget
	native    *files.WriteSet
	acquired  *files.Acquisition
	validated fix.Validated
}

func setupApplyFixture(t *testing.T, ctx context.Context, dependencies map[string][]string) applyFixture {
	t.Helper()
	return setupApplyFixtureContents(t, ctx, dependencies, nil)
}
func setupApplyFixtureContents(t *testing.T, ctx context.Context, dependencies map[string][]string, contents map[string]string) applyFixture {
	t.Helper()
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		source := p
		if value, ok := contents[p]; ok {
			source = value
		}
		put(t, filepath.Join(root, p), source)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	registry, providers, caps, e := configuredProviders()
	if e != nil {
		t.Fatal(e)
	}
	selected, e := config.ResolveValidatedBounded(config.SelectionOptions{WorkingDirectory: root}, registry, providers, config.DiscoveryBudget{Context: ctx, Budget: budget})
	if e != nil {
		t.Fatal(e)
	}
	resolution, e := selected.Admit(caps)
	if e != nil {
		t.Fatal(e)
	}
	inv := &lintInvocation{selection: selected, plan: resolution.ExecutionPlan(), workingDirectory: root, options: lintOptions{format: "json"}}
	targets, e := files.SelectBounded(ctx, files.Options{WorkingDirectory: root, Paths: []string{"a.md", "b.md", "c.md"}}, budget)
	if e != nil {
		t.Fatal(e)
	}
	native, acquired, resources, e := acquireFixWrite(ctx, inv, targets, files.AcquireOptions{Budget: budget})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := native.Close(); e != nil {
			t.Error(e)
		}
	})
	checks := rules.CorrectionCheckers()
	original := checks[0].Check
	checks[0].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		// A's independent trusted proof requires B's actual candidate newline when
		// declared. This fixture makes stale-original dependency checks insufficient.
		if c.After.Path() == "a.md" && len(dependencies["a.md"]) > 0 {
			b, ok := c.After.Source("b.md")
			if !ok || !strings.HasSuffix(string(b.Bytes()), "\n") {
				return fix.Unsafe, nil
			}
		}
		return original(ctx, c)
	}
	validator, e := fix.NewValidator(registry, checks)
	if e != nil {
		t.Fatal(e)
	}
	var inputs []fix.Target
	for _, d := range acquired.Documents {
		p := inv.logicalPath(d.Path)
		inputs = append(inputs, fix.Target{Path: p, Source: d.Source, Plan: inv.plan.CandidatePlan(), Produce: rules.Corrections, Dependencies: dependencies[p]})
	}
	validated, e := validator.Validate(ctx, inputs, fix.ValidationOptions{Budget: budget, Resources: resources})
	if e != nil {
		t.Fatal(e)
	}
	return applyFixture{root, inv, budget, native, acquired, validated}
}

func TestApplyIndependentPartialAndObservedFailures(t *testing.T) {
	for _, mode := range []string{"refused", "failed", "published-error", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := setupApplyFixture(t, ctx, nil)
			out := result.NewFix(false)
			states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
				if p == "b.md" && (mode == "refused" || mode == "failed") {
					return files.Publication{Path: p, Started: mode == "failed", Err: errors.New("injected native failure")}
				}
				pub := v.PublishMapped(ctx, n, p, names)
				if p == "b.md" {
					pub.Err = errors.New("post-publication observation/close error")
					if mode == "unknown" {
						pub.Published = false
						pub.Uncertain = true
						pub.Saved = parser.Source{}
					}
				}
				return pub
			})
			for _, p := range []string{"a.md", "b.md", "c.md"} {
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				got, e := os.ReadFile(filepath.Join(f.root, p))
				if e != nil {
					t.Fatal(e)
				}
				expected := p + "\n"
				if p == "b.md" && (mode == "refused" || mode == "failed") {
					expected = p
				}
				if string(got) != expected || !states[p].observed || string(states[p].saved.Bytes()) != expected {
					t.Fatal(p, string(got), states[p])
				}
			}
			want := mode
			if mode == "published-error" {
				want = "written"
			}
			if states["a.md"].status != "written" || states["b.md"].status != want || states["c.md"].status != "written" || len(out.Errors) != 1 || out.Errors[0].Category != result.IO {
				t.Fatal(states, out)
			}
		})
	}
}

func TestApplyChangedDependencyReverseOrderAndCycleDeferred(t *testing.T) {
	for _, deps := range []map[string][]string{{"a.md": {"b.md"}}, {"b.md": {"a.md"}}, {"a.md": {"b.md"}, "b.md": {"a.md"}}} {
		ctx := context.Background()
		f := setupApplyFixture(t, ctx, deps)
		out := result.NewFix(false)
		states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
			if p == "b.md" {
				return files.Publication{Path: p, Err: errors.New("B publication failed")}
			}
			return v.PublishMapped(ctx, n, p, names)
		})
		for _, p := range []string{"a.md", "b.md", "c.md"} {
			//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
			got, _ := os.ReadFile(filepath.Join(f.root, p))
			expected := p + "\n"
			if p == "b.md" || len(deps[p]) > 0 {
				expected = p
			}
			if string(got) != expected {
				t.Fatal("unsafe coupled publication", deps, p, string(got))
			}
		}
		if states["c.md"].status != "written" {
			t.Fatal(states)
		}
	}
}

func TestApplyOriginalCancellationAndStickyExhaustion(t *testing.T) {
	for _, mode := range []string{"before", "after", "quota"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := setupApplyFixture(t, ctx, nil)
			out := result.NewFix(false)
			if mode == "before" {
				cancel()
			}
			called := 0
			states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
				called++
				pub := v.PublishMapped(ctx, n, p, names)
				if mode == "after" {
					cancel()
				} else if mode == "quota" {
					_ = f.budget.ChargeWork(parser.DefaultLimits().WorkUnits + 1)
				}
				return pub
			})
			want := 1
			if mode == "before" {
				want = 0
			}
			if called != want || len(out.Interruptions) == 0 {
				t.Fatal(called, out)
			}
			for _, p := range []string{"a.md", "b.md", "c.md"} {
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				got, _ := os.ReadFile(filepath.Join(f.root, p))
				expected := p
				if p == "a.md" && want == 1 {
					expected += "\n"
					if states[p].status != "written" || !states[p].observed {
						t.Fatal(states)
					}
				}
				if string(got) != expected {
					t.Fatal(p, string(got))
				}
			}
		})
	}
}

func TestApplyStaleIdenticalInodeAndConfigurationRefusal(t *testing.T) {
	for _, mode := range []string{"inode", "config", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := setupApplyFixture(t, ctx, nil)
			out := result.NewFix(false)
			states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
				if p == "a.md" {
					switch mode {
					case "inode":
						temp := filepath.Join(f.root, "replacement")
						put(t, temp, "a.md")
						if e := os.Rename(temp, names[p]); e != nil {
							t.Fatal(e)
						}
					case "config":
						put(t, filepath.Join(f.root, ".mdchk.yaml"), fixConfiguration()+"# changed\n")
					case "unchanged":
						put(t, names[p], "external current\n")
						return files.Publication{Path: p, NoChange: true}
					}
				}
				return v.PublishMapped(ctx, n, p, names)
			})
			if mode == "unchanged" {
				if string(states["a.md"].saved.Bytes()) != "external current\n" {
					t.Fatal(states)
				}
			} else if states["a.md"].status != "refused" || len(out.Errors) == 0 {
				t.Fatal(states, out)
			}
			if mode == "config" {
				for _, p := range []string{"a.md", "b.md", "c.md"} {
					//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
					got, _ := os.ReadFile(filepath.Join(f.root, p))
					if string(got) != p {
						t.Fatal("config mutation not bound", p, string(got))
					}
				}
			}
		})
	}
}

func TestApplyConfigurationSameReadBaselineAndAncestorAbsence(t *testing.T) {
	ctx := context.Background()
	t.Run("same-read-baseline", func(t *testing.T) {
		f := setupApplyFixture(t, ctx, nil)
		path := filepath.Join(f.root, ".mdchk.yaml")
		//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
		data, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		replacement := filepath.Join(f.root, "new-config")
		//nolint:gosec // Copy only the test-owned configuration into its synthetic replacement sibling.
		if e = os.WriteFile(replacement, data, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(replacement, path); e != nil {
			t.Fatal(e)
		}
		selected, e := files.SelectBounded(ctx, files.Options{WorkingDirectory: f.root, Paths: []string{"a.md"}}, f.budget)
		if e != nil {
			t.Fatal(e)
		}
		native, _, _, e := acquireFixWrite(ctx, f.inv, selected, files.AcquireOptions{Budget: f.budget})
		if native != nil {
			_ = native.Close()
		}
		if e == nil {
			t.Fatal("equal-byte replacement silently replaced configuration approval baseline")
		}
	})
	t.Run("ancestor-absence", func(t *testing.T) {
		root := project(t)
		child := filepath.Join(root, "child")
		put(t, filepath.Join(child, ".mdchk.yaml"), fixConfiguration())
		put(t, filepath.Join(child, "doc.md"), "hello")
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		registry, providers, caps, e := configuredProviders()
		if e != nil {
			t.Fatal(e)
		}
		selected, e := config.ResolveValidatedBounded(config.SelectionOptions{WorkingDirectory: child}, registry, providers, config.DiscoveryBudget{Context: ctx, Budget: budget})
		if e != nil {
			t.Fatal(e)
		}
		resolution, e := selected.Admit(caps)
		if e != nil {
			t.Fatal(e)
		}
		inv := &lintInvocation{selection: selected, plan: resolution.ExecutionPlan(), workingDirectory: child}
		targets, e := files.SelectBounded(ctx, files.Options{WorkingDirectory: child, Paths: []string{"doc.md"}}, budget)
		if e != nil {
			t.Fatal(e)
		}
		native, acquired, resources, e := acquireFixWrite(ctx, inv, targets, files.AcquireOptions{Budget: budget})
		if e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := native.Close(); e != nil {
				t.Error(e)
			}
		}()
		validator, e := fix.NewValidator(registry, rules.CorrectionCheckers())
		if e != nil {
			t.Fatal(e)
		}
		approved, e := validator.Validate(ctx, []fix.Target{{Path: "doc.md", Source: acquired.Documents[0].Source, Plan: inv.plan.CandidatePlan(), Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget, Resources: resources})
		if e != nil {
			t.Fatal(e)
		}
		put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
		pub := approved.PublishMapped(ctx, native, "doc.md", map[string]string{"doc.md": filepath.Join(child, "doc.md")})
		//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
		content, _ := os.ReadFile(filepath.Join(child, "doc.md"))
		if pub.Err == nil || pub.Published || string(content) != "hello" {
			t.Fatal("appeared ancestor definition not bound", pub, string(content))
		}
	})
}

func TestApplyJoinedRuntimeAndIOPreserveBoth(t *testing.T) {
	ctx := context.Background()
	f := setupApplyFixture(t, ctx, nil)
	out := result.NewFix(false)
	states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
		pub := v.PublishMapped(ctx, n, p, names)
		pub.Err = errors.Join(context.Canceled, errors.New("native close failed"))
		return pub
	})
	if states["a.md"].status != "written" || states["b.md"].status != "cancelled" || len(out.Errors) != 1 || len(out.Interruptions) == 0 {
		t.Fatal(states, out)
	}
	engine.FinalizeLintResult(&out)
	if lintExit(out) != 13 {
		t.Fatal(out)
	}
}

func TestApplyUnknownSavedUnavailableAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := setupApplyFixture(t, ctx, nil)
	out := result.NewFix(false)
	states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
		pub := v.PublishMapped(ctx, n, p, names)
		pub.Published = false
		pub.Uncertain = true
		pub.Saved = parser.Source{}
		pub.Err = errors.Join(context.Canceled, errors.New("native saved observation failed"))
		cancel()
		return pub
	})
	if states["a.md"].status != "unknown" || states["a.md"].observed || states["a.md"].saved != (parser.Source{}) {
		t.Fatal("invented saved authority")
	}
	found := false
	for _, c := range out.Coverage {
		found = found || c.Path == "a.md" && c.Reason == "saved-view-unavailable"
	}
	if !found || len(out.Errors) != 1 {
		t.Fatal(out)
	}
}

func TestApplyOwnedLeftoverSurvivesReportingDeadline(t *testing.T) {
	out := result.NewFix(false)
	before := result.Snapshot{View: "original", SHA256: strings.Repeat("a", 64), SizeBytes: 3}
	candidate := result.Snapshot{View: "candidate", SHA256: strings.Repeat("b", 64), SizeBytes: 4}
	out.Files = append(out.Files, result.File{Path: "doc.md", Analysis: "failed", WriteStatus: "failed", ReturnStatus: "not-requested", InputSnapshot: &before, Leftovers: []string{strings.Repeat("s", 255)}, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{"change-1"}})
	out.Changes = append(out.Changes, result.Change{ID: "change-1", Path: "doc.md", Disposition: "proposed", Before: before, Candidate: candidate, Rules: []string{"MD047"}, EditCount: 1})
	out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: "cleanup-failed", Scope: "operation", Path: "doc.md", Message: "owned stage cleanup failed"})
	engine.FinalizeLintResult(&out)
	for _, name := range []string{strings.Repeat("s", 255), ".mdchk-\x1b\n雪"} {
		out.Files[0].Leftovers = []string{name}
		for _, format := range []string{"json", "text"} {
			for _, deadline := range []int64{1, 2000000000} {
				var stdout strings.Builder
				if code := reportResult(out, format, &stdout, map[string]int64{"reporting-time": deadline}); code != 13 {
					t.Fatal(code, stdout.String())
				}
				if format == "json" {
					got := validateEmitted(t, t.TempDir(), []byte(stdout.String()))
					if len(got.Files) != 1 || len(got.Files[0].Leftovers) != 1 || got.Files[0].Leftovers[0] != name || len(got.Changes) != 1 {
						t.Fatal(got)
					}
				} else {
					expected := "owned staging leftover for doc.md: " + textField(name) + "\n"
					if !strings.Contains(stdout.String(), expected) || strings.ContainsRune(stdout.String(), '\x1b') {
						t.Fatal(stdout.String())
					}
				}
			}
		}
	}
	out.Files[0].Leftovers = []string{strings.Repeat("s", 255)}
	full, err := reportSize(context.Background(), out, "text")
	if err != nil {
		t.Fatal(err)
	}
	out.Files[0].Leftovers = nil
	without, err := reportSize(context.Background(), out, "text")
	if err != nil {
		t.Fatal(err)
	}
	expectedDelta := int64(len("owned staging leftover for doc.md: ") + 255 + 1)
	if full-without != expectedDelta {
		t.Fatalf("mandatory text size delta %d want %d", full-without, expectedDelta)
	}
}

func TestApplySavedOverlayAggregateAndStickyLimit(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint("mixed-", mixed), func(t *testing.T) {
			ctx := context.Background()
			contents := map[string]string{}
			if mixed {
				contents["a.md"] = strings.Repeat("a", 599999)
			}
			f := setupApplyFixtureContents(t, ctx, nil, contents)
			f.inv.options.limits = map[string]int64{"total-content-bytes": 1 << 20}
			out := result.NewFix(false)
			states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, p string, names map[string]string) files.Publication {
				if mixed && p == "a.md" {
					return v.PublishMapped(ctx, n, p, names)
				}
				body := strings.Repeat("z", 600000)
				if p == "c.md" {
					body = "small"
				}
				put(t, names[p], body)
				return files.Publication{Path: p, NoChange: true}
			})
			retained := 0
			for _, state := range states {
				if state.observed {
					retained += state.saved.Len()
				}
			}
			if retained != 600000 || states["b.md"].observed || states["c.md"].observed || states["b.md"].saved != (parser.Source{}) || states["c.md"].saved != (parser.Source{}) {
				t.Fatalf("retained=%d states admitted b=%t c=%t", retained, states["b.md"].observed, states["c.md"].observed)
			}
			if mixed && (states["a.md"].status != "written" || states["a.md"].snapshot == nil || states["a.md"].snapshot.SizeBytes != 600000) {
				t.Fatal("earlier publication evidence lost")
			}
			coverage := map[string]bool{}
			for _, c := range out.Coverage {
				if c.Reason == "saved-view-unavailable" {
					coverage[c.Path] = true
				}
			}
			if len(out.Interruptions) == 0 || !coverage["b.md"] || !coverage["c.md"] {
				t.Fatal(out)
			}
			current, e := os.Stat(filepath.Join(f.root, "c.md"))
			if e != nil || current.Size() != 5 {
				t.Fatal("small later target should fit a reset allowance", e)
			}
		})
	}
}

package integration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func contractScope(t *testing.T, text string, settings Settings, profiles ...parser.ParseOptions) (*Scope, Document, *resource.Operation) {
	t.Helper()
	ctx := context.Background()
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsing := parser.ParseOptions{}
	if len(profiles) > 0 {
		parsing = profiles[0]
	}
	policy, _ := resource.NewSettings(parsing, "README.md", "github", "integration-contract-v1")
	key, _ := resource.NewKey("guide/document.md")
	source := parser.NewSource([]byte(text))
	op, err := resource.NewOperation(ctx, budget, root, policy, []resource.Target{{ID: "document.md", Source: source, ResourceKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewScope(ctx, op, view, settings)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.Document(ctx, "document.md")
	if err != nil {
		t.Fatal(err)
	}
	return s, doc, op
}

func TestExplicitContextPolicyPreservesParsingProfiles(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, metadata := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				frontmatter, _ := parser.NewFrontmatterOptions(metadata, nil)
				profile := parser.ParseOptions{GFM: gfm, Frontmatter: frontmatter}
				s, doc, operation := contractScope(t, "{python}", Settings{QuartoEnabled: enabled}, profile)
				decision, err := s.QuartoHeader(context.Background(), doc, contractLocation(t, doc.Source(), 0, 8), false)
				want := OrdinaryFence
				if enabled {
					want = SupportedCandidate
				}
				if err != nil || decision.Class() != want || operation.Settings().ParseOptions() != profile {
					t.Fatal("explicit policy coupled to flavor/frontmatter or changed profile", gfm, metadata, enabled, decision, err)
				}
			}
		}
	}
}
func contractLocation(t *testing.T, source parser.Source, start, end int) Location {
	t.Helper()
	span, err := source.Span(start, end)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := NewLocation(source, span)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// These are executable candidate-policy oracles. A passing table is deliberately
// NOT a claim that a document's fenced-node collection has been recognized.
func TestQuartoCandidatePolicyBoundaries(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, tc := range []struct {
			raw             string
			protected       bool
			class           HeaderClass
			language, label string
		}{
			{"{r}", false, SupportedCandidate, "r", ""},
			{"{python}", false, SupportedCandidate, "python", ""},
			{"{julia}", false, SupportedCandidate, "julia", ""},
			{"{r chart}", false, SupportedCandidate, "r", "chart"},
			{"{r, chart, echo=FALSE, eval=FALSE}", false, SupportedCandidate, "r", "chart"},
			{"{r label=\"a b\"}", false, SupportedCandidate, "r", "a b"},
			{"{r label = 'a b', eval = FALSE}", false, SupportedCandidate, "r", "a b"},
			{"{r, label='é-図', echo=TRUE}", false, SupportedCandidate, "r", "é-図"},
			{"{r,}", false, SupportedCandidate, "r", ""},
			{"python", false, OrdinaryFence, "", ""},
			{"{{python}}", false, EscapedExample, "", ""},
			{"{{python}", false, UnsupportedCell, "", ""},
			{"{ r}", false, UnsupportedCell, "", ""},
			{"{python}", true, EscapedExample, "", ""},
			{"{bash}", false, UnsupportedCell, "", ""},
			{"{R}", false, UnsupportedCell, "", ""},
			{"{python label=x}", false, UnsupportedCell, "python", ""},
			{"{r label=paste('a','b')}", false, UnsupportedCell, "r", ""},
			{"{r one label='two'}", false, UnsupportedCell, "r", ""},
			{"{r label=9}", false, UnsupportedCell, "r", ""},
			{"{r label='unterminated}", false, UnsupportedCell, "r", ""},
			{"{r, echo=system('id')}", false, UnsupportedCell, "r", ""},
			{"{r label='x'echo=FALSE}", false, UnsupportedCell, "r", ""},
		} {
			t.Run(tc.raw+"/"+map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
				s, doc, _ := contractScope(t, tc.raw, Settings{QuartoEnabled: enabled})
				got, err := s.QuartoHeader(context.Background(), doc, contractLocation(t, doc.Source(), 0, len(tc.raw)), tc.protected)
				if err != nil {
					t.Fatal(err)
				}
				class, language, label := tc.class, tc.language, tc.label
				if !enabled {
					class, language, label = OrdinaryFence, "", ""
				}
				if got.Class() != class || got.Language() != language || got.Label().Text() != label {
					t.Fatalf("policy=%s/%q/%q want=%s/%q/%q", got.Class(), got.Language(), got.Label().Text(), class, language, label)
				}
				actual, err := s.Quarto(context.Background(), doc)
				if err != nil {
					t.Fatal(err)
				}
				want := Unavailable
				if !enabled {
					want = Inactive
				}
				if actual.Status() != want {
					t.Fatal("candidate policy invented authoritative cells", actual)
				}
			})
		}
	}
}

func TestQuartoDeclaredEffectiveLabelPolicy(t *testing.T) {
	s, doc, _ := contractScope(t, "雪 {r old label}\r\n#| label: ''\r\n", Settings{QuartoEnabled: true})
	headerLoc := contractLocation(t, doc.Source(), 4, 17)
	bodyLoc := contractLocation(t, doc.Source(), 19, 30)
	absent, _ := NewLabel(LabelAbsent, "", Location{})
	header, _ := NewLabel(LabelString, "old label", headerLoc)
	for _, tc := range []struct {
		name           string
		kind           LabelKind
		text           string
		status         Status
		effective      string
		missing, known bool
	}{
		{"absent", LabelAbsent, "", Available, "old label", false, true},
		{"blank", LabelString, "", Available, "", true, true},
		{"whitespace", LabelString, "\u2003", Available, "\u2003", true, true},
		{"exact", LabelString, "É+X", Available, "É+X", false, true},
		{"null", LabelNonString, "", Unavailable, "", false, false},
		{"duplicate", LabelAmbiguous, "", Unavailable, "", false, false},
		{"tag", LabelUnknown, "", Unavailable, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := absent
			if tc.kind != LabelAbsent {
				var err error
				body, err = NewLabel(tc.kind, tc.text, bodyLoc)
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.ResolveLabels(context.Background(), doc, header, body)
			if err != nil {
				t.Fatal(err)
			}
			missing, known := got.Effective().Missing()
			if got.Status() != tc.status || got.Effective().Text() != tc.effective || missing != tc.missing || known != tc.known {
				t.Fatal("effective label/precedence", got, missing, known)
			}
			if got.Header().Text() != "old label" || got.Header().Location() != headerLoc || got.Body() != body {
				t.Fatal("overridden declaration or provenance lost", got)
			}
		})
	}
	foreign := parser.NewSource(doc.Source().Bytes())
	foreignLabel, _ := NewLabel(LabelString, "different", contractLocation(t, foreign, 0, 3))
	if _, err := s.ResolveLabels(context.Background(), doc, header, foreignLabel); !errors.Is(err, ErrContract) {
		t.Fatal("equal-byte foreign Source accepted", err)
	}
}

func TestQuartoAmbiguousDeclarationsRemainIndependent(t *testing.T) {
	raw := "{r 'not a positional literal'}"
	// Unsupported syntax is not a missing label or authoritative empty cell.
	s, doc, _ := contractScope(t, raw, Settings{QuartoEnabled: true})
	h, err := s.QuartoHeader(context.Background(), doc, contractLocation(t, doc.Source(), 0, len(raw)), false)
	if err != nil || h.Class() != UnsupportedCell || h.Label().Kind() != LabelUnknown {
		t.Fatal("unsupported header became known absent", h, err)
	}
	raw = "{r label='old label', label='new'}"
	s, doc, _ = contractScope(t, raw, Settings{QuartoEnabled: true})
	h, err = s.QuartoHeader(context.Background(), doc, contractLocation(t, doc.Source(), 0, len(raw)), false)
	if err != nil || h.Class() != UnsupportedCell || h.Label().Kind() != LabelAmbiguous {
		t.Fatal("duplicate labels became clean", h, err)
	}
	declared, err := h.DeclaredLabels(context.Background())
	if err != nil || len(declared) != 2 || declared[0].Text() != "old label" || declared[1].Text() != "new" {
		t.Fatal("reliable duplicate declarations lost", declared, err)
	}
	declared[0] = Label{}
	again, _ := h.DeclaredLabels(context.Background())
	if again[0].Text() != "old label" {
		t.Fatal("declaration slice alias")
	}
	body, _ := NewLabel(LabelString, "body", contractLocation(t, doc.Source(), 0, len(raw)))
	decision, err := s.ResolveLabels(context.Background(), doc, h.Label(), body)
	if err != nil || decision.Status() != Available || decision.Effective().Text() != "body" || decision.DeclarationsComplete() {
		t.Fatal("effective label erased independent declaration uncertainty", decision, err)
	}
}

func TestIntegrationActualCombinedControls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opctx, end := context.WithTimeout(context.Background(), time.Second)
		defer end()
		root, _ := resource.NewRoot(resource.SuppliedRoot)
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "controls")
		source := parser.NewSource([]byte("{r}"))
		op, err := resource.NewOperation(opctx, budget, root, policy, []resource.Target{{ID: "a.md", Source: source}})
		if err != nil {
			t.Fatal(err)
		}
		view, _ := op.NewView(resource.Original, nil)
		s, err := NewScope(context.Background(), op, view, Settings{QuartoEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		doc, _ := s.Document(context.Background(), "a.md")
		quotaErr := budget.ChargeWork(budget.Limits().WorkUnits + 1)
		var want *parser.LimitExceeded
		if !errors.As(quotaErr, &want) {
			t.Fatal(quotaErr)
		}
		time.Sleep(2 * time.Second)
		caller, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := s.Quarto(caller, doc)
		var actual *parser.LimitExceeded
		if got.Status() != Unavailable || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &actual) || *actual != *want {
			t.Fatal("independent actual controls collapsed", got, err)
		}
	})
}

func TestEqualByteForeignProvenanceDoesNotBorrowOwnerAllowance(t *testing.T) {
	// A large selected source leaves only a small working allowance after its
	// one charged admission scan. Foreign equal-byte provenance must be rejected
	// by owner identity, not revalidated by scanning/copying whole source bytes.
	// This asserts ownership and deterministic charged work, not elapsed time.
	source := parser.NewSource([]byte("{r}" + strings.Repeat("x", 256<<10)))
	limits := parser.DefaultLimits()
	limits.WorkUnits = int64(source.Len()) + 512
	budget, _ := parser.NewOperationBudget(limits)
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "owner-cost")
	op, err := resource.NewOperation(context.Background(), budget, root, policy, []resource.Target{{ID: "a.md", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := op.NewView(resource.Original, nil)
	scope, err := NewScope(context.Background(), op, view, Settings{QuartoEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	document, err := scope.Document(context.Background(), "a.md")
	if err != nil {
		t.Fatal(err)
	}
	foreign := parser.NewSource(source.Bytes())
	location := contractLocation(t, foreign, 0, 3)
	if foreign.Token() == source.Token() {
		t.Fatal("independent Source owners collapsed")
	}
	for range 8 {
		if _, err := scope.QuartoHeader(context.Background(), document, location, false); !errors.Is(err, ErrContract) {
			t.Fatal("foreign equal-byte source borrowed selected identity", err)
		}
	}
	actual, err := scope.QuartoHeader(context.Background(), document, contractLocation(t, source, 0, 3), false)
	if err != nil || actual.Class() != SupportedCandidate {
		t.Fatal("foreign rejection scanned/charged selected bytes again", actual, err)
	}
	if (resource.View{}).TargetCount() != 0 || view.TargetCount() != 1 {
		t.Fatal("target-count identity metadata")
	}
}

func TestIntegrationScopeOwnershipAndActualStops(t *testing.T) {
	s, doc, op := contractScope(t, "{python}", Settings{QuartoEnabled: true})
	foreign, _ := resource.NewOperation(context.Background(), op.Budget(), op.Root(), op.Settings(), op.Targets())
	if _, err := NewScope(context.Background(), foreign, doc.View(), Settings{}); !errors.Is(err, ErrContract) {
		t.Fatal("same-root/budget/targets wrong operation accepted", err)
	}
	if (*resource.Operation)(nil).OwnsView(doc.View()) || op.OwnsView(resource.View{}) {
		t.Fatal("nil/zero ownership")
	}
	view, err := op.NewView(resource.Candidate, []resource.Replacement{{TargetID: doc.ID(), Source: parser.NewSource([]byte("{python}"))}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewScope(context.Background(), op, view, Settings{QuartoEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Quarto(context.Background(), doc); !errors.Is(err, ErrContract) {
		t.Fatal("foreign exact view accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limit := op.Budget().ChargeWork(op.Budget().Limits().WorkUnits + 1)
	got, err := s.Quarto(ctx, doc)
	var actual *parser.LimitExceeded
	if got.Status() != Unavailable || !errors.Is(err, context.Canceled) || !errors.As(err, &actual) || !errors.Is(err, limit) && actual.Name != "work-units" {
		t.Fatal("actual independent controls lost", got, err)
	}
	if _, err := s.Quarto(nil, doc); !errors.Is(err, ErrContract) { //nolint:staticcheck // Exercise the rejected nil-context contract.
		t.Fatal("nil context")
	}
}

func TestMkDocsStaticPolicyTransitions(t *testing.T) {
	s, _, _ := contractScope(t, "markdown request unrelated to config", Settings{MkDocs: MkDocsPolicy{Enabled: true, ConfigPath: "site/mkdocs.yml", CheckOmitted: true}})
	root, err := s.PlanDocsRoot(context.Background(), parser.Some("pages"))
	if err != nil || root.Name() != "site/pages" {
		t.Fatal("config-relative docs root", root, err)
	}
	defaultRoot, err := s.PlanDocsRoot(context.Background(), parser.Optional[string]{})
	if err != nil || defaultRoot.Name() != "site/docs" {
		t.Fatal("default docs root", defaultRoot, err)
	}
	if root.ResourceRoot() != s.operation.Root() {
		t.Fatal("minted a new resource root")
	}
	explicitEmpty, err := s.PlanDocsRoot(context.Background(), parser.Some(""))
	if err != nil || explicitEmpty.Name() != "site" {
		t.Fatal("literal empty docs_dir treated as absent", explicitEmpty, err)
	}
	literal, err := s.PlanDocsRoot(context.Background(), parser.Some("pages#part?x"))
	if err != nil || literal.Name() != "site/pages#part?x" {
		t.Fatal("literal config path treated as URL", literal, err)
	}
	for _, tc := range []struct {
		key  string
		want bool
	}{{"site/pages/guide.md", true}, {"site/pages-evil/guide.md", false}, {"site/guide.md", false}} {
		key, _ := resource.NewKey(tc.key)
		got, err := s.WithinDocsRoot(context.Background(), root, key)
		if err != nil || got != tc.want {
			t.Fatal("docs subroot guard", tc, got, err)
		}
	}
	for _, dir := range []string{"../../escape", "/native", "C:/native", `\\server\share`, "https://site", "removed\n/../pages"} {
		if _, err := s.PlanDocsRoot(context.Background(), parser.Some(dir)); !errors.Is(err, ErrContract) {
			t.Fatal("unsafe docs directory", dir, err)
		}
	}
	source := parser.NewSource([]byte("雪 nav:\r\n - Guide: guide.md\r\n"))
	loc := contractLocation(t, source, 11, 27)
	for _, shape := range []NavShape{NavAbsent, NavNull, NavList, NavUnsupported} {
		nav, err := s.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), shape, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := AutomaticNav
		if shape == NavList {
			want = ManualNav
		} else if shape == NavUnsupported {
			want = UnknownNav
		}
		if nav.Mode() != want {
			t.Fatal("absent/null versus manual empty collapsed", shape, nav.Mode())
		}
		allowed, err := s.CanCheckOmissions(context.Background(), nav, EligibleInventory{}, true)
		if err != nil || allowed {
			t.Fatal("partial/queried inventory granted omissions", allowed, err)
		}
		// Private trusted-producer seam simulation checks the policy prerequisite,
		// not a claim that a source inventory has been acquired in this issue.
		allowed, err = s.CanCheckOmissions(context.Background(), nav, EligibleInventory{scope: s, source: source, complete: true}, true)
		if err != nil || allowed != (shape == NavList) {
			t.Fatal("omission prerequisites", shape, allowed, err)
		}
	}
	items := []NavItem{{Kind: NavSection, Title: "Section", Location: loc, Children: []NavItem{{Kind: NavPath, Title: "Guide", Destination: "guide.md#ignored", Location: loc}}}}
	nav, err := s.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), NavList, items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].Children[0].Destination = "mutated"
	entries, err := nav.Entries(context.Background(), s)
	if err != nil || entries[0].Children[0].Destination != "guide.md#ignored" {
		t.Fatal("input nav alias", entries, err)
	}
	entries[0].Children[0].Title = "mutated"
	again, _ := nav.Entries(context.Background(), s)
	if again[0].Children[0].Title != "Guide" {
		t.Fatal("output nav alias")
	}
	foreign := parser.NewSource(source.Bytes())
	bad := items
	bad[0].Location = contractLocation(t, foreign, 11, 27)
	if _, err := s.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), NavList, bad); !errors.Is(err, ErrContract) {
		t.Fatal("foreign config provenance accepted", err)
	}
	actual, err := s.MkDocs(context.Background())
	if err != nil || actual.Status() != Unavailable || actual.ConfigSource() != (parser.Source{}) {
		t.Fatal("policy DTO invented acquired MkDocs context", actual, err)
	}
}

func TestMkDocsAbsoluteAndDynamicPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s, _, _ := contractScope(t, "", Settings{MkDocs: MkDocsPolicy{Enabled: true, CheckAbsolute: enabled}})
		root, _ := s.PlanDocsRoot(context.Background(), parser.Optional[string]{})
		for _, destination := range []string{"/guide.md#part", "//remote/guide.md", "guide.html?x#part", "file:///native", "C:/native"} {
			got, err := s.LeadingSlash(context.Background(), root, destination)
			if err != nil {
				t.Fatal(err)
			}
			want := destination
			skip := false
			if destination == "/guide.md#part" {
				if enabled {
					want = "guide.md#part"
				} else {
					skip = true
				}
			}
			if got.Skipped() != skip || got.Destination() != want {
				t.Fatal("absolute policy rewrote host/site/html/remote identity", destination, got)
			}
		}
	}
	s, _, _ := contractScope(t, "", Settings{})
	for _, tc := range []struct {
		plugins                             []Plugin
		hooks, inherit, tags, dynamic, want bool
	}{
		{want: true}, {plugins: []Plugin{{Name: "search"}}, want: true}, {plugins: []Plugin{{Name: "custom", Disabled: true}}, want: true},
		{plugins: []Plugin{{Name: "custom"}}}, {hooks: true}, {inherit: true}, {tags: true}, {dynamic: true},
	} {
		got, err := s.StaticNavigation(context.Background(), tc.plugins, tc.hooks, tc.inherit, tc.tags, tc.dynamic)
		if err != nil || got != tc.want {
			t.Fatal("dynamic policy", tc, got, err)
		}
	}
	actual, err := s.MkDocs(context.Background())
	if err != nil || actual.Status() != Inactive {
		t.Fatal("default MkDocs activation", actual, err)
	}
}

func TestMkDocsOmissionActivationAndAuthority(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, omitted := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%v/omitted=%v", enabled, omitted), func(t *testing.T) {
				s, _, _ := contractScope(t, "", Settings{MkDocs: MkDocsPolicy{Enabled: enabled, CheckOmitted: omitted}})
				source := parser.NewSource([]byte("nav: []"))
				definition := contractLocation(t, source, 0, source.Len())
				manual, err := s.Navigation(context.Background(), definition, NavList, nil)
				if err != nil {
					t.Fatal(err)
				}
				// Trusted-producer prerequisite simulation only; no YAML or eligible
				// inventory acquisition is claimed by this shared policy test.
				exact := EligibleInventory{scope: s, source: source, complete: true}
				got, err := s.CanCheckOmissions(context.Background(), manual, exact, true)
				if err != nil || got != (enabled && omitted) {
					t.Fatalf("omission activation: allowed=%v want=%v err=%v", got, enabled && omitted, err)
				}
				actual, err := s.MkDocs(context.Background())
				wantStatus := Unavailable
				if !enabled {
					wantStatus = Inactive
				}
				if err != nil || actual.Status() != wantStatus {
					t.Fatal("provider activation disagrees", actual, err)
				}
				for _, shape := range []NavShape{NavAbsent, NavNull, NavUnsupported} {
					nav, err := s.Navigation(context.Background(), definition, shape, nil)
					if err != nil {
						t.Fatal(err)
					}
					if got, err := s.CanCheckOmissions(context.Background(), nav, exact, true); err != nil || got {
						t.Fatal("nonmanual nav permitted omissions", shape, got, err)
					}
				}
				if got, err := s.CanCheckOmissions(context.Background(), manual, exact, false); err != nil || got {
					t.Fatal("dynamic context permitted omissions", got, err)
				}
				foreignScope, _, _ := contractScope(t, "", Settings{})
				for _, inventory := range []EligibleInventory{{}, {scope: s, source: source}, {scope: foreignScope, source: source, complete: true}, {scope: s, source: parser.NewSource(source.Bytes()), complete: true}} {
					if got, err := s.CanCheckOmissions(context.Background(), manual, inventory, true); err != nil || got {
						t.Fatal("partial/foreign inventory permitted omissions", inventory, got, err)
					}
				}
				foreignNav, err := foreignScope.Navigation(context.Background(), definition, NavList, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.CanCheckOmissions(context.Background(), foreignNav, exact, true); !errors.Is(err, ErrContract) {
					t.Fatal("foreign navigation accepted", err)
				}
			})
		}
	}
}

func TestMkDocsOmissionActualControlsBeforeActivation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, omitted := range []bool{false, true} {
			for _, stop := range []string{"cancel", "deadline", "quota", "compound"} {
				t.Run(fmt.Sprintf("enabled=%v/omitted=%v/%s", enabled, omitted, stop), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						opctx, end := context.WithTimeout(context.Background(), time.Second)
						defer end()
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						root, _ := resource.NewRoot(resource.SuppliedRoot)
						policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "omission-controls")
						op, err := resource.NewOperation(opctx, budget, root, policy, nil)
						if err != nil {
							t.Fatal(err)
						}
						view, err := op.NewView(resource.Original, nil)
						if err != nil {
							t.Fatal(err)
						}
						s, err := NewScope(context.Background(), op, view, Settings{MkDocs: MkDocsPolicy{Enabled: enabled, CheckOmitted: omitted}})
						if err != nil {
							t.Fatal(err)
						}
						source := parser.NewSource([]byte("nav: []"))
						manual, err := s.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), NavList, nil)
						if err != nil {
							t.Fatal(err)
						}
						exact := EligibleInventory{scope: s, source: source, complete: true}
						var wantQuota *parser.LimitExceeded
						if stop == "quota" || stop == "compound" {
							if err := budget.ChargeWork(budget.Limits().WorkUnits + 1); !errors.As(err, &wantQuota) {
								t.Fatal("no actual quota", err)
							}
						}
						if stop == "deadline" || stop == "compound" {
							time.Sleep(2 * time.Second)
						}
						caller, cancel := context.WithCancel(context.Background())
						defer cancel()
						if stop == "cancel" || stop == "compound" {
							cancel()
						}
						got, err := s.CanCheckOmissions(caller, manual, exact, true)
						if got || err == nil {
							t.Fatal("activation short-circuited actual stop", got, err)
						}
						if errors.Is(err, context.Canceled) != (stop == "cancel" || stop == "compound") || errors.Is(err, context.DeadlineExceeded) != (stop == "deadline" || stop == "compound") {
							t.Fatal("independent canonical controls lost", stop, err)
						}
						var actual *parser.LimitExceeded
						hasQuota := errors.As(err, &actual)
						if hasQuota != (wantQuota != nil) || hasQuota && *actual != *wantQuota {
							t.Fatal("exact observed quota lost", stop, err, wantQuota)
						}
					})
				})
			}
		}
	}
}

func TestNavigationCopiesBoundedAndForeignScope(t *testing.T) {
	s, _, op := contractScope(t, "", Settings{})
	source := parser.NewSource([]byte("nav: []"))
	loc := contractLocation(t, source, 0, source.Len())
	items := []NavItem{{Kind: NavSection, Location: loc}}
	items[0].Children = items // An arbitrary cyclic caller model cannot recurse forever.
	var limit *parser.LimitExceeded
	if _, err := s.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), NavList, items); !errors.As(err, &limit) || limit.Name != "syntax-depth" {
		t.Fatal("cyclic semantic model not bounded", err)
	}
	s2, _, _ := contractScope(t, "", Settings{})
	nav, err := s2.Navigation(context.Background(), contractLocation(t, source, 0, source.Len()), NavList, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nav.Entries(context.Background(), s); !errors.Is(err, ErrContract) {
		t.Fatal("foreign scope copy admitted", err)
	}
	if _, err := s.CanCheckOmissions(context.Background(), nav, EligibleInventory{}, true); !errors.Is(err, ErrContract) {
		t.Fatal("foreign navigation authority admitted", err)
	}
	if !reflect.DeepEqual(op.Settings().ParseOptions(), parser.ParseOptions{}) || op.Settings().IndexFile() != "README.md" {
		t.Fatal("context changed shared parser/index settings")
	}
	if _, err := NewLabel(LabelString, strings.Repeat("x", 1<<20+1), loc); !errors.Is(err, ErrContract) {
		t.Fatal("unbounded policy label")
	}
}

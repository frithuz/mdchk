package configscan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

const admissible = "version: 1\nflavor: commonmark\ndirectives:\n  enabled: false\n"

func validationRegistry(t *testing.T) *engine.Registry {
	t.Helper()
	r, e := engine.NewRegistry(engine.Catalog(), nil)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func runValidation(t *testing.T, root string) ValidationResult {
	t.Helper()
	return Validate(context.Background(), Options{Root: root, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
}
func TestValidationScopedPruningBeforeReads(t *testing.T) {
	for _, pattern := range []string{"**", "pruned/", ".mdchk.yaml", "pruned/.mdchk.yaml"} {
		t.Run(pattern, func(t *testing.T) {
			root := temp(t)
			fixture(t, root, config.Filename, admissible+"exclude: ['"+pattern+"']\n")
			fixture(t, root, "pruned/.mdchk.yaml", "invalid: [")
			fixture(t, root, "pruned/.gitignore", "!child/\n")
			fixture(t, root, "pruned/child/.mdchk.yaml", admissible)
			before := snapshot(t, root)
			opens := map[string]int{}
			r := validate(context.Background(), Options{Root: root, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess {
				return effectAccess{scanAccess: a, opening: func(p string, _ bool) { opens[p]++ }}
			})
			if !r.Scan.Complete || r.Scan.Interruption != nil {
				t.Fatalf("%+v", r)
			}
			if len(r.Outcomes) == 0 || r.Outcomes[0].Err != nil {
				t.Fatalf("root did not validate: %+v", r)
			}
			child := filepath.Join(root, "pruned")
			if pattern == "**" || pattern == "pruned/" {
				for p, n := range opens {
					if p == child || strings.HasPrefix(p, child+string(filepath.Separator)) {
						t.Fatalf("pruned source opened %s %d", p, n)
					}
				}
				if len(r.Scan.Skipped) != 1 || r.Scan.Skipped[0].Source != filepath.Join(root, config.Filename) || len(r.Scan.Skipped[0].Origins) != 1 {
					t.Fatalf("skips %+v", r.Scan.Skipped)
				}
			}
			if pattern == "pruned/.mdchk.yaml" && opens[filepath.Join(child, config.Filename)] != 0 {
				t.Fatal("excluded definition opened")
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("validation wrote fixtures")
			}
		})
	}
}
func TestValidationInvalidDefinitionNeverActivatesPolicy(t *testing.T) {
	invalid := []string{"version: [", admissible + "unknown: true\n", admissible + "rules:\n MD013: {enabled: false, length: -1}\n", strings.Replace(admissible, "commonmark", "gfm", 1) + "exclude: ['**']\nallow_nested_excludes: false\n", admissible + "rules:\n MD001: {enabled: true, allow_directives: false}\nexclude: ['**']\n"}
	for _, bad := range invalid {
		root := temp(t)
		fixture(t, root, config.Filename, admissible)
		fixture(t, root, "child/.mdchk.yaml", bad)
		fixture(t, root, "child/grand/.mdchk.yaml", admissible+"exclude: ['unused']\n")
		fixture(t, root, "sibling/.mdchk.yaml", admissible)
		r := runValidation(t, root)
		if !r.Scan.Complete || len(r.Scan.Errors) != 1 || len(r.Outcomes) != 4 {
			t.Fatalf("%+v bad=%q", r, bad)
		}
		for _, o := range r.Outcomes {
			if strings.Contains(o.Source.Path, "grand") && (o.Err != nil || len(o.Resolution.EffectivePermissions.NestedExcludes) != 0) {
				t.Fatalf("invalid state activated %+v", o)
			}
		}
	}
}
func TestValidationLocksOriginsAndIndependentSettings(t *testing.T) {
	root := temp(t)
	base := admissible + "allow_nested_excludes: false\nrules:\n MD013: {enabled: false, length: 99, allow_directives: false}\n"
	fixture(t, root, config.Filename, base+"exclude: ['root-only/']\n")
	fixture(t, root, "child/.mdchk.yaml", base)
	fixture(t, root, "child/good/.mdchk.yaml", "version: 1\nflavor: commonmark\ndirectives: {enabled: true}\nallow_nested_excludes: true\nexclude: []\nrules:\n MD013: {enabled: false, length: 77, allow_directives: true}\n")
	fixture(t, root, "child/bad/.mdchk.yaml", admissible+"exclude: ['**']\nrules:\n MD001: {enabled: false, allow_directives: false}\n")
	fixture(t, root, "child/bad/grand/.mdchk.yaml", admissible)
	r := runValidation(t, root)
	if !r.Scan.Complete || len(r.Scan.Errors) != 1 {
		t.Fatalf("%+v", r)
	}
	for _, o := range r.Outcomes {
		if strings.Contains(o.Source.Path, "/good/") {
			p := o.Resolution.EffectivePermissions
			if len(p.Directives) != 2 || len(p.NestedExcludes) != 2 || len(p.RuleDirectives["MD013"]) != 2 || o.Resolution.Rules["MD013"].Options["length"] != 77 {
				t.Fatalf("origins/settings %+v", o.Resolution)
			}
		}
		if strings.Contains(o.Source.Path, "/grand/") && len(o.Resolution.EffectivePermissions.RuleDirectives["MD001"]) != 0 {
			t.Fatal("policy-invalid file granted denial")
		}
	}
	var policy *config.PolicyError
	if !errors.As(r.Scan.Errors[0], &policy) || len(policy.Restrictions) != 2 {
		t.Fatalf("missing locks %+v", r.Scan.Errors)
	}
}

type denyAccess struct {
	scanAccess
	denied string
}

func (a denyAccess) open(p string, dir bool) (readHandle, error) {
	if p == a.denied {
		return nil, fs.ErrPermission
	}
	return a.scanAccess.open(p, dir)
}
func TestValidationUnreadableRoleAndSourceIdentity(t *testing.T) {
	for _, name := range []string{config.Filename, ".gitignore"} {
		t.Run(name, func(t *testing.T) {
			root := temp(t)
			fixture(t, root, config.Filename, admissible+"allow_nested_excludes: false\n")
			fixture(t, root, "child/"+name, "version: [")
			fixture(t, root, "child/grand/.mdchk.yaml", admissible)
			fixture(t, root, "sibling/.mdchk.yaml", admissible)
			denied := filepath.Join(root, "child", name)
			r := validate(context.Background(), Options{Root: root, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess { return denyAccess{a, denied} })
			if r.Scan.Complete || len(r.Scan.Errors) != 1 || IsDefinitionFailure(r.Scan.Errors[0]) != (name == config.Filename) {
				t.Fatalf("roles %+v", r)
			}
			for _, o := range r.Outcomes {
				if o.Source.Path == denied {
					t.Fatal("fabricated acquired snapshot")
				}
			}
			want := 3
			if name == ".gitignore" {
				want = 2
			}
			if len(r.Outcomes) != want {
				t.Fatalf("continued outcomes %+v", r)
			}
		})
	}
	root := temp(t)
	body := admissible + "unknown: true\r\n"
	fixture(t, root, config.Filename, body)
	r := runValidation(t, root)
	sum := sha256.Sum256([]byte(body))
	o := r.Outcomes[0]
	if o.Source.SHA256 != hex.EncodeToString(sum[:]) || o.Source.SizeBytes != int64(len(body)) {
		t.Fatalf("snapshot %+v", o)
	}
	var decode *config.DecodeError
	if !errors.As(o.Err, &decode) || decode.Origin.Position == nil || decode.Origin.Position.Line != 5 {
		t.Fatalf("source origin %v", o.Err)
	}
}
func TestValidationNoAboveRootAndScopedSibling(t *testing.T) {
	outer := temp(t)
	fixture(t, outer, config.Filename, "invalid: [")
	root := filepath.Join(outer, "project")
	fixture(t, root, "a/.mdchk.yaml", admissible+"exclude: ['nested/']\n")
	fixture(t, root, "a/nested/.mdchk.yaml", "invalid: [")
	fixture(t, root, "b/nested/.mdchk.yaml", admissible)
	r := runValidation(t, root)
	if !r.Scan.Complete || len(r.Scan.Errors) != 0 || len(r.Outcomes) != 2 || len(r.Scan.Skipped) != 1 {
		t.Fatalf("%+v", r)
	}
}
func TestValidationSharedResidualBudgetStopsBeforeProvider(t *testing.T) {
	root := temp(t)
	fixture(t, root, config.Filename, admissible)
	limits := parser.DefaultLimits()
	b, _ := parser.NewOperationBudget(limits)
	if e := b.ChargeWork(limits.WorkUnits - 1000); e != nil {
		t.Fatal(e)
	}
	r := Validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: b}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	var limit *parser.LimitExceeded
	if !errors.As(r.Scan.Interruption, &limit) || r.Scan.Complete || len(r.Scan.Errors) != 0 {
		t.Fatalf("residual work %+v", r)
	}
	b, _ = parser.NewOperationBudget(limits)
	if e := b.ChargeNodes(limits.TotalNodes); e != nil {
		t.Fatal(e)
	}
	r = Validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: b}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	if !errors.As(r.Scan.Interruption, &limit) || limit.Name != "total-nodes" {
		t.Fatalf("residual nodes %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = Validate(ctx, Options{Root: root, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	if !errors.Is(r.Scan.Interruption, context.Canceled) || len(r.Outcomes) != 0 {
		t.Fatalf("cancel %+v", r)
	}
}

func TestValidationAllExclusionOriginsAndAggregateBytes(t *testing.T) {
	root := temp(t)
	fixture(t, root, config.Filename, admissible+"exclude: ['child/target/.mdchk.yaml']\n")
	fixture(t, root, "child/.mdchk.yaml", admissible+"exclude: ['target/.mdchk.yaml']\n")
	fixture(t, root, "child/target/.mdchk.yaml", "invalid: [")
	r := runValidation(t, root)
	if !r.Scan.Complete || len(r.Outcomes) != 2 || len(r.Scan.Skipped) != 1 || len(r.Scan.Skipped[0].Origins) != 2 {
		t.Fatalf("origins %+v", r)
	}
	for _, extra := range []int64{0, 1} {
		root := temp(t)
		fixture(t, root, config.Filename, admissible)
		fixture(t, root, "child/.mdchk.yaml", admissible)
		limits := DefaultLimits()
		limits.SourceBytes = int64(len(admissible))
		limits.TotalSourceBytes = 2*int64(len(admissible)) - extra
		r := Validate(context.Background(), Options{Root: root, Limits: limits, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
		if extra == 0 {
			if !r.Scan.Complete || len(r.Outcomes) != 2 {
				t.Fatalf("exact %+v", r)
			}
		} else {
			var limit *parser.LimitExceeded
			if !errors.As(r.Scan.Interruption, &limit) || limit.Name != "total-config-bytes" || len(r.Outcomes) != 1 {
				t.Fatalf("one over %+v", r)
			}
		}
	}
}

func TestValidationProvidersOnceAndCancellationBeforeDescendants(t *testing.T) {
	root := temp(t)
	fixture(t, root, config.Filename, admissible+"rules: {MD095: {enabled: false, allowed_hosts: [example.org]}}\n")
	fixture(t, root, "child/.mdchk.yaml", admissible)
	calls := 0
	provider := engine.ValidationProviders{ValidateHostname: func(string) error { calls++; return nil }}
	r := Validate(context.Background(), Options{Root: root, Environment: environment(t)}, validationRegistry(t), provider, config.Capabilities{CommonMark: true})
	if !r.Scan.Complete || len(r.Scan.Errors) != 0 || calls != 1 {
		t.Fatalf("provider %d result %+v", calls, r)
	}
	limits := parser.DefaultLimits()
	b, _ := parser.NewOperationBudget(limits)
	if err := b.ChargeWork(limits.WorkUnits - 2000); err != nil {
		t.Fatal(err)
	}
	calls = 0
	r = Validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: b}, validationRegistry(t), provider, config.Capabilities{CommonMark: true})
	if r.Scan.Interruption == nil || calls != 0 {
		t.Fatalf("expensive callback before reservation %d %+v", calls, r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ValidateHostname = func(string) error { cancel(); return nil }
	opens := 0
	r = validate(ctx, Options{Root: root, Environment: environment(t)}, validationRegistry(t), provider, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess {
		return effectAccess{scanAccess: a, opening: func(p string, _ bool) {
			if strings.Contains(p, "/child") {
				opens++
			}
		}}
	})
	if !errors.Is(r.Scan.Interruption, context.Canceled) || opens != 0 || r.Outcomes[0].Resolution != nil || len(r.Scan.Errors) != 0 {
		t.Fatalf("canceled callback opens %d %+v", opens, r)
	}
}

func TestValidationSameBasenameExternalIgnoreKeepsIgnoreRole(t *testing.T) {
	root := temp(t)
	home := temp(t)
	fixture(t, root, ".git/config", "[core]\n")
	fixture(t, home, config.Filename, "ignored/\n")
	env := environment(t)
	fixture(t, home, "gitconfig", "[core]\n excludesFile = "+filepath.Join(home, config.Filename)+"\n")
	env["GIT_CONFIG_GLOBAL"] = filepath.Join(home, "gitconfig")
	denied := filepath.Join(home, config.Filename)
	r := validate(context.Background(), Options{Root: root, Environment: env}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess { return denyAccess{a, denied} })
	if r.Scan.Complete || len(r.Scan.Errors) != 1 || r.Scan.Errors[0].Role != IgnoreSource || IsDefinitionFailure(r.Scan.Errors[0]) || len(r.Outcomes) != 0 {
		t.Fatalf("role %+v", r)
	}
}

func TestValidationMatchingConsumesResidualOperation(t *testing.T) {
	root := temp(t)
	name := strings.Repeat("x", 200)
	fixture(t, root, config.Filename, admissible+"exclude: ['"+strings.Repeat("?", 200)+"']\n")
	fixture(t, root, name+"/.mdchk.yaml", admissible)
	observed := false
	// Find the narrow interval in which admission fits, but a single charged
	// standard-library match exceeds the residual shared operation. This avoids
	// coupling the assertion to the decoder's exact accounting constants.
	for remaining := int64(100000); remaining <= 500000; remaining += 1000 {
		limits := parser.DefaultLimits()
		b, _ := parser.NewOperationBudget(limits)
		if err := b.ChargeWork(limits.WorkUnits - remaining); err != nil {
			t.Fatal(err)
		}
		opens := 0
		r := validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: b}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess {
			return effectAccess{scanAccess: a, opening: func(p string, _ bool) {
				if strings.Contains(p, "/"+name) {
					opens++
				}
			}}
		})
		var limit *parser.LimitExceeded
		if len(r.Outcomes) == 1 && r.Outcomes[0].Resolution != nil && errors.As(r.Scan.Interruption, &limit) && limit.Name == "work-units" && limit.Observed-limit.Allowed > 1000 {
			if opens != 0 || len(r.Scan.Errors) != 0 {
				t.Fatalf("matching exhausted after child effects %+v", r)
			}
			observed = true
			break
		}
	}
	if !observed {
		t.Fatal("no shared matching interruption after successful admission")
	}
}

func TestValidationIndependentConfigurationByteCap(t *testing.T) {
	for _, markdownCap := range []int64{16, 2 << 20} {
		root := temp(t)
		fixture(t, root, config.Filename, admissible)
		limits := parser.DefaultLimits()
		limits.DocumentBytes = markdownCap
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		r := Validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: budget}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
		if !r.Scan.Complete || r.Scan.Interruption != nil || len(r.Scan.Errors) != 0 || len(r.Outcomes) != 1 || r.Outcomes[0].Resolution == nil {
			t.Fatalf("Markdown bytes %d altered independent configuration admission: %+v", markdownCap, r)
		}
	}
	root := temp(t)
	// An operator-raised configuration cap also replaces the legacy standalone
	// decoder cap, independently of the Markdown document cap.
	body := admissible + "#" + strings.Repeat("x", 1<<20) + "\n"
	fixture(t, root, config.Filename, body)
	limits := DefaultLimits()
	limits.SourceBytes = 2 << 20
	r := Validate(context.Background(), Options{Root: root, Environment: environment(t), Limits: limits}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	if !r.Scan.Complete || r.Scan.Interruption != nil || len(r.Scan.Errors) != 0 || r.Outcomes[0].Resolution == nil {
		t.Fatalf("raised config cap %+v", r)
	}
}

func TestValidationDecoderResourceLimitsInterruptBeforeDescendants(t *testing.T) {
	alias := "version: 1\nx0: &a0 text\n"
	for i := 1; i < 10; i++ {
		alias += fmt.Sprintf("x%d: &a%d {x: *a%d}\n", i, i, i-1)
	}
	for _, test := range []struct {
		name, body, limit string
		configure         func(*parser.Limits)
	}{
		{"flow-depth", "version: 1\nrules: {future: {x: " + strings.Repeat("[", 65) + "x" + strings.Repeat("]", 65) + "}}\n", "structured-input-depth", func(*parser.Limits) {}},
		{"alias-depth", alias, "syntax-depth", func(l *parser.Limits) { l.SyntaxDepth = 3 }},
		{"tokens", admissible, "document-nodes", func(l *parser.Limits) { l.DocumentNodes = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := temp(t)
			fixture(t, root, config.Filename, test.body)
			fixture(t, root, "child/.mdchk.yaml", admissible)
			limits := parser.DefaultLimits()
			test.configure(&limits)
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			opens := 0
			r := validate(context.Background(), Options{Root: root, Environment: environment(t), Budget: budget}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess {
				return effectAccess{scanAccess: a, opening: func(p string, _ bool) {
					if strings.Contains(p, "/child") {
						opens++
					}
				}}
			})
			var limit *parser.LimitExceeded
			if !errors.As(r.Scan.Interruption, &limit) || limit.Name != test.limit || r.Scan.Complete || len(r.Scan.Errors) != 0 || len(r.Outcomes) != 1 || r.Outcomes[0].Resolution != nil || opens != 0 {
				t.Fatalf("limit=%+v child opens=%d result=%+v", limit, opens, r)
			}
		})
	}
}

func TestValidationExcludedDefinitionSourceKinds(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "regular"} {
		t.Run(kind, func(t *testing.T) {
			root := temp(t)
			fixture(t, root, config.Filename, admissible+"exclude: ['child/.mdchk.yaml']\n")
			definition := filepath.Join(root, "child", config.Filename)
			fixture(t, root, "target", "invalid target source: [")
			switch kind {
			case "symlink":
				if e := os.MkdirAll(filepath.Dir(definition), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("../target", definition); e != nil {
					t.Fatal(e)
				}
			case "directory":
				fixture(t, root, "child/.mdchk.yaml/.mdchk.yaml", "invalid nested source: [")
			case "regular":
				fixture(t, root, "child/.mdchk.yaml", "invalid excluded source: [")
			}
			before := snapshot(t, root)
			opens := map[string]int{}
			r := validate(context.Background(), Options{Root: root, Environment: environment(t)}, validationRegistry(t), engine.ValidationProviders{}, config.Capabilities{CommonMark: true}, func(a scanAccess) scanAccess {
				return effectAccess{scanAccess: a, opening: func(p string, _ bool) { opens[p]++ }}
			})
			if !r.Scan.Complete || r.Scan.Interruption != nil || len(r.Scan.Errors) != 0 || len(r.Outcomes) != 1 || r.Outcomes[0].Err != nil {
				t.Fatalf("unexpected validation outcome %+v", r)
			}
			if len(r.Scan.Skipped) != 1 {
				t.Fatalf("duplicate source-kind skips: %+v", r.Scan.Skipped)
			}
			skip := r.Scan.Skipped[0]
			expectedReason := "configuration exclusion"
			if kind == "symlink" {
				expectedReason = "symlink"
			}
			if skip.Path != definition || skip.Definition != (kind == "regular") || skip.Reason != expectedReason {
				t.Fatalf("source kind %s classified as %+v", kind, skip)
			}
			for p, n := range opens {
				if p == definition || strings.HasPrefix(p, definition+string(filepath.Separator)) || p == filepath.Join(root, "target") {
					t.Fatalf("ineligible/pruned source opened %s %d", p, n)
				}
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("validation changed source bytes")
			}
		})
	}
}

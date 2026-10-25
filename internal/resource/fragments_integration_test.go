package resource

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type fragmentIntegrationFixture struct {
	service  *Service
	op       *Operation
	view     View
	source   parser.Source
	resolver *FragmentResolver
	id       string
}

func fragmentIntegrationSupplied(t *testing.T, key, index, raw string, destinations []string) fragmentIntegrationFixture {
	t.Helper()
	text := "# Part\n"
	for _, destination := range destinations {
		text += fmt.Sprintf("[query](%s)\n", destination)
	}
	source := parser.NewSource([]byte(text))
	root, err := NewRoot(SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := NewSettings(parser.ParseOptions{}, index, "github", "fragment-integration-v1")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	resourceKey, err := NewKey(key)
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewOperation(context.Background(), budget, root, settings, []Target{{ID: "selected", Source: source, ResourceKey: resourceKey}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeSupplied(context.Background(), budget, DefaultInputLimits(), []byte(raw), []SuppliedFile{{Key: resourceKey, Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewSuppliedReader(context.Background(), op, input, DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSuppliedService(reader, DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := NewNormalizer(view, DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	return fragmentIntegrationFixture{service, op, view, source, resolver, "selected"}
}
func fragmentIntegrationResolve(t *testing.T, resolver *FragmentResolver, source parser.Source, id, destination string, cross bool) FragmentResolution {
	t.Helper()
	whole, _ := source.Span(0, source.Len())
	text, _ := source.Text(whole)
	start := strings.Index(text, destination)
	if start < 0 {
		t.Fatalf("semantic destination %q absent from fixture source", destination)
	}
	span, err := source.Span(start, start+len(destination))
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewLink(id, "MD051", destination, source, span)
	if err != nil {
		t.Fatal(err)
	}
	query, err := resolver.Prepare(link)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(context.Background(), query, cross)
	if err != nil {
		t.Fatal(destination, err)
	}
	return resolution
}

func TestFragmentIntegrationApplicabilityPrecedence(t *testing.T) {
	// There is deliberately no MD057 registration or enabled flag in these
	// fixtures. Missing-file ownership cannot transfer to the fragment query.
	raw := `{"complete":false,"entries":[{"path":"docs/gone.md","kind":"missing"},{"path":"docs/no-content.md","kind":"file"},{"path":"docs/asset.bin","kind":"file","content":"# Part\n"},{"path":"docs/good.MD","kind":"file","content":"# Part\n"},{"path":"docs/uncertain.md","kind":"file","content":"<svg><title><span id=part>x</span></title></svg>\n"},{"path":"docs/file.md","kind":"file","content":"# Part\n"},{"path":"docs/sub","kind":"directory"},{"path":"docs/sub/index.md","kind":"directory"},{"path":"docs/sub/index.md/index.md","kind":"file","content":"# Part\n"},{"path":"docs/sub/README.md","kind":"file","content":"# Part\n"}]}`
	rows := []struct {
		destination string
		cross       bool
		want        FragmentOutcome
		existence   Existence
		coverage    int
	}{
		{"gone.md#part", true, FragmentInapplicable, Missing, 0},
		{"gone.md#%FF", true, FragmentInapplicable, Missing, 0},
		{"unknown.md#part", true, FragmentContextUnavailable, Unknown, 1},
		{"unknown.md#%FF", true, FragmentContextUnavailable, Unknown, 1},
		{"no-content.md#part", true, FragmentContextUnavailable, File, 1},
		{"no-content.md#%FF", true, FragmentContextUnavailable, File, 1},
		{"asset.bin#part", true, FragmentContextUnavailable, File, 1},
		{"asset.bin#%FF", true, FragmentContextUnavailable, File, 1},
		{"asset.bin#", true, DocumentReference, "", 0},
		{"good.MD#part", true, FragmentFound, File, 0},
		{"good.MD#Part", true, FragmentAbsentTarget, File, 0},
		{"good.MD#%FF", true, FragmentMalformed, File, 0},
		{"uncertain.md#%FF", true, FragmentContextUnavailable, File, 1},
		{"file.md/#%FF", true, FragmentInapplicable, File, 0},
		{"sub/#part", true, FragmentInapplicable, Directory, 0},
		{"good.MD#%FF", false, FragmentSkipped, "", 0},
		{"gone.md#part", false, FragmentSkipped, "", 0},
		{"unknown.md#part", false, FragmentSkipped, "", 0},
		{"https://example.invalid/a#part", true, FragmentSkipped, "", 0},
		{"//example.invalid/a#part", true, FragmentSkipped, "", 0},
		{"data:text/plain,x#part", true, FragmentSkipped, "", 0},
		{"good.MD", true, NoNamedQuery, "", 0},
	}
	for i, row := range rows {
		t.Run(fmt.Sprintf("%02d-%s", i, row.destination), func(t *testing.T) {
			f := fragmentIntegrationSupplied(t, "docs/source.md", "index.md", raw, []string{row.destination})
			type acquisition struct {
				key string
				cap Capability
			}
			var acquired []acquisition
			read := f.service.read
			f.service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
				acquired = append(acquired, acquisition{q.Key().String(), q.Capability()})
				return read(ctx, q, receipt)
			}
			got := fragmentIntegrationResolve(t, f.resolver, f.source, f.id, row.destination, row.cross)
			if got.Outcome() != row.want {
				t.Fatalf("outcome=%q want %q", got.Outcome(), row.want)
			}
			observations := got.Observations()
			if row.existence == "" {
				if len(observations) != 0 || len(acquired) != 0 {
					t.Fatalf("unneeded acquisition: %+v %+v", observations, acquired)
				}
			} else {
				if len(observations) == 0 || observations[len(observations)-1].Existence() != row.existence {
					t.Fatalf("independent final existence: %+v want %q", observations, row.existence)
				}
			}
			if strings.HasPrefix(row.destination, "asset.bin") {
				for _, a := range acquired {
					if a.cap != ExistenceOnly {
						t.Fatalf("binary payload acquisition: %+v", acquired)
					}
				}
			}
			if row.destination == "sub/#part" {
				for _, a := range acquired {
					if a.key != "docs/sub" && a.key != "docs/sub/index.md" {
						t.Fatalf("recursive/fallback acquisition: %+v", acquired)
					}
				}
			}
			owner, err := NewFragmentReport(f.resolver, result.Result{})
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.Append(f.view, []FragmentResolution{got}); err != nil {
				t.Fatal(err)
			}
			out, err := owner.Finish()
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Coverage) != row.coverage || len(out.Diagnostics) != 0 || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Files) != 0 {
				t.Fatalf("unexpected effects: %+v", out)
			}
		})
	}
}

func TestFragmentIntegrationFinalSelfIndexAndAliases(t *testing.T) {
	for _, key := range []string{"README.md", "docs/README.md"} {
		for _, destination := range []string{"#part", "?ignored#part", "README.md#part", "./README.md#part", "./#part", ".#part", "../docs/#part", "../docs#part"} {
			if key == "README.md" && strings.HasPrefix(destination, "../") {
				continue
			}
			t.Run(key+"/"+destination, func(t *testing.T) {
				f := fragmentIntegrationSupplied(t, key, "README.md", `{"entries":[{"path":"elsewhere/README.md","kind":"file","content":"# Part\n"}]}`, []string{destination})
				var acquisitions []string
				read := f.service.read
				f.service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
					acquisitions = append(acquisitions, q.Key().String())
					return read(ctx, q, receipt)
				}
				got := fragmentIntegrationResolve(t, f.resolver, f.source, f.id, destination, false)
				if got.Outcome() != FragmentFound {
					t.Fatal("final current-index identity skipped", got.Outcome())
				}
				index, ok := got.Anchors().Index()
				if !ok || index.Source() != f.source {
					t.Fatal("selected snapshot not retained", ok, index.Source())
				}
				for _, name := range acquisitions {
					if name != "docs" {
						t.Fatalf("self snapshot reopened or speculative index acquired: %v", acquisitions)
					}
				}
				if key == "docs/README.md" && (destination == "./#part" || destination == ".#part" || strings.HasPrefix(destination, "../docs")) && len(acquisitions) != 1 {
					t.Fatalf("bare/hinted directory not actually established: %v", acquisitions)
				}
			})
		}
	}
}

func TestFragmentIntegrationOneIndexMissingAndUnknown(t *testing.T) {
	for _, row := range []struct {
		raw      string
		want     FragmentOutcome
		wantKeys []string
	}{
		{`{"complete":true,"entries":[{"path":"docs/sub","kind":"directory"},{"path":"docs/sub/README.md","kind":"file","content":"# Part\n"}]}`, FragmentInapplicable, []string{"docs/sub", "docs/sub/index.md"}},
		{`{"entries":[{"path":"docs/sub","kind":"directory"},{"path":"docs/sub/README.md","kind":"file","content":"# Part\n"}]}`, FragmentContextUnavailable, []string{"docs/sub", "docs/sub/index.md"}},
		{`{"entries":[]}`, FragmentContextUnavailable, []string{"docs/sub"}},
	} {
		f := fragmentIntegrationSupplied(t, "docs/source.md", "index.md", row.raw, []string{"sub/#part"})
		var keys []string
		read := f.service.read
		f.service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			keys = append(keys, q.Key().String())
			return read(ctx, q, receipt)
		}
		got := fragmentIntegrationResolve(t, f.resolver, f.source, f.id, "sub/#part", true)
		if got.Outcome() != row.want || strings.Join(keys, "|") != strings.Join(row.wantKeys, "|") {
			t.Fatalf("one-index authority: outcome=%q keys=%v want %q %v", got.Outcome(), keys, row.want, row.wantKeys)
		}
	}
}

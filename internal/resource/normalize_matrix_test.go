package resource_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

// These are policy oracles for docs/guide.md, not expected values obtained by
// invoking another URL/path normalizer. Literal map keys are a separate domain.
type destinationCase struct {
	destination, key string
	class            r.ReferenceClass
	root, directory  bool
	same             bool
	fragment         r.FragmentStatus
	text             string
}

func normalizationMatrix() []destinationCase {
	return []destinationCase{
		{destination: "../README.md", class: r.Local, key: "README.md", fragment: r.FragmentAbsent},
		{destination: "%2e%2e/README.md", class: r.Local, key: "README.md", fragment: r.FragmentAbsent},
		{destination: "../../secret.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "%2e%2e/%2e%2e/secret.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "../../docs/guide.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "%252e%252e/a.md", class: r.Local, key: "docs/%2e%2e/a.md", fragment: r.FragmentAbsent},
		{destination: "a%252Fb.md", class: r.Local, key: "docs/a%2Fb.md", fragment: r.FragmentAbsent},
		{destination: "%252F", class: r.Local, key: "docs/%2F", fragment: r.FragmentAbsent},
		{destination: "a%20b.md", class: r.Local, key: "docs/a b.md", fragment: r.FragmentAbsent},
		{destination: "a+b.md", class: r.Local, key: "docs/a+b.md", fragment: r.FragmentAbsent},
		{destination: "a%2Bb.md", class: r.Local, key: "docs/a+b.md", fragment: r.FragmentAbsent},
		{destination: "a%23b.md#part", class: r.Local, key: "docs/a#b.md", fragment: r.FragmentValid, text: "part"},
		{destination: "a%3Fb.md?ignored=%#part", class: r.Local, key: "docs/a?b.md", fragment: r.FragmentValid, text: "part"},
		{destination: "a%25b.md", class: r.Local, key: "docs/a%b.md", fragment: r.FragmentAbsent},
		{destination: "a.md?ignored=%#part", class: r.Local, key: "docs/a.md", fragment: r.FragmentValid, text: "part"},
		{destination: "a.md#part?data", class: r.Local, key: "docs/a.md", fragment: r.FragmentValid, text: "part?data"},
		{destination: "a.md?%FF%GG", class: r.Local, key: "docs/a.md", fragment: r.FragmentAbsent},
		{destination: "a%2Fb", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a%2fb", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a%5Cb", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a%5cb", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a%", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "%GG", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "%FF.md", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "%ED%A0%80.md", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "a%00b.md", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "a%0Ab.md", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "a\x7fb.md", class: r.InvalidReference, fragment: r.FragmentAbsent},
		{destination: "a.md#%", class: r.Local, key: "docs/a.md", fragment: r.FragmentInvalid},
		{destination: "a.md#%GG", class: r.Local, key: "docs/a.md", fragment: r.FragmentInvalid},
		{destination: "a.md#%FF", class: r.Local, key: "docs/a.md", fragment: r.FragmentInvalid},
		{destination: "a.md#%ED%A0%80", class: r.Local, key: "docs/a.md", fragment: r.FragmentInvalid},
		{destination: "#%2520", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid, text: "%20"},
		{destination: "#a+b", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid, text: "a+b"},
		{destination: "#a%2Fb", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid, text: "a/b"},
		{destination: "#x#y", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid, text: "x#y"},
		{destination: "https:", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "HtTpS://example.test/a%GG#%FF", class: r.Ignored, fragment: r.FragmentUnchecked},
		{destination: "data:text/plain,%GG", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "mailto:a@example.test", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "git+ssh:payload", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "//example.test/a%GG", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "///example.test/a", class: r.Ignored, fragment: r.FragmentAbsent},
		{destination: "file:a.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "FiLe:///a.md#x", class: r.BlockedReference, fragment: r.FragmentUnchecked},
		{destination: "/a.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "C:relative", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "c:/a.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a:stream", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "\\\\host\\a.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a\\b.md", class: r.BlockedReference, fragment: r.FragmentAbsent},
		{destination: "a///b.md", class: r.Local, key: "docs/a/b.md", fragment: r.FragmentAbsent},
		{destination: "./x/../guide.md", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentAbsent},
		{destination: "folder/", class: r.Local, key: "docs/folder", directory: true, fragment: r.FragmentAbsent},
		{destination: "folder/.", class: r.Local, key: "docs/folder", directory: true, fragment: r.FragmentAbsent},
		{destination: "folder/%2e", class: r.Local, key: "docs/folder", directory: true, fragment: r.FragmentAbsent},
		{destination: "folder/..", class: r.Local, key: "docs", directory: true, fragment: r.FragmentAbsent},
		{destination: "..", class: r.Local, root: true, directory: true, fragment: r.FragmentAbsent},
		{destination: "../", class: r.Local, root: true, directory: true, fragment: r.FragmentAbsent},
		{destination: "../.", class: r.Local, root: true, directory: true, fragment: r.FragmentAbsent},
		{destination: ".", class: r.Local, key: "docs", directory: true, fragment: r.FragmentAbsent},
		{destination: "guide.md/.", class: r.Local, key: "docs/guide.md", directory: true, fragment: r.FragmentAbsent},
		{destination: "guide.md/", class: r.Local, key: "docs/guide.md", directory: true, fragment: r.FragmentAbsent},
		{destination: "", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentAbsent},
		{destination: "?query", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentAbsent},
		{destination: "#", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid},
		{destination: "guide.md#part", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentValid, text: "part"},
		{destination: "./guide.md", class: r.Local, key: "docs/guide.md", same: true, fragment: r.FragmentAbsent},
		{destination: "Guide.md", class: r.Local, key: "docs/Guide.md", fragment: r.FragmentAbsent},
		{destination: "é.md", class: r.Local, key: "docs/é.md", fragment: r.FragmentAbsent},
		{destination: "e\u0301.md", class: r.Local, key: "docs/e\u0301.md", fragment: r.FragmentAbsent},
		{destination: "a\u2028b.md", class: r.Local, key: "docs/a\u2028b.md", fragment: r.FragmentAbsent},
		{destination: "a\u2029b.md", class: r.Local, key: "docs/a\u2029b.md", fragment: r.FragmentAbsent},
		{destination: "a%EF%BF%BDb.md", class: r.Local, key: "docs/a\ufffdb.md", fragment: r.FragmentAbsent},
		{destination: "CON", class: r.Local, key: "docs/CON", fragment: r.FragmentAbsent},
		{destination: "./name:stream", class: r.Local, key: "docs/name:stream", fragment: r.FragmentAbsent},
		{destination: "name.", class: r.Local, key: "docs/name.", fragment: r.FragmentAbsent},
		{destination: "name ", class: r.Local, key: "docs/name ", fragment: r.FragmentAbsent},
		{destination: "http%3Avalue", class: r.Local, key: "docs/http:value", fragment: r.FragmentAbsent},
		{destination: "./C%3Arelative", class: r.Local, key: "docs/C:relative", fragment: r.FragmentAbsent},
	}
}

func matrixNormalizer(t testing.TB) (*r.Normalizer, parser.Source) {
	t.Helper()
	root, err := r.NewRoot(r.SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := r.NewSettings(parser.ParseOptions{}, "index.md", "github", "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte("original source\r\n"))
	key, err := r.NewKey("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	op, err := r.NewOperation(context.Background(), budget, root, settings, []r.Target{{ID: "report.md", Source: source, ResourceKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(r.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return normalizer, source
}

func matrixLink(t testing.TB, source parser.Source, destination string) r.Link {
	t.Helper()
	span, err := source.Span(0, source.Len())
	if err != nil {
		t.Fatal(err)
	}
	link, err := r.NewLink("report.md", "MD057", destination, source, span)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func TestNormalizeExactDestinations(t *testing.T) {
	for _, tc := range normalizationMatrix() {
		t.Run(tc.destination, func(t *testing.T) {
			normalizer, source := matrixNormalizer(t)
			link := matrixLink(t, source, tc.destination)
			got, err := normalizer.Normalize(link)
			if err != nil {
				t.Fatal(err)
			}
			key, hasKey := got.Location().Key()
			if got.Class() != tc.class || hasKey != (tc.key != "") || key.String() != tc.key || got.Location().IsRootDirectory() != tc.root {
				t.Fatalf("path decision: class=%s key=%q present=%t root=%t; want class=%s key=%q root=%t", got.Class(), key.String(), hasKey, got.Location().IsRootDirectory(), tc.class, tc.key, tc.root)
			}
			wantType := r.AnyType
			if tc.directory {
				wantType = r.DirectoryType
			}
			if tc.class == r.Local && (got.DirectoryHint() != tc.directory || got.RequiredType() != wantType) {
				t.Fatalf("directory requirement=%t/%s want %t/%s", got.DirectoryHint(), got.RequiredType(), tc.directory, wantType)
			}
			if got.Fragment().Status() != tc.fragment || got.Fragment().Text() != tc.text || got.Fragment().RequiresAnchor() != (tc.fragment == r.FragmentValid && tc.text != "") {
				t.Fatalf("fragment=%s/%q requires=%t; want %s/%q", got.Fragment().Status(), got.Fragment().Text(), got.Fragment().RequiresAnchor(), tc.fragment, tc.text)
			}
			same, reused := got.SameSource()
			if reused != tc.same || reused && same != source {
				t.Fatalf("same-source=%t, source identity preserved=%t; want %t", reused, same == source, tc.same)
			}
			if got.Link() != link || string(source.Bytes()) != "original source\r\n" {
				t.Fatal("normalization changed requesting provenance or source bytes")
			}
			wantProblem := r.NoProblem
			if tc.class == r.BlockedReference {
				wantProblem = r.Blocked
			} else if tc.class == r.InvalidReference {
				wantProblem = r.MalformedReference
			}
			if got.Problem().Kind != wantProblem {
				t.Fatal("path failure attribution differs from classification", got.Problem(), wantProblem)
			}
			_, rawFragment, _ := strings.Cut(tc.destination, "#")
			if got.Fragment().Raw() != rawFragment {
				t.Fatal("raw fragment spelling lost", got.Fragment().Raw(), rawFragment)
			}
		})
	}
}

func FuzzNormalizeDestination(f *testing.F) {
	for _, tc := range normalizationMatrix() {
		f.Add(tc.destination)
	}
	for _, seed := range []string{string([]byte{0xff}), strings.Repeat("../", 66), strings.Repeat("/", 96), "%", "%%%%", "%00", "a%2fb#%FF", "./%252e/%252e%252e"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, destination string) {
		if len(destination) > 2048 {
			return
		}
		normalizer, source := matrixNormalizer(t)
		span, err := source.Span(0, source.Len())
		if err != nil {
			t.Fatal(err)
		}
		link, err := r.NewLink("report.md", "MD057", destination, source, span)
		if !utf8.ValidString(destination) {
			if !errors.Is(err, r.ErrContract) {
				t.Fatal("malformed semantic Unicode accepted", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := normalizer.Normalize(link)
		if err != nil {
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) {
				t.Fatal("unexpected error for bounded semantic input", err)
			}
			return
		}
		switch got.Class() {
		case r.Local, r.Ignored, r.BlockedReference, r.InvalidReference:
		default:
			t.Fatal("unknown classification", got.Class())
		}
		key, hasKey := got.Location().Key()
		if got.Class() == r.Local {
			if hasKey {
				canonical, e := r.NewKey(key.String())
				if e != nil || canonical != key {
					t.Fatal("successful result is outside the literal key domain", key.String(), e)
				}
			} else if !got.Location().IsRootDirectory() {
				t.Fatal("in-namespace local decision has no explicit locator")
			}
		} else if hasKey || got.Location().IsRootDirectory() {
			t.Fatal("nonlocal decision grants a resource locator")
		}
		if got.Link() != link || string(source.Bytes()) != "original source\r\n" {
			t.Fatal("source provenance mutated")
		}
		repeated, e := normalizer.Normalize(link)
		if e != nil || !samePathDecision(got, repeated) || got.Fragment() != repeated.Fragment() {
			t.Fatal("same immutable input gives different facts", e)
		}
		// Queries are not part of resource identity, and invalid escapes in a
		// discarded query cannot invalidate an otherwise unchanged raw path.
		beforeFragment, _, _ := strings.Cut(destination, "#")
		rawPath, _, _ := strings.Cut(beforeFragment, "?")
		query, e := normalizer.Normalize(matrixLink(t, source, rawPath+"?ignored=%GG+%FF"))
		if e != nil || !samePathDecision(got, query) {
			t.Fatal("query spelling changed path identity", e)
		}
		// The source's directory is exactly one step below root. Prefixing two
		// parents must never clamp above-root traversal back to an allowed key.
		escape, e := normalizer.Normalize(matrixLink(t, source, "../../"+destination))
		if e == nil && escape.Class() == r.Local {
			t.Fatal("above-root traversal was accepted", destination)
		}
		if e != nil {
			var limit *parser.LimitExceeded
			if !errors.As(e, &limit) {
				t.Fatal(e)
			}
		}
		// Once-only decoding has an independently known literal percent result.
		// Do not feed that result back as a URL: keys and URLs are different domains.
		data := []byte(destination)
		if len(data) > 16 {
			data = data[:16]
		}
		suffix := hex.EncodeToString(data) + ".md"
		once, e := normalizer.Normalize(matrixLink(t, source, "%252e%252e/"+suffix))
		onceKey, present := once.Location().Key()
		if e != nil || once.Class() != r.Local || !present || onceKey.String() != "docs/%2e%2e/"+suffix {
			t.Fatal("percent data was decoded twice or treated as parent traversal", e, onceKey.String())
		}
		fragment, e := normalizer.Normalize(matrixLink(t, source, "asset-"+suffix+"#%FF"))
		fragmentKey, present := fragment.Location().Key()
		if e != nil || fragment.Class() != r.Local || !present || fragmentKey.String() != "docs/asset-"+suffix || fragment.Fragment().Status() != r.FragmentInvalid {
			t.Fatal("invalid fragment hid independent valid existence", e)
		}
	})
}

func samePathDecision(a, b r.NormalizedLink) bool {
	ak, ap := a.Location().Key()
	bk, bp := b.Location().Key()
	as, ar := a.SameSource()
	bs, br := b.SameSource()
	return a.Class() == b.Class() && ak == bk && ap == bp && a.Location().IsRootDirectory() == b.Location().IsRootDirectory() && a.DirectoryHint() == b.DirectoryHint() && ar == br && as == bs
}

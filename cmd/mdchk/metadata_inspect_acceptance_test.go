package main

import (
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// Lint and inspect decode independently; on one file they must agree exactly.
func TestCompiledMetadataLintAndInspectAgree(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	directive := "# <!-- mdchk:disable MD072 -->"
	header := "---\nz: 1   \n" + directive + "\na:\n  y: 1\n  b: 2\n---\n"
	source := header + "\n# Body\n"
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\nfrontmatter: {enabled: true}\nrules:\n  MD072: {enabled: true, recursive: true}\n")
	putConfigurationFile(t, root, "doc.md", source)

	lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
	inspected, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
	if lint.Status != 10 || inspected.Status != 0 {
		t.Fatal(lint, inspected)
	}
	pick := func(all []result.Diagnostic) (out []result.ByteRange) {
		for _, d := range all {
			if d.Rule == "MD072" {
				out = append(out, d.ByteRange)
			}
		}
		return out
	}
	byStart := func(a, b result.ByteRange) int { return a.Start - b.Start }
	findings := pick(lint.Result.Diagnostics)
	if len(findings) != 2 || !slices.Equal(findings, pick(inspected.Result.Diagnostics)) {
		t.Fatalf("lint %+v inspect %+v", lint.Result.Diagnostics, inspected.Result.Diagnostics)
	}
	for i, d := range lint.Result.Diagnostics {
		if d.Rule == "MD072" && (d.Message == "" || d.Message != inspected.Result.Diagnostics[i].Message) {
			t.Fatalf("%+v", d)
		}
	}
	record := p.Metadata.Data
	if record == nil || record.State != "complete" || !record.Ordering.Recursive {
		t.Fatalf("%+v", p.Metadata)
	}
	var located []result.ByteRange
	for _, mapping := range record.Ordering.Mappings {
		for _, key := range mapping.OffendingKeys {
			if key.ByteRange != nil && !slices.Contains(located, *key.ByteRange) {
				located = append(located, *key.ByteRange)
			}
		}
	}
	slices.SortFunc(located, byStart)
	slices.SortFunc(findings, byStart)
	if !slices.Equal(located, findings) {
		t.Fatalf("offending %+v findings %+v", located, findings)
	}

	if directives := *p.Directives.Data; len(directives) != 0 {
		t.Fatalf("header directive escaped: %+v", directives)
	}
	candidates := *p.RawCandidates.Data
	if len(candidates) != 1 || candidates[0].Context != "literal" || candidates[0].DirectiveID != "" {
		t.Fatalf("%+v", candidates)
	}
	for _, rule := range *p.Rules.Data {
		for _, interval := range rule.Intervals {
			if interval.Origin == "directive" {
				t.Fatalf("header line changed %s state: %+v", rule.Rule, interval)
			}
		}
	}
	for _, d := range append(lint.Result.Diagnostics, inspected.Result.Diagnostics...) {
		if d.Rule != "MD072" && d.ByteRange.Start < len(header) {
			t.Fatalf("body rule reached the header: %+v", d)
		}
	}
}

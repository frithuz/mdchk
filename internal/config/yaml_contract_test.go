package config

import (
	"errors"
	"reflect"
	"testing"
)

func decodeContractYAML(t *testing.T, source string) *Definition {
	t.Helper()
	d, err := DecodeYAML("missing/../source.yaml", []byte(source))
	if err != nil || d == nil {
		t.Fatalf("DecodeYAML(%q) = %v, %v", source, d, err)
	}
	return d
}

func TestYAMLContractStrictVersion(t *testing.T) {
	for _, value := range []string{`"1"`, `'1'`, "1.0", "1.9", "01", "0x1", "0o1", "0b1", "+1", "1_0", "-1", "0", "2", "true", "null", "~", "[]", "{}"} {
		t.Run(value, func(t *testing.T) {
			if d, err := DecodeYAML("input.yaml", []byte("version: "+value+"\n")); err == nil || d != nil {
				t.Fatalf("version %s = %v, %v; want rejection", value, d, err)
			}
		})
	}
	decodeContractYAML(t, "version: 1\n")
}

func TestYAMLContractFixedShapesAndScalarTypes(t *testing.T) {
	invalid := []string{
		"flavor: 1", "flavor: true", "flavor: null", "flavor: []",
		"rules: []", "rules: {MD001: false}", "rules: {MD001: null}",
		"rules: {MD001: {enabled: 'false'}}", "rules: {MD001: {allow_directives: 0}}",
		"exclude: vendor", "exclude: [42]", "exclude: [null]",
		"directives: false", "directives: {enabled: 'true'}", "directives: {enabled: yes}",
		"allow_nested_excludes: 'false'", "allow_nested_excludes: FALSE", "allow_nested_excludes: off",
		"resources: []", "resources: {index_file: 3}", "resources: {anchor_style: true}",
		"frontmatter: []", "frontmatter: {enabled: 1}", "frontmatter: {formats: yaml}", "frontmatter: {formats: [3]}",
		"documents: []", "documents: {slides: '*.md'}", "documents: {slides: [false]}",
		"integrations: []", "integrations: {quarto: true}", "integrations: {quarto: {enabled: 'false'}}",
		"unknown: true", "directives: {unknown: true}", "resources: {unknown: true}",
		"frontmatter: {unknown: true}", "documents: {unknown: true}", "integrations: {unknown: {}}", "integrations: {quarto: {unknown: true}}",
	}
	for _, mapping := range []string{"rules", "resources", "frontmatter", "documents", "integrations", "directives", "exclude", "allow_nested_excludes"} {
		invalid = append(invalid, mapping+": null")
	}
	for _, fragment := range invalid {
		t.Run(fragment, func(t *testing.T) {
			d, err := DecodeYAML("input.yaml", []byte("version: 1\n"+fragment+"\n"))
			if err == nil || d != nil {
				t.Fatalf("accepted %q: %v, %v", fragment, d, err)
			}
		})
	}
}

func TestYAMLContractPresenceAndPresentation(t *testing.T) {
	omitted := decodeContractYAML(t, "version: 1\n")
	if omitted.Flavor != nil || omitted.Rules != nil || omitted.Exclude != nil || omitted.Resources != nil || omitted.Frontmatter != nil || omitted.Integrations != nil || omitted.Documents != nil || omitted.Directives != nil || omitted.AllowNestedExcludes != nil {
		t.Fatalf("omitted fields gained values: %+v", omitted)
	}
	d := decodeContractYAML(t, `# ordinary presentation forms
version: 1 # schema
flavor: "commonmark"
rules: {MD001: {}, MD013: {enabled: false, allow_directives: false}}
exclude: []
resources: {index_file: 'README.md', anchor_style: explicit}
frontmatter:
  enabled: false
  formats: [yaml, toml]
documents: {slides: []}
integrations: {quarto: {enabled: false}}
directives: {enabled: false}
allow_nested_excludes: false
`)
	if *d.Version != 1 || *d.Flavor != "commonmark" || d.Rules["MD001"].Enabled != nil || d.Rules["MD013"].Enabled == nil || *d.Rules["MD013"].Enabled || *d.Rules["MD013"].AllowDirectives {
		t.Fatalf("presence lost: %+v", d)
	}
	if d.Exclude == nil || len(*d.Exclude) != 0 || d.Documents.Slides == nil || len(*d.Documents.Slides) != 0 || *d.Frontmatter.Enabled || *d.Integrations.Quarto.Enabled || *d.Directives.Enabled || *d.AllowNestedExcludes {
		t.Fatal("explicit empty/false settings lost")
	}
	if !reflect.DeepEqual(*d.Frontmatter.Formats, []string{"yaml", "toml"}) || *d.Resources.IndexFile != "README.md" || *d.Resources.AnchorStyle != "explicit" {
		t.Fatal("supplied settings changed")
	}
	for _, key := range []string{"resources", "frontmatter", "documents", "integrations", "directives", "rules"} {
		decodeContractYAML(t, "version: 1\n"+key+": {}\n")
	}
	decodeContractYAML(t, "version: 1\nrules:\n  UNKNOWN:\n    future_option: |\n      first\n      second\n")
}

func TestYAMLContractDocumentBoundary(t *testing.T) {
	for _, source := range []string{"", "# comment\n", "---\n", "null\n", "~\n", "[]\n", "[1]\n", "{}\n", "version: 1\n---\nversion: 1\n", "version: 1\n---\n", "version: 1\n...\n---\n", "---\n---\nversion: 1\n"} {
		t.Run(source, func(t *testing.T) {
			d, err := DecodeYAML("input.yaml", []byte(source))
			if err == nil || d != nil {
				t.Fatalf("accepted %q: %v, %v", source, d, err)
			}
		})
	}
	decodeContractYAML(t, "---\nversion: 1\n...\n# trailing comment\n")
	for _, source := range [][]byte{[]byte("version: 1\nflavor: \xff\n"), append([]byte("version: 1\n# "), 0xff)} {
		if d, err := DecodeYAML("input.yaml", source); err == nil || d != nil {
			t.Fatalf("accepted invalid UTF-8: %v, %v", d, err)
		}
	}
	decodeContractYAML(t, "version: 1\nrules: {future: {text: '\ufffd'}}\n")
}

func TestYAMLContractForbiddenNodes(t *testing.T) {
	for _, fragment := range []string{
		"? [a, b]\n: value", "true: value", "1: value", "rules: {MD001: {1: x}}",
		"flavor: !custom gfm", "rules: {MD001: {x: !custom value}}", "rules: {MD001: {x: null}}", "rules: {MD001: {x: [null]}}",
		"rules: {MD001: &a {x: *a}}", "rules: {MD001: *missing}", "rules: {MD001: &a {enabled: false}, MD013: {<<: *a}}",
		"rules: {MD001: {'<<': {enabled: false}}}",
	} {
		t.Run(fragment, func(t *testing.T) {
			d, err := DecodeYAML("input.yaml", []byte("version: 1\n"+fragment+"\n"))
			if err == nil || d != nil {
				t.Fatalf("accepted forbidden syntax %q: %v, %v", fragment, d, err)
			}
		})
	}
}

func contractEntry(t *testing.T, v LocatedValue, key string) LocatedValue {
	t.Helper()
	for _, entry := range v.Entries() {
		if entry.Key == key {
			return entry.Value
		}
	}
	t.Fatalf("missing key %q", key)
	return LocatedValue{}
}

func TestYAMLContractDuplicateLocations(t *testing.T) {
	cases := []struct {
		source, path                         string
		firstLine, firstColumn, line, column int
	}{
		{"version: 1\nversion: 1\n", "version", 1, 1, 2, 1},
		{"version: 1\nrules: {future: {a: first, 'a': second}}\n", "rules.future.a", 2, 18, 2, 28},
		{"version: 1\nunknown:\n  a: first\n  \"\\u0061\": second\n", "unknown.a", 3, 3, 4, 3},
		{"version: 1\nrules:\n  MD001: {}\n  MD001: {}\n", "rules.MD001", 3, 3, 4, 3},
		{"version: 1\nrules:\n  MD001:\n    enabled: false\n    enabled: true\n", "rules.MD001.enabled", 4, 5, 5, 5},
		{"version: 1\nrules:\n  future:\n    option:\n      child: 1\n      child: 2\n", "rules.future.option.child", 5, 7, 6, 7},
		{"version: 1\nintegrations:\n  quarto:\n    enabled: false\n    enabled: true\n", "integrations.quarto.enabled", 4, 5, 5, 5},
		{"version: 1\nresources:\n  index_file: a\n  index_file: b\n", "resources.index_file", 3, 3, 4, 3},
		{"version: 1\nfrontmatter: {enabled: false, enabled: true}\n", "frontmatter.enabled", 2, 15, 2, 31},
		{"version: 1\ndocuments: {slides: [], slides: []}\n", "documents.slides", 2, 13, 2, 25},
		{"version: 1\ndirectives: {enabled: false, enabled: true}\n", "directives.enabled", 2, 14, 2, 30},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			d, err := DecodeYAML("source.yaml", []byte(tc.source))
			var diagnostic *DecodeError
			if d != nil || !errors.As(err, &diagnostic) {
				t.Fatalf("want located duplicate diagnostic, got %v, %v", d, err)
			}
			if diagnostic.Previous == nil {
				t.Fatalf("missing first occurrence: %v", err)
			}
			for _, want := range []struct {
				origin       Origin
				line, column int
			}{{diagnostic.Origin, tc.line, tc.column}, {*diagnostic.Previous, tc.firstLine, tc.firstColumn}} {
				if want.origin.Path != "source.yaml" || want.origin.PropertyPath != tc.path || want.origin.Position == nil || want.origin.Position.Line != want.line || want.origin.Position.Column != want.column {
					t.Errorf("origin %+v position %+v; want %s:%d:%d %s", want.origin, want.origin.Position, "source.yaml", want.line, want.column, tc.path)
				}
			}
		})
	}
}

func TestYAMLContractAliasesAtUseSite(t *testing.T) {
	d := decodeContractYAML(t, "version: 1\nrules:\n  first: &settings\n    text: &text 'hello'\n    seq: &seq [one, two]\n  second: *settings\n  third: {text: *text, seq: *seq}\n")
	rules := contractEntry(t, d.Syntax(), "rules")
	second := contractEntry(t, rules, "second")
	for _, v := range []LocatedValue{second, contractEntry(t, second, "text"), contractEntry(t, second, "seq")} {
		o := v.Origin()
		if o.Position == nil || o.Position.Line != 6 || o.Position.Column != 11 {
			t.Fatalf("whole mapping alias lost use site: %+v %+v", o, o.Position)
		}
	}
	third := contractEntry(t, rules, "third")
	text := contractEntry(t, third, "text")
	if text.StringValue() != "hello" || text.Origin().PropertyPath != "rules.third.text" || text.Origin().Position.Line != 7 || text.Origin().Position.Column != 17 {
		t.Fatalf("scalar alias origin/value: %+v %q", text.Origin(), text.StringValue())
	}
	seq := contractEntry(t, third, "seq")
	if len(seq.Elements()) != 2 || seq.Origin().Position.Column != 29 {
		t.Fatalf("sequence alias: %+v %+v", seq.Origin(), seq.Elements())
	}
	for _, source := range []string{"version: 1\nrules: {future: {number: &n 3}}\nflavor: *n\n", "version: 1\nrules: {future: {sequence: &s [a]}}\ndirectives: *s\n"} {
		if d, err := DecodeYAML("input.yaml", []byte(source)); d != nil || err == nil {
			t.Fatalf("wrong type alias accepted: %v, %v", d, err)
		}
	}
}

func TestYAMLContractRawOptionsAndCopies(t *testing.T) {
	d := decodeContractYAML(t, "version: 1\nrules:\n  unknown_id:\n    text: '12'\n    integer: -12\n    number: 1.5\n    boolean: false\n    sequence: [one]\n    mapping: {nested: value}\n")
	options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "unknown_id")
	for _, tc := range []struct {
		key      string
		kind     ValueKind
		spelling string
	}{{"text", StringValue, "'12'"}, {"integer", IntegerValue, "-12"}, {"number", NumberValue, "1.5"}, {"boolean", BooleanValue, "false"}, {"sequence", SequenceValue, ""}, {"mapping", MappingValue, ""}} {
		v := contractEntry(t, options, tc.key)
		if v.Kind() != tc.kind || tc.spelling != "" && v.Spelling() != tc.spelling {
			t.Errorf("%s kind/spelling = %s %q", tc.key, v.Kind(), v.Spelling())
		}
	}
	if contractEntry(t, options, "text").StringValue() != "12" {
		t.Fatal("quoted string was not decoded")
	}
	entries := options.Entries()
	entries[0].Key = "changed"
	entries[0].Origin.Position.Line = 999
	if options.Entries()[0].Key != "text" || options.Entries()[0].Origin.Position.Line != 4 {
		t.Fatal("Entries exposes mutable storage")
	}
	seq := contractEntry(t, options, "sequence")
	elements := seq.Elements()
	elements[0] = LocatedValue{}
	if seq.Elements()[0].StringValue() != "one" {
		t.Fatal("Elements exposes mutable storage")
	}
	origin := options.Origin()
	origin.Position.Line = 999
	if options.Origin().Position.Line != 4 {
		t.Fatal("Origin exposes mutable position")
	}
	d.Provenance["rules.unknown_id"].Position.Line = 999
	if options.Origin().Position.Line != 4 {
		t.Fatal("mutable definition provenance changed syntax snapshot")
	}
	if options.Origin().Path != "missing/../source.yaml" {
		t.Fatalf("source path was normalized/read: %q", options.Origin().Path)
	}
}

func TestYAMLContractUnicodeAndCRLFColumns(t *testing.T) {
	d := decodeContractYAML(t, "version: 1\r\nrules: {future: {é: value, word: '�'}}\r\n")
	options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
	word := contractEntry(t, options, "word")
	if word.Origin().Position.Line != 2 || word.Origin().Position.Column != 34 {
		t.Fatalf("want code-point column 34, got %+v", word.Origin().Position)
	}
}

func TestYAMLContractStandardTags(t *testing.T) {
	for _, source := range []string{
		"version: !!int 1\nflavor: !!str 'gfm'\n",
		"version: !<tag:yaml.org,2002:int> 1\nflavor: !<tag:yaml.org,2002:str> gfm\n",
		"version: 1\ndirectives: !!map {enabled: !!bool false}\nexclude: !!seq [!!str 'vendor/**']\n",
		"version: 1\nrules: {future: {value: !!float 1.5}}\n",
		"version: !!int '1'\nflavor: !!str 123\ndirectives: {enabled: !!bool 'true'}\n",
	} {
		decodeContractYAML(t, source)
	}
	for _, fragment := range []string{"version: !!str 1", "version: !!int 1.0", "version: 1\ndirectives: {enabled: !!bool yes}", "version: 1\nrules: {future: {value: !!null null}}"} {
		if d, err := DecodeYAML("input.yaml", []byte(fragment+"\n")); d != nil || err == nil {
			t.Fatalf("tag changed kind: %q => %v, %v", fragment, d, err)
		}
	}
}

func TestYAMLContractAliasesKeepOriginalBindings(t *testing.T) {
	for _, tc := range []struct {
		name, original, replacement string
		kind                        ValueKind
		text                        string
	}{
		{"scalar", "false", "true", BooleanValue, "false"},
		{"sequence", "[old]", "[new]", SequenceValue, "old"},
		{"mapping", "{name: old}", "{name: new}", MappingValue, "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := decodeContractYAML(t, "version: 1\nrules:\n  first: {value: &value "+tc.original+"}\n  holder: &holder {nested: *value}\n  redefine: {value: &value "+tc.replacement+"}\n  reuse: *holder\n  current: {value: *value}\n  reuse_again: *holder\n")
			rules := contractEntry(t, d.Syntax(), "rules")
			for _, key := range []string{"holder", "reuse", "reuse_again"} {
				v := contractEntry(t, contractEntry(t, rules, key), "nested")
				if v.Kind() != tc.kind {
					t.Fatalf("%s kind = %s", key, v.Kind())
				}
				switch tc.kind {
				case StringValue, IntegerValue, NumberValue:
					t.Fatalf("unsupported fixture kind %s", tc.kind)
				case BooleanValue:
					if v.Spelling() != tc.text {
						t.Fatalf("%s rebound to %q", key, v.Spelling())
					}
				case SequenceValue:
					if v.Elements()[0].StringValue() != tc.text {
						t.Fatalf("%s nested alias rebound", key)
					}
				case MappingValue:
					if contractEntry(t, v, "name").StringValue() != tc.text {
						t.Fatalf("%s nested alias rebound", key)
					}
				}
			}
			current := contractEntry(t, contractEntry(t, rules, "current"), "value")
			switch tc.kind {
			case StringValue, IntegerValue, NumberValue:
				t.Fatalf("unsupported fixture kind %s", tc.kind)
			case BooleanValue:
				if current.Spelling() != "true" {
					t.Fatal("direct alias ignored latest anchor")
				}
			case SequenceValue:
				if current.Elements()[0].StringValue() != "new" {
					t.Fatal("direct alias ignored latest anchor")
				}
			case MappingValue:
				if contractEntry(t, current, "name").StringValue() != "new" {
					t.Fatal("direct alias ignored latest anchor")
				}
			}
		})
	}
	d := decodeContractYAML(t, "version: 1\nrules:\n  first: {value: &permission false}\n  holder: &settings {enabled: *permission}\n  redefine: {value: &permission true}\ndirectives: *settings\n")
	if *d.Directives.Enabled {
		t.Fatal("reusing a mapping changed its originally false directive permission")
	}
}

func TestYAMLContractNonStringKeyLocations(t *testing.T) {
	cases := []struct {
		source, path string
		line, column int
	}{
		{"version: 1\nrules:\n  future:\n    123: value\n", "rules.future", 4, 5},
		{"version: 1\r\nrules: {future: {é: one, 123: two}}\r\n", "rules.future", 2, 26},
		{"version: 1\nrules: {future: {number: &number 3}}\n? *number\n: value\n", "", 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			d, err := DecodeYAML("source.yaml", []byte(tc.source))
			var diagnostic *DecodeError
			if d != nil || !errors.As(err, &diagnostic) {
				t.Fatalf("want located error, got %v, %v", d, err)
			}
			o := diagnostic.Origin
			if o.Path != "source.yaml" || o.PropertyPath != tc.path || o.Position == nil || o.Position.Line != tc.line || o.Position.Column != tc.column {
				t.Fatalf("key origin = %+v position %+v; want path %q at %d:%d", o, o.Position, tc.path, tc.line, tc.column)
			}
		})
	}
	decodeContractYAML(t, "version: 1\nrules:\n  future:\n    key: &key option\n    ? *key\n    : value\n")
}

func TestYAMLContractBlockSpellingIsOriginalSource(t *testing.T) {
	for _, tc := range []struct{ name, prefix, spelling, decoded string }{
		{"literal_keep", "", "|+ # retain presentation\n      first\n      second\n\n\n", "first\nsecond\n\n\n"},
		{"folded", "", "> # folded presentation\n      first\n      second\n\n", "first second\n"},
		{"tagged_literal", "!!str ", "|+ # tagged presentation\n      first\n      second\n\n\n", "first\nsecond\n\n\n"},
		{"tagged_folded", "!!str ", "> # tagged folded\n      first\n      second\n\n", "first second\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "version: 1\nrules:\n  future:\n    text: " + tc.prefix + tc.spelling + "    following: plain\n"
			d := decodeContractYAML(t, source)
			text := contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future"), "text")
			if text.Kind() != StringValue || text.Spelling() != tc.spelling || text.StringValue() != tc.decoded {
				t.Fatalf("block kind/spelling/value = %s %q %q; want string %q %q", text.Kind(), text.Spelling(), text.StringValue(), tc.spelling, tc.decoded)
			}
		})
	}
}

func TestYAMLContractBlockSpellingPreservesUnicodeAndCRLF(t *testing.T) {
	for _, prefix := range []string{"", "!!str "} {
		t.Run(prefix, func(t *testing.T) {
			spelling := "|+ # café 世界\r\n      héllo\r\n      世界\r\n\r\n"
			source := "version: 1\r\nrules:\r\n  future:\r\n    café: " + prefix + spelling + "    following: plain\r\n"
			d := decodeContractYAML(t, source)
			text := contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future"), "café")
			if text.Kind() != StringValue || text.Spelling() != spelling {
				t.Fatalf("kind/spelling = %s %q; want string %q", text.Kind(), text.Spelling(), spelling)
			}
			if contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future"), "following").StringValue() != "plain" {
				t.Fatal("block spelling consumed following sibling")
			}
		})
	}
}

func TestYAMLContractBlockNewlineBoundaries(t *testing.T) {
	newlineCases := []struct {
		name    string
		endings []string
	}{
		{"LF", []string{"\n"}},
		{"CR", []string{"\r"}},
		{"CRLF", []string{"\r\n"}},
		{"mixed", []string{"\r\n", "\r", "\n"}},
	}
	fixtures := []struct {
		name, indicator, tag, decoded string
		empty, eof                    bool
	}{
		{"literal_alias_sibling", "|+", "", "hello\nworld\n\n", false, false},
		{"folded_tagged_alias_sibling", ">", "!!str ", "hello world\n", false, false},
		{"empty_alias_sibling", "|+", "", "", true, false},
		{"empty_tagged_EOF", ">", "!!str ", "", true, true},
		{"literal_tagged_EOF", "|+", "!!str ", "hello\nworld\n\n", false, true},
		{"folded_EOF", ">", "", "hello world\n", false, true},
	}
	for _, nl := range newlineCases {
		t.Run(nl.name, func(t *testing.T) {
			end := func(line int) string { return nl.endings[(line-1)%len(nl.endings)] }
			for _, fixture := range fixtures {
				t.Run(fixture.name, func(t *testing.T) {
					spelling := fixture.indicator + end(4)
					lastLine := 4
					if !fixture.empty {
						spelling += "      hello" + end(5) + "      world" + end(6) + end(7)
						lastLine = 7
					}
					source := "version: 1" + end(1) + "rules:" + end(2) + "  future:" + end(3) + "    text: &body " + fixture.tag + spelling
					if !fixture.eof {
						source += "    copy: *body" + end(lastLine+1) + "    following: plain" + end(lastLine+2)
					}
					d := decodeContractYAML(t, source)
					options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
					text := contractEntry(t, options, "text")
					if text.Kind() != StringValue || text.Spelling() != spelling || text.StringValue() != fixture.decoded {
						t.Fatalf("block kind/spelling/value = %s %q %q; want string %q %q", text.Kind(), text.Spelling(), text.StringValue(), spelling, fixture.decoded)
					}
					if !fixture.eof {
						copy := contractEntry(t, options, "copy")
						if copy.Spelling() != spelling || copy.StringValue() != fixture.decoded {
							t.Fatalf("alias value/spelling changed: %q %q", copy.Spelling(), copy.StringValue())
						}
						origin := copy.Origin()
						if origin.Path != "missing/../source.yaml" || origin.PropertyPath != "rules.future.copy" || origin.Position == nil || origin.Position.Line != lastLine+1 || origin.Position.Column != 11 {
							t.Fatalf("alias use-site origin = %+v position %+v", origin, origin.Position)
						}
						if contractEntry(t, options, "following").StringValue() != "plain" {
							t.Fatal("block consumed following sibling")
						}
					}
				})
			}
		})
	}
}

func TestYAMLContractCRBlockPanicRegression(t *testing.T) {
	d := decodeContractYAML(t, "version: 1\rrules:\r  future:\r    text: |+\r      hello\r\r    following: plain\r")
	options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
	text := contractEntry(t, options, "text")
	if text.Spelling() != "|+\r      hello\r\r" || text.StringValue() != "hello\n\n" {
		t.Fatalf("CR literal spelling/value = %q %q", text.Spelling(), text.StringValue())
	}
	if contractEntry(t, options, "following").StringValue() != "plain" {
		t.Fatal("CR literal consumed following sibling")
	}
}

func TestYAMLContractHeaderOnlyEmptyBlocks(t *testing.T) {
	for _, fixture := range []struct{ name, prefix, header string }{
		{"literal", "", "|"}, {"literal_strip", "", "|-"}, {"literal_keep", "", "|+"},
		{"folded", "", ">"}, {"folded_strip", "", ">-"}, {"folded_keep", "", ">+"},
		{"literal_indent", "", "|2"}, {"folded_indent_keep", "", ">2+"},
		{"literal_keep_indent", "", "|+2"}, {"folded_strip_indent", "", ">-2"},
		{"tagged", "!!str ", "|-"}, {"anchored", "&empty ", ">"},
		{"tagged_anchored", "&empty !!str ", "|2-"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, boundary := range []string{"sibling", "EOF", "EOF_no_newline"} {
				t.Run(boundary, func(t *testing.T) {
					eof := boundary != "sibling"
					spelling := fixture.header + " # empty header"
					if boundary != "EOF_no_newline" {
						spelling += "\n"
					}
					source := "version: 1\nrules:\n  future:\n    text: " + fixture.prefix + spelling
					if !eof {
						source += "# dedented standalone comment\n    # intervening field comment\n    following: plain\n"
					}
					d := decodeContractYAML(t, source)
					options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
					text := contractEntry(t, options, "text")
					if text.Kind() != StringValue || text.StringValue() != "" || text.Spelling() != spelling {
						t.Fatalf("empty block = %s %q %q; want string, empty value, %q", text.Kind(), text.StringValue(), text.Spelling(), spelling)
					}
					if !eof && contractEntry(t, options, "following").StringValue() != "plain" {
						t.Fatal("empty block consumed following sibling")
					}
				})
			}
		})
	}
}

func TestYAMLContractBlankBlockBodyIsNotHeaderOnly(t *testing.T) {
	for _, prefix := range []string{"", "&blank !!str "} {
		t.Run(prefix, func(t *testing.T) {
			spelling := "|+ # actual blank body\n\n"
			d := decodeContractYAML(t, "version: 1\nrules:\n  future:\n    text: "+prefix+spelling+"    following: plain\n")
			text := contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future"), "text")
			if text.Kind() != StringValue || text.StringValue() != "\n" || text.Spelling() != spelling {
				t.Fatalf("blank-body block = %s %q %q; want string newline %q", text.Kind(), text.StringValue(), text.Spelling(), spelling)
			}
		})
	}
}

func TestYAMLContractEmptyBlockRepairDoesNotAcceptMalformedYAML(t *testing.T) {
	fragments := []string{
		"    text: |x\n", "    text: |0\n", "    text: |22\n", "    text: |+-\n", "    text: |--\n", "    text: |++\n", "    text: |2+2\n", "    text: |+0\n", "    text: |+ extra\n",
		"    text: |\n    following: \"unterminated\n",
		"    text: |\n    following: [unterminated\n",
		"    text: |\n    following: {unterminated\n",
		"    text: |\n\tfollowing: plain\n",
		"    text: |2\n     underindented\n",
		"    text: |\n    following:\n      - item\n     invalid: indentation\n",
	}
	for _, fragment := range fragments {
		t.Run(fragment, func(t *testing.T) {
			source := "version: 1\nrules:\n  future:\n" + fragment
			if d, err := DecodeYAML("source.yaml", []byte(source)); d != nil || err == nil {
				t.Fatalf("accepted malformed YAML %q: %v, %v", source, d, err)
			}
		})
	}
}

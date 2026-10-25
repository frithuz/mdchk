package config

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeObjectKindsAndOwnership(t *testing.T) {
	list := []string{"one"}
	nested := []any{"two"}
	options := map[string]any{"integer": 7, "number": 2.5, "boolean": false, "string": "true", "list": list, "nested": nested, "mapping": map[string]any{"inside": "yes"}}
	root := map[string]any{"version": 1, "exclude": []any{}, "directives": map[string]any{"enabled": false}, "rules": map[string]any{"MD013": options}}
	d, err := DecodeObject(root)
	if err != nil {
		t.Fatal(err)
	}
	if d.Exclude == nil || len(*d.Exclude) != 0 || d.Directives.Enabled == nil || *d.Directives.Enabled {
		t.Fatal("lost explicit empty/false")
	}
	got := d.Rules["MD013"].Options
	for key, want := range map[string]any{"integer": 7, "number": 2.5, "boolean": false, "string": "true", "list": []string{"one"}, "nested": []string{"two"}} {
		if !reflect.DeepEqual(got[key], want) {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	list[0] = "changed"
	nested[0] = "changed"
	options["integer"] = 99
	delete(root, "version")
	if got["integer"] != 7 || got["list"].([]string)[0] != "one" || got["nested"].([]string)[0] != "two" {
		t.Fatal("input mutation changed definition")
	}
	*d.Version = 9
	if entryObject(t, d.Syntax(), "version").StringValue() != "1" {
		t.Fatal("passive definition mutation changed syntax")
	}
	v := entryObject(t, entryObject(t, entryObject(t, d.Syntax(), "rules"), "MD013"), "list")
	elements := v.Elements()
	elements[0] = LocatedValue{}
	if v.Elements()[0].StringValue() != "one" {
		t.Fatal("accessor mutation changed snapshot")
	}
	for path, origin := range d.Provenance {
		if origin.Path != "" || origin.Position != nil || origin.PropertyPath != path {
			t.Errorf("unexpected origin %#v", origin)
		}
	}
}

func entryObject(t *testing.T, v LocatedValue, key string) LocatedValue {
	t.Helper()
	for _, e := range v.Entries() {
		if e.Key == key {
			return e.Value
		}
	}
	t.Fatalf("missing %s", key)
	return LocatedValue{}
}

func TestDecodeObjectRejectsCoercionAndNull(t *testing.T) {
	type namedString string
	type namedMap map[string]any
	for _, value := range []any{nil, int8(1), int64(1), uint(1), float32(1), json.Number("1"), namedString("x"), namedMap{}, []int{}, map[any]any{}, (*int)(nil), map[string]any(nil), []any(nil), []string(nil), math.NaN(), math.Inf(1)} {
		d, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": value}}})
		var decode *DecodeError
		if d != nil || !errors.As(err, &decode) || decode.Origin.PropertyPath != "rules.MD013.option" || decode.Origin.Position != nil {
			t.Errorf("%T: got %v, %v", value, d, err)
		}
	}
	for _, value := range []any{"1", 1.0, true} {
		if d, err := DecodeObject(map[string]any{"version": value}); d != nil || err == nil {
			t.Errorf("accepted version %#v", value)
		}
	}
	if d, err := DecodeObject(nil); d != nil || err == nil {
		t.Fatal("accepted nil root")
	}
}

func TestDecodeObjectFixedStructure(t *testing.T) {
	for _, entry := range []struct {
		key   string
		value any
	}{
		{"unknown", true}, {"directives", map[string]any{"unknown": true}}, {"resources", map[string]any{"unknown": true}},
		{"frontmatter", map[string]any{"unknown": true}}, {"documents", map[string]any{"unknown": true}},
		{"exclude", []any{false}}, {"directives", map[string]any{"enabled": "false"}},
	} {
		if d, err := DecodeObject(map[string]any{"version": 1, entry.key: entry.value}); d != nil || err == nil {
			t.Errorf("accepted %s=%#v", entry.key, entry.value)
		}
	}
	if d, err := DecodeObject(map[string]any{}); d != nil || err == nil {
		t.Fatal("accepted missing version")
	}
}

func TestDecodeObjectCyclesAndBounds(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	slice := make([]any, 1)
	slice[0] = slice
	for _, value := range []any{cycle, slice} {
		_, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": value}}})
		if err == nil || !strings.Contains(err.Error(), "cyclic") {
			t.Fatalf("cycle = %v", err)
		}
	}
	var deep any = "end"
	for range structuredDepth {
		deep = []any{deep}
	}
	_, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": deep}}})
	if err == nil || !strings.Contains(err.Error(), "structured-input-depth") {
		t.Fatalf("depth = %v", err)
	}
	_, err = DecodeObject(map[string]any{"version": 1, "exclude": make([]string, 250001)})
	if err == nil || !strings.Contains(err.Error(), "document-nodes") {
		t.Fatalf("nodes = %v", err)
	}
	shared := map[string]any{"value": "ok"}
	if _, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"first": shared, "second": shared}}}); err != nil {
		t.Fatalf("acyclic sharing rejected: %v", err)
	}
}

func TestDecodeObjectYAMLStructuralParity(t *testing.T) {
	object, err := DecodeObject(map[string]any{"version": 1, "flavor": "gfm", "frontmatter": map[string]any{"enabled": false, "formats": []string{"yaml", "toml"}}, "rules": map[string]any{"MD013": map[string]any{"enabled": false, "line_length": 80}}, "exclude": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := DecodeYAML("", []byte("version: 1\nflavor: gfm\nfrontmatter: {enabled: false, formats: [yaml, toml]}\nrules: {MD013: {enabled: false, line_length: 80}}\nexclude: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(object.Rules, yaml.Rules) || !reflect.DeepEqual(object.Frontmatter, yaml.Frontmatter) || !reflect.DeepEqual(object.Exclude, yaml.Exclude) || *object.Flavor != *yaml.Flavor {
		t.Fatalf("object and YAML differ: %#v / %#v", object, yaml)
	}
}

func TestDecodeObjectWorkBudget(t *testing.T) {
	// Shared Go strings still require inspection at every distinct syntax use.
	long := strings.Repeat("x", 1000000)
	values := make([]any, 251)
	for i := range values {
		values[i] = long
	}
	d, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": values}}})
	if d != nil || err == nil || !strings.Contains(err.Error(), "work-units") {
		t.Fatalf("work = %v, %v", d, err)
	}
}

func TestDecodeObjectEmptySubsliceIsNotCycle(t *testing.T) {
	values := make([]any, 1)
	values[0] = values[:0]
	if _, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": values}}}); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeObjectNonemptyOverlappingPrefixIsNotCycle(t *testing.T) {
	values := make([]any, 2)
	values[0] = "ok"
	values[1] = values[:1]
	d, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": values}}})
	if err != nil {
		t.Fatal(err)
	}
	option := entryObject(t, entryObject(t, entryObject(t, d.Syntax(), "rules"), "MD013"), "option")
	elements := option.Elements()
	if len(elements) != 2 || elements[0].StringValue() != "ok" || len(elements[1].Elements()) != 1 || elements[1].Elements()[0].StringValue() != "ok" {
		t.Fatal("overlapping slice view was not fully decoded")
	}
	values[0] = "changed"
	values[1] = values
	if elements[0].StringValue() != "ok" || elements[1].Elements()[0].StringValue() != "ok" {
		t.Fatal("overlapping input mutation changed owned snapshot")
	}
	// Repeated active views still detect a genuine self-reference.
	_, err = DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": values}}})
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("genuine slice cycle accepted: %v", err)
	}
}

func TestDecodeObjectRejectsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff})
	surrogate := string([]byte{0xed, 0xa0, 0x80})
	for _, bad := range []string{invalid, surrogate} {
		for _, test := range []struct {
			value any
			path  string
		}{
			{bad, "rules.MD013.option"},
			{[]string{bad}, "rules.MD013.option[0]"},
			{[]any{bad}, "rules.MD013.option[0]"},
			{map[string]any{bad: "value"}, "rules.MD013.option." + bad},
		} {
			d, err := DecodeObject(map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"option": test.value}}})
			var decode *DecodeError
			if d != nil || !errors.As(err, &decode) || decode.Origin != (Origin{PropertyPath: test.path}) || !strings.Contains(decode.Message, "UTF-8") {
				t.Errorf("invalid Unicode: %v, %v", d, err)
			}
		}
	}
	if _, err := DecodeObject(map[string]any{"version": 1, "exclude": []string{"文档/😀.md"}}); err != nil {
		t.Fatalf("valid Unicode rejected: %v", err)
	}
}

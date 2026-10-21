package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestYAMLBoundedInputs(t *testing.T) {
	cases := []struct{ name, source, limit string }{
		{"bytes", strings.Repeat(" ", configBytes+1), "config-bytes"},
		{"flow-depth", "version: 1\nrules: {future: {x: " + strings.Repeat("[", 65) + "x" + strings.Repeat("]", 65) + "}}\n", "structured-input-depth"},
	}
	// A tiny source can describe an exponential graph. Each alias use must be
	// revalidated but the adapter stops before constructing an unbounded tree.
	var aliases strings.Builder
	aliases.WriteString("version: 1\nrules:\n  future:\n    a0: &a0 [x, x, x, x, x, x, x, x]\n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&aliases, "    a%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d, ", i-1), 8), ", "))
	}
	cases = append(cases, struct{ name, source, limit string }{"alias-expansion", aliases.String(), "document-nodes"})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := DecodeYAML("config.yaml", []byte(tc.source))
			if d != nil || err == nil || !strings.Contains(err.Error(), tc.limit) {
				t.Fatalf("got %v, %v; want %s", d, err, tc.limit)
			}
		})
	}
}

func TestYAMLCoreTagsPreserveLexicalEvidence(t *testing.T) {
	d, err := DecodeYAML("config.yaml", []byte("version: !!int '1'\nrules: {future: {s: !!str 001, n: !!int '-12', b: !!bool 'false'}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	rule := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
	for _, tc := range []struct {
		key      string
		kind     ValueKind
		spelling string
	}{{"s", StringValue, "001"}, {"n", IntegerValue, "'-12'"}, {"b", BooleanValue, "'false'"}} {
		v := contractEntry(t, rule, tc.key)
		if v.Kind() != tc.kind || v.Spelling() != tc.spelling {
			t.Fatalf("%s: kind %s spelling %q", tc.key, v.Kind(), v.Spelling())
		}
	}
	if d.Rules["future"].Options["n"] != -12 || d.Rules["future"].Options["b"] != false {
		t.Fatal("tagged scalar content was incorrectly converted")
	}
	for _, bad := range []string{"!!int '+1'", "!!int '01'", "!!int '1.0'", "!!int '0x1'", "!!bool 'TRUE'", "!!bool 'yes'"} {
		d, err := DecodeYAML("config.yaml", []byte("version: 1\nrules: {future: {x: "+bad+"}}\n"))
		if d != nil || err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestYAMLImplicitNumbersNeverBecomeStrings(t *testing.T) {
	for _, n := range []string{"1e3", "-2E+05", "1e99999", "18446744073709551616", "-9223372036854775809", "08", "09", "+01"} {
		t.Run(n, func(t *testing.T) {
			d, err := DecodeYAML("config.yaml", []byte("version: 1\nflavor: "+n+"\n"))
			if d != nil || err == nil {
				t.Fatalf("numeric scalar accepted as string: %s", n)
			}
		})
	}
	for _, s := range []string{"'1e3'", `"1e3"`, "!!str 1e3", "!!str 08", "!!str 18446744073709551616"} {
		if _, err := DecodeYAML("config.yaml", []byte("version: 1\nflavor: "+s+"\n")); err != nil {
			t.Fatalf("string %s: %v", s, err)
		}
	}
	d, err := DecodeYAML("config.yaml", []byte("version: 1\nrules: {future: {n: 1e3, i: 18446744073709551616}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
	if contractEntry(t, options, "n").Kind() != NumberValue || contractEntry(t, options, "i").Kind() != IntegerValue {
		t.Fatal("lost numeric kinds outside parser's built-in numeric range")
	}
}

func TestYAMLCoreNumericTagsMatchPlainNumbers(t *testing.T) {
	for _, n := range []string{"1e3", ".inf", "-.Inf", "+.INF", ".nan", ".NaN", ".NAN"} {
		for _, prefix := range []string{"", "!!float "} {
			d, err := DecodeYAML("config.yaml", []byte("version: 1\nrules: {future: {n: "+prefix+n+"}}\n"))
			if err != nil {
				t.Fatal(err)
			}
			option := contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future"), "n")
			if option.Kind() != NumberValue || option.Spelling() != n {
				t.Fatalf("%s%s: lost numeric kind/spelling", prefix, n)
			}
		}
	}
}

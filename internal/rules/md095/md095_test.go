package md095

import (
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestMD095SplitAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, destination, host, problem string
	}{
		// Step 1: hierarchical absolute and protocol-relative destinations.
		{"https", "https://example.org/path", "example.org", ""},
		{"scheme case", "HTTPS://example.org", "example.org", ""},
		{"scheme characters", "git+ssh.v-2://example.org", "example.org", ""},
		{"protocol relative", "//example.org/x", "example.org", ""},
		{"relative", "docs/guide.md", "", ""},
		{"absolute path", "/docs/guide.md", "", ""},
		{"fragment", "#heading", "", ""},
		{"query", "?q=1", "", ""},
		{"empty", "", "", ""},
		{"mailto", "mailto:user@exаmple.org", "", ""},
		{"tel", "tel:+1555", "", ""},
		{"urn", "urn:isbn:0451450523", "", ""},
		{"data", "data:text/plain,hi", "", ""},
		{"scheme starts with digit", "1http://example.org", "", ""},
		{"single slash", "http:/example.org", "example.org", ""},
		{"no slash", "https:exаmple.org", "exаmple.org", ""},
		{"extra slashes", "https:///exаmple.org/", "exаmple.org", ""},
		{"many slashes", "HTTPS:////exаmple.org", "exаmple.org", ""},
		{"backslashes", `https:\\exаmple.org`, "exаmple.org", ""},
		{"mixed slashes", `wss:/\/exаmple.org`, "exаmple.org", ""},
		{"backslash ends authority", `https://exаmple.org\@good.org`, "exаmple.org", ""},
		{"protocol relative extra slashes", "///exаmple.org/", "exаmple.org", ""},
		{"protocol relative backslash", `/\exаmple.org`, "exаmple.org", ""},
		{"special empty host", "https:///", "", ""},
		{"non-special needs two slashes", "git:/exаmple.org", "", ""},
		{"non-special extra slash", "git:///exаmple.org", "", ""},
		{"non-special backslash", `git://exаmple.org\x`, `exаmple.org\x`, ""},
		{"no scheme", "://example.org", "", ""},
		// Step 2: the authority ends at the first /, ? or #.
		{"query ends authority", "https://example.org?x=/y", "example.org", ""},
		{"fragment ends authority", "https://example.org#x/y", "example.org", ""},
		{"lookalike in path", "https://example.org/exаmple", "example.org", ""},
		// Step 3: user information runs to the last @.
		{"user information", "https://exаmple@example.org", "example.org", ""},
		{"last at", "https://a@b@example.org:1/", "example.org", ""},
		// Step 4: bracketed literals.
		{"ipv6", "https://[::1]:8080/", "", ""},
		{"ipvfuture", "https://[v1.x]/", "", ""},
		{"unterminated bracket", "https://[::1/", "", md095InvalidAuthority},
		// Step 5: the port follows the last colon.
		{"port", "https://exаmple.org:8443/", "exаmple.org", ""},
		{"empty port", "https://example.org:/", "example.org", ""},
		{"non-digit port", "https://exаmple.org:80a/", "", md095InvalidAuthority},
		{"last colon", "https://example.org:1:2/", "example.org:1", ""},
		// Step 6: an empty host.
		{"file", "file:///etc/hosts", "", ""},
		{"empty host with port", "https://:80/", "", ""},
		{"empty host with user", "https://user@/", "", ""},
		// Step 7: IPv4 literals.
		{"ipv4", "https://127.0.0.1/", "", ""},
		{"ipv4 hex", "https://0x7f.1/", "", ""},
		{"ipv4 hex upper", "https://0X7F/", "", ""},
		{"ipv4 root dot", "https://127.0.0.1./", "", ""},
		{"numeric final label", "https://exаmple.123/", "", ""},
		{"hex prefix alone", "https://exаmple.0x/", "exаmple.0x", ""},
		{"alphanumeric final label", "https://exаmple.1a/", "exаmple.1a", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := md095SplitAuthority(tc.destination)
			if a.problem != tc.problem || tc.destination[a.hostStart:a.hostEnd] != tc.host {
				t.Fatalf("%+v", a)
			}
		})
	}
}

func TestMD095Decode(t *testing.T) {
	for _, tc := range []struct {
		host, decoded string
		ok            bool
	}{
		{"example.org", "example.org", true},
		{"ex%D0%B0mple.org", "exаmple.org", true},
		{"ex%d0%b0mple.org", "exаmple.org", true},
		{"example%2Eorg", "example.org", true},
		{"ex%2541", "ex%41", true},
		{"ex%zzmple", "", false},
		{"ex%D", "", false},
		{"ex%", "", false},
		{"ex%D0mple", "", false},
		{"ex%FF", "", false},
	} {
		if decoded, ok := md095Decode(tc.host); ok != tc.ok || ok && decoded != tc.decoded {
			t.Fatalf("%q: %q %v", tc.host, decoded, ok)
		}
	}
}

func TestMD095FastPath(t *testing.T) {
	for host, fast := range map[string]bool{
		"example.org":        true,
		"my_host.example":    true,
		"EXAMPLE.ORG":        true,
		"xn--exmple-4nf.org": false,
		"a.XN--exmple-4nf":   false,
		"xn-.org":            true,
		"exаmple.org":        false,
	} {
		if md095FastPath(host) != fast {
			t.Fatal(host)
		}
	}
}

func TestMD095FlaggedAndMessage(t *testing.T) {
	items, first, last := md095Flagged("аеxаmple\U0001D41A.org")
	want := []string{`U+0430 looks like "a"`, `U+0435 looks like "e"`, `U+1D41A looks like "a"`}
	if !slices.Equal(items, want) || first != 0 || last != len("аеxаmple\U0001D41A") {
		t.Fatal(items, first, last)
	}
	if items, _, _ := md095Flagged("зero中ʼ.org"); len(items) != 0 {
		t.Fatal("digit, punctuation and unmapped code points flagged", items)
	}
	if items, _, _ := md095Flagged("æ.org"); !slices.Equal(items, []string{`U+00E6 looks like "ae"`}) {
		t.Fatal(items)
	}
	got := md095Message("аеxаmple.org", want[:2])
	if got != `Hostname "аеxаmple.org" contains lookalike characters: U+0430 looks like "a", U+0435 looks like "e".` {
		t.Fatal(got)
	}
}

// md095Segments lays parts out from offset in source. Each part is the
// original spelling, its transformation and its semantic text; a Synthetic
// part has no source.
func md095Segments(t *testing.T, source parser.Source, offset int, parts ...[3]string) []parser.Segment {
	t.Helper()
	var out []parser.Segment
	at := offset
	for _, p := range parts {
		kind := parser.Transformation(p[1])
		if kind == parser.Synthetic {
			out = append(out, parser.Segment{Transformation: kind, Semantic: parser.Some(p[2])})
			continue
		}
		span, err := source.Span(at, at+len(p[0]))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, parser.Segment{Source: span, Transformation: kind, Semantic: parser.Some(p[2])})
		at += len(p[0])
	}
	return out
}

func md095Exact(text string) [3]string { return [3]string{text, string(parser.Exact), text} }

func TestMD095CheckRanges(t *testing.T) {
	type part = [3]string
	for _, tc := range []struct {
		name       string
		parts      []part
		allowed    []string
		text, host string
		problem    string
		silent     bool
	}{
		{name: "precise", parts: []part{md095Exact("https://exаmple.org")}, text: "а", host: "exаmple.org"},
		{name: "first to last", parts: []part{md095Exact("https://аexаmple.org/аb")}, text: "аexа", host: "аexаmple.org"},
		{name: "root dot precise", parts: []part{md095Exact("https://exаmple.org.")}, text: "а", host: "exаmple.org."},
		{name: "port and user", parts: []part{md095Exact("https://u@exаmple.org:443/")}, text: "а", host: "exаmple.org"},
		{name: "punycode", parts: []part{md095Exact("https://xn--exmple-4nf.org")}, text: "xn--exmple-4nf.org", host: "xn--exmple-4nf.org"},
		{name: "uppercase", parts: []part{md095Exact("https://EXАMPLE.org")}, text: "EXАMPLE.org", host: "EXАMPLE.org"},
		{name: "nfc", parts: []part{md095Exact("https://exаmple.cafe\u0301")}, text: "exаmple.cafe\u0301", host: "exаmple.cafe\u0301"},
		{name: "percent", parts: []part{md095Exact("https://ex%D0%B0mple.org")}, text: "ex%D0%B0mple.org", host: "ex%D0%B0mple.org"},
		{name: "entity", parts: []part{md095Exact("https://ex"), {"&#x430;", string(parser.Entity), "а"}, md095Exact("mple.org")}, text: "ex&#x430;mple.org", host: "ex&#x430;mple.org"},
		{name: "escape", parts: []part{md095Exact("https://exаmple"), {`\.`, string(parser.Escape), "."}, md095Exact("org")}, text: `exаmple\.org`, host: `exаmple\.org`},
		{name: "synthetic prefix", parts: []part{{"", string(parser.Synthetic), "http://"}, md095Exact("www.exаmple.org")}, text: "а", host: "www.exаmple.org"},
		{name: "allowed", parts: []part{md095Exact("https://exаmple.org:8443")}, allowed: []string{"xn--exmple-4nf.org"}, silent: true},
		{name: "ascii lookalike allowed", parts: []part{md095Exact("https://exаmple.org")}, allowed: []string{"example.org"}, text: "а", host: "exаmple.org"},
		{name: "ascii", parts: []part{md095Exact("https://example.org")}, silent: true},
		{name: "fullwidth", parts: []part{md095Exact("https://ｅｘａｍｐｌｅ.org")}, silent: true},
		{name: "unflagged", parts: []part{md095Exact("https://café.org")}, silent: true},
		{name: "invalid port", parts: []part{md095Exact("https://exаmple.org:x/")}, text: "exаmple.org:x", problem: md095InvalidAuthority},
		{name: "unterminated bracket", parts: []part{md095Exact("https://[::1/")}, text: "[::1", problem: md095InvalidAuthority},
		{name: "invalid percent", parts: []part{md095Exact("https://ex%D0mple.org")}, text: "ex%D0mple.org", problem: md095InvalidPercent},
		{name: "invalid punycode", parts: []part{md095Exact("https://xn--a.org")}, text: "xn--a.org", problem: "label 0: disallowed code point (V7)"},
		{name: "std3", parts: []part{md095Exact("https://exаmple_.org")}, text: "exаmple_.org", problem: "label 0: ASCII character other than a-z, 0-9 or hyphen (U1)"},
		{name: "unmapped", parts: []part{md095Exact("https://exа"), {"", string(parser.Synthetic), "x"}, md095Exact("mple.org")}, text: "(https://exаmple.org)", problem: md095Unmapped},
		{name: "unavailable mapping", parts: []part{md095Exact("https://"), {"exаmple", string(parser.MappingUnavailable), "exаmple"}, md095Exact(".org")}, text: "(https://exаmple.org)", problem: md095Unmapped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var original, destination string
			for _, p := range tc.parts {
				original += p[0]
				destination += p[2]
			}
			text := "(" + original + ")"
			source := parser.NewSource([]byte(text))
			segments := md095Segments(t, source, 1, tc.parts...)
			fallback, err := source.Span(0, len(text))
			if err != nil {
				t.Fatal(err)
			}
			allowed := map[string]bool{}
			for _, a := range tc.allowed {
				allowed[a] = true
			}
			got, err := md095Check(destination, segments, fallback, allowed)
			if err != nil {
				t.Fatal(err)
			}
			if tc.silent {
				if got.reported() {
					t.Fatalf("%+v", got)
				}
				return
			}
			if got.problem != tc.problem || text[got.from:got.to] != tc.text || tc.problem == "" && (text[got.hostFrom:got.hostTo] != tc.host || len(got.items) == 0) {
				t.Fatalf("%+v %q", got, text[got.from:got.to])
			}
		})
	}
}

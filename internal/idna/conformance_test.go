package idna

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The retained IdnaTestV2.txt has 6391 test lines; only the two whose source
// or expected strings spell an unpaired surrogate (unrepresentable in a Go
// string) are skipped.
const (
	conformanceExecuted = 6389
	conformanceSkipped  = 2
)

var escape = regexp.MustCompile(`\\u([0-9A-Fa-f]{4})|\\x\{([0-9A-Fa-f]+)\}`)

// unescape expands \uXXXX and \x{XXXX}; it reports false for a surrogate.
func unescape(s string) (string, bool) {
	ok := true
	out := escape.ReplaceAllStringFunc(s, func(m string) string {
		parts := escape.FindStringSubmatch(m)
		v, err := strconv.ParseInt(parts[1]+parts[2], 16, 32)
		if err != nil || v >= 0xD800 && v <= 0xDFFF {
			ok = false
			return ""
		}
		return string(rune(v))
	})
	return out, ok
}

func readTestData(t *testing.T, name string) string {
	t.Helper()
	//nolint:gosec // Read only retained Unicode test data.
	data, err := os.ReadFile("testdata/unicode/17.0.0/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestConformanceIdnaTestV2(t *testing.T) {
	executed, skipped := 0, 0
	for n, line := range strings.Split(readTestData(t, "IdnaTestV2.txt"), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		raw := strings.Split(line, ";")
		if len(raw) != 7 {
			t.Fatalf("line %d: %d columns", n+1, len(raw))
		}
		cols := make([]string, len(raw))
		representable := true
		for i, c := range raw {
			c = strings.Trim(c, " \t")
			if c == `""` {
				c = ""
			} else if c == "" {
				c = "\x00blank"
			}
			var ok bool
			cols[i], ok = unescape(c)
			representable = representable && ok
		}
		if !representable {
			skipped++
			continue
		}
		executed++
		blank := func(i int) bool { return cols[i] == "\x00blank" }
		source := cols[0]
		unicode, unicodeStatus := cols[1], cols[2]
		if blank(1) {
			unicode = source
		}
		if blank(2) {
			unicodeStatus = ""
		}
		ascii, asciiStatus := cols[3], cols[4]
		if blank(3) {
			ascii = unicode
		}
		if blank(4) {
			asciiStatus = unicodeStatus
		}
		if asciiStatus == "[]" {
			asciiStatus = ""
		}
		got, err := ToUnicode(source)
		if unicodeStatus == "" && (err != nil || got != unicode) {
			t.Errorf("line %d: ToUnicode(%+q) = %+q, %v; want %+q", n+1, source, got, err, unicode)
		} else if unicodeStatus != "" && err == nil {
			t.Errorf("line %d: ToUnicode(%+q) = %+q; want error %s", n+1, source, got, unicodeStatus)
		}
		got, err = ToASCII(source)
		if asciiStatus == "" && (err != nil || got != ascii) {
			t.Errorf("line %d: ToASCII(%+q) = %+q, %v; want %+q", n+1, source, got, err, ascii)
		} else if asciiStatus != "" && err == nil {
			t.Errorf("line %d: ToASCII(%+q) = %+q; want error %s", n+1, source, got, asciiStatus)
		}
	}
	t.Logf("executed %d IdnaTestV2 lines, skipped %d unpaired-surrogate lines", executed, skipped)
	if executed != conformanceExecuted || skipped != conformanceSkipped {
		t.Fatalf("executed=%d skipped=%d; want %d and %d", executed, skipped, conformanceExecuted, conformanceSkipped)
	}
}

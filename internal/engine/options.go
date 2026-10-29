package engine

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Options admit only immutable scalars and copied string lists. A rule cannot
// receive arbitrary mutable objects or callbacks through configuration.
type Options map[string]any

type OptionKind string

const (
	Boolean OptionKind = "boolean"
	Integer OptionKind = "integer"
	Number  OptionKind = "number"
	String  OptionKind = "string"
	Strings OptionKind = "strings"
)

type OptionSpec struct {
	Kind     OptionKind
	Default  any
	Minimum  *int
	Maximum  *int
	Choices  []string
	MinItems int
	Domain   OptionDomain
}
type OptionSchema map[string]OptionSpec

// OptionDomain identifies constraints beyond the primitive kind. Hostnames need
// the accepted Unicode 17.0.0 nontransitional UTS #46 provider; a structural
// approximation must never stand in for that validation.
type OptionDomain string

const (
	UnicodeScalarHex   OptionDomain = "unicode-scalar-hex"
	HTMLComment        OptionDomain = "html-comment"
	DNSHostname        OptionDomain = "dns-hostname-unicode-17"
	HeadingOutline     OptionDomain = "heading-outline"
	ConfiguredNameUTF8 OptionDomain = "configured-name-utf8"
)

var ErrHostnameValidationUnavailable = errors.New("hostname validation unavailable: requires Unicode 17.0.0 UTS #46")

// ValidationProviders supplies semantic validation only, without I/O. A hostname
// validator must implement the complete pinned profile in rule-behavior.md,
// including A-label round trips, STD3, bidi/joiner and DNS length checks. It must
// reject non-host syntax and preserve the supplied spelling; normalization for
// later matching belongs to the hostname provider, not catalog resolution.
type ValidationProviders struct {
	ValidateHostname func(string) error
}

func cloneValue(v any) any {
	if s, ok := v.([]string); ok {
		return slices.Clone(s)
	}
	return v
}
func cloneOptions(o Options) Options {
	r := make(Options, len(o))
	for k, v := range o {
		r[k] = cloneValue(v)
	}
	return r
}
func cloneSchema(s OptionSchema) OptionSchema {
	r := maps.Clone(s)
	for k, v := range r {
		v.Default = cloneValue(v.Default)
		v.Choices = slices.Clone(v.Choices)
		if v.Minimum != nil {
			x := *v.Minimum
			v.Minimum = &x
		}
		if v.Maximum != nil {
			x := *v.Maximum
			v.Maximum = &x
		}
		r[k] = v
	}
	return r
}
func (s OptionSpec) validConstraints() bool {
	if (s.Minimum != nil || s.Maximum != nil) && s.Kind != Integer && s.Kind != Number {
		return false
	}
	if len(s.Choices) > 0 && s.Kind != String && s.Kind != Strings {
		return false
	}
	if s.MinItems < 0 || s.MinItems > 0 && s.Kind != Strings {
		return false
	}
	switch s.Domain {
	case "":
		return true
	case HTMLComment:
		return s.Kind == String
	case UnicodeScalarHex, DNSHostname, HeadingOutline, ConfiguredNameUTF8:
		return s.Kind == Strings
	default:
		return false
	}
}

func (s OptionSpec) validate(v any) error {
	valid := false
	switch s.Kind {
	case Boolean:
		_, valid = v.(bool)
	case Integer:
		n, ok := v.(int)
		valid = ok && (s.Minimum == nil || n >= *s.Minimum) && (s.Maximum == nil || n <= *s.Maximum)
	case Number:
		var n float64
		switch x := v.(type) {
		case int:
			n = float64(x)
			valid = true
		case float64:
			n = x
			valid = true
		}
		valid = valid && !math.IsNaN(n) && !math.IsInf(n, 0) && (s.Minimum == nil || n >= float64(*s.Minimum)) && (s.Maximum == nil || n <= float64(*s.Maximum))
	case String:
		x, ok := v.(string)
		valid = ok && (len(s.Choices) == 0 || slices.Contains(s.Choices, x))
	case Strings:
		xs, ok := v.([]string)
		valid = ok && len(xs) >= s.MinItems
		if valid {
			for i, x := range xs {
				if len(s.Choices) > 0 && !slices.Contains(s.Choices, x) {
					return &SettingError{Element: &i, Err: fmt.Errorf("value outside %s option domain", s.Kind)}
				}
			}
		}
	}
	if !valid {
		return fmt.Errorf("value outside %s option domain", s.Kind)
	}
	if s.Domain == HTMLComment {
		x, ok := v.(string)
		if !ok || len(x) < 7 || !strings.HasPrefix(x, "<!--") || !strings.HasSuffix(x, "-->") || strings.ContainsAny(x, "\r\n") {
			return fmt.Errorf("expected a single-line complete HTML comment")
		}
		body := x[4 : len(x)-3]
		if strings.Contains(body, "-->") || strings.Contains(body, "<!--") || strings.Contains(body, "--!>") || strings.HasPrefix(body, ">") || strings.HasPrefix(body, "->") || strings.HasSuffix(body, "<!-") {
			return fmt.Errorf("invalid HTML comment marker")
		}
	}
	if s.Domain == UnicodeScalarHex {
		for i, x := range v.([]string) {
			if x == "" || strings.IndexFunc(x, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') }) >= 0 {
				return &SettingError{Element: &i, Err: fmt.Errorf("expected hexadecimal Unicode scalar")}
			}
			n, err := strconv.ParseUint(x, 16, 32)
			if err != nil || n > utf8.MaxRune || n >= 0xD800 && n <= 0xDFFF {
				return &SettingError{Element: &i, Err: fmt.Errorf("expected hexadecimal Unicode scalar")}
			}
		}
	}
	if s.Domain == ConfiguredNameUTF8 {
		for i, value := range v.([]string) {
			if !utf8.ValidString(value) {
				return &SettingError{Element: &i, Err: fmt.Errorf("expected valid UTF-8 configured name")}
			}
		}
	}
	if s.Domain == HeadingOutline {
		for i, value := range v.([]string) {
			if _, err := headingOutlineEntry(value, nil); err != nil {
				return &SettingError{Element: &i, Err: err}
			}
		}
	}
	return nil
}
func (s OptionSchema) resolve(o Options, providers ValidationProviders) (Options, error) {
	r := make(Options, len(s))
	for _, k := range sortedKeys(s) {
		r[k] = cloneValue(s[k].Default)
	}
	for _, k := range sortedKeys(o) {
		spec, ok := s[k]
		if !ok {
			return nil, &SettingError{Option: k, Err: fmt.Errorf("unknown option %q", k)}
		}
		if err := spec.validate(o[k]); err != nil {
			var detail *SettingError
			if errors.As(err, &detail) {
				owned := *detail
				owned.Option = k
				return nil, &owned
			}
			return nil, &SettingError{Option: k, Err: err}
		}
		r[k] = cloneValue(o[k])
	}
	for _, k := range sortedKeys(s) {
		if s[k].Domain != DNSHostname {
			continue
		}
		for i, host := range r[k].([]string) {
			if providers.ValidateHostname == nil {
				return nil, &SettingError{Option: k, Element: &i, Err: ErrHostnameValidationUnavailable}
			}
			if err := providers.ValidateHostname(host); err != nil {
				return nil, &SettingError{Option: k, Element: &i, Err: err}
			}
		}
	}
	return r, nil
}
func sortedKeys[M ~map[string]V, V any](m M) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	slices.Sort(r)
	return r
}

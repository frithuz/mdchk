package metadata

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// Decode interprets this bound document's recognized payload on demand. It
// shares the original operation and document allowances; it grants no edit.
// Absent and unterminated headers return an unavailable result safely usable
// by dependent policy. Retained immutable facts remain readable after a stop.
func Decode(ctx context.Context, b Binding) (out Result, err error) {
	if b.state == nil || ctx == nil {
		return Result{}, ErrContract
	}
	defer func() { err = errors.Join(err, b.charge(ctx, 0, 0)) }()
	if err = b.charge(ctx, 0, 0); err != nil {
		return Result{}, err
	}
	if b.State() != Undecoded {
		records := int64(1)
		if b.State() == Unterminated {
			records++
		}
		if err := b.charge(ctx, records, records); err != nil {
			return Result{}, err
		}
		r := Result{owner: b.state, root: Value{&value{owner: b.state, kind: Unknown, span: b.Header().Content(), depth: 1}}}
		if b.State() == Unterminated {
			r.problems = []Problem{{b.Header().Content(), Malformed}}
		}
		return r, nil
	}
	if _, err = admitDecode(ctx, b); err != nil {
		return Result{}, err
	}
	switch b.Header().Format() {
	case "yaml":
		return decodeYAML(ctx, b)
	case "toml":
		return decodeTOML(ctx, b)
	default:
		return decodeFailure(ctx, b, Unsupported, b.Header().Content())
	}
}

func admitDecode(ctx context.Context, b Binding) (string, error) {
	n := int64(b.Header().Content().End() - b.Header().Content().Start())
	if int64(b.Source().Len()) > b.state.budget.Limits().DocumentBytes {
		return "", &parser.LimitExceeded{Name: "document-bytes", Observed: int64(b.Source().Len()), Allowed: b.state.budget.Limits().DocumentBytes}
	}
	// Reserve temporary input/AST storage before copying or entering a library.
	// Retained projection/alias expansion is charged separately by builders.
	if err := b.charge(ctx, 64*n+1, 32*n+1); err != nil {
		return "", err
	}
	text, err := b.Source().Text(b.Header().Content())
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(text) {
		return "", ErrContract
	}
	return text, nil
}
func decodeSpan(b Binding, start, end int) parser.Span {
	span, _ := b.Source().Span(b.Header().Content().Start()+start, b.Header().Content().Start()+end)
	return span
}
func decodeFailure(ctx context.Context, b Binding, code ProblemCode, span parser.Span) (Result, error) {
	if !b.owns(span) {
		span = b.Header().Content()
	}
	root, err := scalar(ctx, b, b.Header().Content(), Unknown, "", false)
	if err != nil {
		return Result{}, err
	}
	return result(ctx, b, root, []Problem{{span, code}})
}

var decimalNumber = regexp.MustCompile(`^[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?$`)
var coreInteger = regexp.MustCompile(`^(?:[-+]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)

func canonicalInteger(text string) (string, bool) {
	text = strings.ReplaceAll(text, "_", "")
	base := 10
	digits := text
	sign := ""
	if strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		sign = digits[:1]
		digits = digits[1:]
	}
	if len(digits) > 2 {
		switch digits[:2] {
		case "0x":
			base = 16
			digits = digits[2:]
		case "0o":
			base = 8
			digits = digits[2:]
		case "0b":
			base = 2
			digits = digits[2:]
		}
	}
	n, ok := new(big.Int).SetString(sign+digits, base)
	if !ok {
		return "", false
	}
	return n.String(), true
}

// Decimal floats use exact coefficient e exponent text, with no binary rounding
// or expansion of large exponents. Signed floating zero retains its sign.
func canonicalFloat(text string) (string, bool) {
	text = strings.ToLower(strings.ReplaceAll(text, "_", ""))
	switch text {
	case ".nan", "nan", "+nan", "-nan":
		return "nan", true
	case ".inf", "+.inf", "inf", "+inf":
		return "+inf", true
	case "-.inf", "-inf":
		return "-inf", true
	}
	if !decimalNumber.MatchString(text) {
		return "", false
	}
	neg := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "-"), "+")
	exp := new(big.Int)
	if i := strings.IndexByte(text, 'e'); i >= 0 {
		if _, ok := exp.SetString(text[i+1:], 10); !ok {
			return "", false
		}
		text = text[:i]
	}
	if i := strings.IndexByte(text, '.'); i >= 0 {
		exp.Sub(exp, big.NewInt(int64(len(text)-i-1)))
		text = text[:i] + text[i+1:]
	}
	text = strings.TrimLeft(text, "0")
	if text == "" {
		if neg {
			return "-0e0", true
		}
		return "0e0", true
	}
	trimmed := strings.TrimRight(text, "0")
	exp.Add(exp, big.NewInt(int64(len(text)-len(trimmed))))
	if neg {
		trimmed = "-" + trimmed
	}
	return trimmed + "e" + exp.String(), true
}

// Reserve before exact integer parsing/decimal formatting or exponent arithmetic.
// A quadratic work bound covers the maintained big.Int conversion operations;
// symbolic float exponents avoid allocating an exponentially expanded decimal.
func reserveNumeric(ctx context.Context, b Binding, text string) error {
	n := int64(len(text))
	return b.charge(ctx, n*n+1, 4*n+1)
}

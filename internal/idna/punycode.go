package idna

import (
	"errors"
	"math"
	"strings"
)

// RFC 3492 Bootstring parameters for Punycode.
const (
	base        = 36
	tMin        = 1
	tMax        = 26
	skew        = 38
	damp        = 700
	initialBias = 72
	initialN    = 128
)

// digits are the Punycode digit values 0 to 35.
const digits = "abcdefghijklmnopqrstuvwxyz0123456789"

var errPunycode = errors.New("invalid punycode")

func adapt(delta, points int, first bool) int {
	if first {
		delta /= damp
	} else {
		delta /= 2
	}
	delta += delta / points
	k := 0
	for delta > (base-tMin)*tMax/2 {
		delta /= base - tMin
		k += base
	}
	return k + (base-tMin+1)*delta/(delta+skew)
}

func threshold(k, bias int) int {
	switch {
	case k <= bias:
		return tMin
	case k >= bias+tMax:
		return tMax
	default:
		return k - bias
	}
}

// decodePunycode decodes the part of an A-label after "xn--". It records each
// insertion first and then places them from last to first into the free
// slots of the output, so cost is O(n log n) rather than quadratic.
func decodePunycode(s string) ([]rune, error) {
	var basic []rune
	in := 0
	if b := strings.LastIndexByte(s, '-'); b >= 0 {
		for i := 0; i < b; i++ {
			if s[i] >= 0x80 {
				return nil, errPunycode
			}
			basic = append(basic, rune(s[i]))
		}
		in = b + 1
	}
	type insertion struct {
		r   rune
		pos int
	}
	var inserts []insertion
	n, i, bias := initialN, 0, initialBias
	for in < len(s) {
		oldi, w := i, 1
		for k := base; ; k += base {
			if in >= len(s) {
				return nil, errPunycode
			}
			digit := punycodeDigit(s[in])
			in++
			if digit < 0 || digit > (math.MaxInt32-i)/w {
				return nil, errPunycode
			}
			i += digit * w
			t := threshold(k, bias)
			if digit < t {
				break
			}
			if w > math.MaxInt32/(base-t) {
				return nil, errPunycode
			}
			w *= base - t
		}
		points := len(basic) + len(inserts) + 1
		bias = adapt(i-oldi, points, oldi == 0)
		if i/points > math.MaxInt32-n {
			return nil, errPunycode
		}
		n += i / points
		i %= points
		if n < 0 || n > 0x10FFFF {
			return nil, errPunycode
		}
		if n >= 0xD800 && n <= 0xDFFF {
			return nil, errPunycode
		}
		inserts = append(inserts, insertion{rune(n), i})
		i++
	}
	// Insertion k landed at index pos of the string as it was then; in the
	// final string that is the pos-th slot not taken by any later insertion.
	out := make([]rune, len(basic)+len(inserts))
	free := newSlotTree(len(out))
	for k := len(inserts) - 1; k >= 0; k-- {
		out[free.take(inserts[k].pos)] = inserts[k].r
	}
	for _, r := range basic {
		out[free.take(0)] = r
	}
	return out, nil
}

// slotTree is a Fenwick tree counting free slots, for finding and taking the
// k-th free slot in O(log n).
type slotTree struct {
	counts []int
	top    int
}

func newSlotTree(n int) *slotTree {
	t := &slotTree{counts: make([]int, n+1), top: 1}
	for i := 1; i <= n; i++ {
		t.counts[i]++
		if j := i + i&-i; j <= n {
			t.counts[j] += t.counts[i]
		}
	}
	for t.top*2 <= n {
		t.top *= 2
	}
	return t
}

// take marks the k-th (zero-based) free slot as used and returns its index.
func (t *slotTree) take(k int) int {
	pos := 0
	for step := t.top; step > 0; step /= 2 {
		if next := pos + step; next < len(t.counts) && t.counts[next] <= k {
			pos = next
			k -= t.counts[next]
		}
	}
	for i := pos + 1; i < len(t.counts); i += i & -i {
		t.counts[i]--
	}
	return pos
}

func punycodeDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c-'0') + 26
	case c >= 'a' && c <= 'z':
		return int(c - 'a')
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	default:
		return -1
	}
}

// encodePunycode encodes a label without the "xn--" prefix. Callers bound the
// label first, so the scan for each next code point stays constant-bounded.
func encodePunycode(label []rune) string {
	var out strings.Builder
	for _, r := range label {
		if r < 0x80 {
			out.WriteRune(r)
		}
	}
	basic := out.Len()
	handled := basic
	if basic > 0 {
		out.WriteByte('-')
	}
	n, delta, bias := initialN, 0, initialBias
	for handled < len(label) {
		m := math.MaxInt32
		for _, r := range label {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range label {
			if int(r) < n {
				delta++
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := base; ; k += base {
				t := threshold(k, bias)
				if q < t {
					break
				}
				out.WriteByte(digits[t+(q-t)%(base-t)])
				q = (q - t) / (base - t)
			}
			out.WriteByte(digits[q])
			bias = adapt(delta, handled+1, handled == basic)
			delta = 0
			handled++
		}
		delta++
		n++
	}
	return out.String()
}

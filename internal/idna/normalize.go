package idna

// Hangul syllable composition constants from The Unicode Standard, section 3.12.
const (
	hangulS = 0xAC00
	hangulL = 0x1100
	hangulV = 0x1161
	hangulT = 0x11A7
	countL  = 19
	countV  = 21
	countT  = 28
	countN  = countV * countT
	countS  = countL * countN
)

// nfc returns the Unicode 17.0.0 Normalization Form C of s. Every step is
// linear in the length of s.
func nfc(s []rune) []rune {
	d := make([]rune, 0, len(s))
	for _, r := range s {
		d = decompose(d, r)
	}
	reorder(d)
	return compose(d)
}

func decompose(d []rune, r rune) []rune {
	if i := r - hangulS; i >= 0 && i < countS {
		d = append(d, hangulL+i/countN, hangulV+i%countN/countT)
		if t := i % countT; t != 0 {
			d = append(d, hangulT+t)
		}
		return d
	}
	if m, ok := decompositions[r]; ok {
		return append(d, []rune(m)...)
	}
	return append(d, r)
}

// reorder applies the canonical ordering algorithm to each run of nonstarters.
// Short runs use insertion sort; long runs use a stable counting sort so that
// a long run of marks stays linear.
func reorder(d []rune) {
	for i := 0; i < len(d); {
		if combiningClass(d[i]) == 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(d) && combiningClass(d[j]) != 0 {
			j++
		}
		run := d[i:j]
		if len(run) <= 16 {
			for k := 1; k < len(run); k++ {
				for m := k; m > 0 && combiningClass(run[m-1]) > combiningClass(run[m]); m-- {
					run[m-1], run[m] = run[m], run[m-1]
				}
			}
		} else {
			var counts [257]int
			for _, r := range run {
				counts[int(combiningClass(r))+1]++
			}
			for k := 1; k < len(counts); k++ {
				counts[k] += counts[k-1]
			}
			sorted := make([]rune, len(run))
			for _, r := range run {
				c := combiningClass(r)
				sorted[counts[c]] = r
				counts[c]++
			}
			copy(run, sorted)
		}
		i = j
	}
}

// compose applies the canonical composition algorithm to a canonically
// ordered decomposition.
func compose(d []rune) []rune {
	out := d[:0]
	starter, last := -1, -1
	for _, r := range d {
		cc := int(combiningClass(r))
		if starter >= 0 && (last == -1 || last < cc) {
			if p, ok := composePair(out[starter], r); ok {
				out[starter] = p
				continue
			}
		}
		if cc == 0 {
			starter, last = len(out), -1
		} else {
			last = cc
		}
		out = append(out, r)
	}
	return out
}

func composePair(a, b rune) (rune, bool) {
	if l, v := a-hangulL, b-hangulV; l >= 0 && l < countL && v >= 0 && v < countV {
		return hangulS + (l*countV+v)*countT, true
	}
	if s, t := a-hangulS, b-hangulT; s >= 0 && s < countS && s%countT == 0 && t > 0 && t < countT {
		return a + t, true
	}
	p, ok := compositions[[2]rune{a, b}]
	return p, ok
}

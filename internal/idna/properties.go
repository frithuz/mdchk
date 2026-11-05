package idna

import "sort"

// status is a UTS #46 IdnaMappingTable.txt status value.
type status uint8

const (
	statusValid status = iota
	statusMapped
	statusDeviation
	statusDisallowed
	statusIgnored
)

type mappingRange struct {
	lo      rune
	status  status
	mapping string
}

// propertyRange assigns value to lo..hi; unlisted code points take the zero
// value of the property (Bidi_Class L, Joining_Type U, ccc 0, not a mark).
type propertyRange struct {
	lo, hi rune
	value  uint8
}

const (
	bidiL = iota
	bidiR
	bidiAL
	bidiAN
	bidiEN
	bidiES
	bidiCS
	bidiET
	bidiON
	bidiBN
	bidiNSM
	bidiOther
)

const (
	joiningU = iota
	joiningD
	joiningL
	joiningR
	joiningT
)

const virama = 9

func lookupMapping(r rune) mappingRange {
	i := sort.Search(len(mappingRanges), func(i int) bool { return mappingRanges[i].lo > r })
	return mappingRanges[i-1]
}

func lookup(table []propertyRange, r rune) uint8 {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	if i < len(table) && table[i].lo <= r {
		return table[i].value
	}
	return 0
}

func combiningClass(r rune) uint8 { return lookup(combiningClasses, r) }
func bidiClass(r rune) uint8      { return lookup(bidiClasses, r) }
func joiningType(r rune) uint8    { return lookup(joiningTypes, r) }
func isMark(r rune) bool          { return lookup(marks, r) != 0 }

// Package fragmentdest classifies a link destination by the document its
// fragment refers to.
package fragmentdest

import "strings"

// Destination says which document a link destination's fragment refers to.
type Destination int

const (
	SameDocument Destination = iota
	OtherDocument
	Remote
)

// Classify inspects the raw destination only up to its first
// fragment marker. Remote and protocol-relative references are never fetched. A
// nonempty path is another document: without resource context a spelling that
// might name this document cannot be proven to do so.
func Classify(destination string) Destination {
	before, _, _ := strings.Cut(destination, "#")
	if strings.HasPrefix(before, "//") {
		return Remote
	}
	for i := 0; i < len(before); i++ {
		b := before[i]
		switch {
		case b == ':':
			if i > 1 {
				return Remote
			}
			return OtherDocument
		case b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || i > 0 && (b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.'):
		default:
			i = len(before)
		}
	}
	path, _, _ := strings.Cut(before, "?")
	if path == "" {
		return SameDocument
	}
	return OtherDocument
}

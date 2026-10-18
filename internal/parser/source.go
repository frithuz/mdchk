// Package parser defines mdchk's in-memory, source-preserving Markdown model,
// physical-line index, diagnostic mapping and structural indentation helpers.
// Parse validates UTF-8, recognizes thematic breaks/ATX headings and accumulates
// staged paragraphs. Documents of blanks, breaks and empty headings complete;
// other input retains whole-document uncertainty until later providers arrive.
// NewLineIndex indexes original bytes, including malformed input, without changing
// Source ownership. Text mapping and semantic helpers require strict UTF-8.
// See model.md for the catalog, outcome contract and pending delivery boundaries.
//
// File loading, configuration, rules, CLI behavior, and reporting belong to
// callers outside this package.
package parser

// Source retains an immutable copy of a document's original bytes. It does not
// normalize line endings, decode text, or validate encoding. Its zero value is
// an empty source. Nil and empty inputs represent the same empty content.
type Source struct {
	content  string
	identity *sourceIdentity
}

// Nonzero size ensures independent snapshots have distinct pointer identities.
// Identity is private ownership metadata, never an output or serialization ID.
type sourceIdentity struct{ marker byte }

// NewSource copies content into a Source. Callers may reuse or modify content
// after NewSource returns without changing the retained bytes.
func NewSource(content []byte) Source {
	return Source{content: string(content), identity: &sourceIdentity{}}
}

// Bytes returns a copy of the original bytes. Modifying the returned slice does
// not change the Source or any other slice returned by Bytes.
func (s Source) Bytes() []byte {
	return []byte(s.content)
}

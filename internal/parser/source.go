// Package parser defines mdchk's in-memory, source-preserving Markdown model,
// physical-line index, diagnostic mapping and structural indentation helpers.
// Parse validates UTF-8, recognizes core block structure and constructs
// source-preserving inline children in paragraphs and headings. Complete results
// affirm all core syntax and reference resolution; interruption remains explicit.
// ParseWithOptions adds independent opaque YAML/TOML header recognition and
// full GFM selection while preserving legacy entry-point defaults. Headers retain
// original source ranges in Document.Frontmatter, outside the ordinary body AST.
// NewLineIndex indexes original bytes, including malformed input, without changing
// Source ownership. Text mapping and semantic helpers require strict UTF-8.
// See model.md for the catalog, outcome contract and source ownership.
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
type sourceIdentity struct{ _ byte }

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

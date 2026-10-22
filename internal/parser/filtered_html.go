package parser

import (
	"context"
	"sort"
	"strings"
)

var filteredHTMLNames = [...]string{"title", "textarea", "style", "xmp", "iframe", "noembed", "noframes", "script", "plaintext"}

// Filtering has its own exact ASCII-four boundary. In particular EOF, VT/FF,
// and a slash without an immediate '>' do not qualify.
func filteredHTMLPrefix(text string) (name string, closing bool, width int) {
	if len(text) < 2 || text[0] != '<' {
		return
	}
	start := 1
	if text[1] == '/' {
		closing = true
		start++
	}
	for _, candidate := range filteredHTMLNames {
		end := start + len(candidate)
		if end >= len(text) {
			continue
		}
		equal := true
		for i := range candidate {
			c := text[start+i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != candidate[i] {
				equal = false
				break
			}
		}
		if equal && filteredHTMLBoundary(text[end:]) {
			return candidate, closing, end
		}
	}
	return "", false, 0
}
func filteredHTMLBoundary(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case ' ', '\t', '\r', '\n', '>':
		return true
	case '/':
		return len(s) > 1 && s[1] == '>'
	}
	return false
}

// This pass adds facts only after both syntax phases. It never visits private
// tail nodes as independent filter owners. Rebuilding retains all private syntax
// evidence and the existing owner's depth; the final parse boundary alone grants
// completion authority.
func filterHTMLTree(ctx context.Context, n *Node) (*Node, error) {
	meterWork(ctx, 1)
	if interruption(ctx) != "" {
		return n, nil
	}
	spec := n.spec
	changed := false
	if n.Kind() == HTMLBlock || n.Kind() == InlineHTML {
		facts, err := filteredHTMLFacts(ctx, n)
		if err != nil {
			return nil, err
		}
		if len(facts) != 0 {
			meterRecords(ctx, 1)
			p := *spec.Properties.HTML
			p.FilteredTags = facts
			spec.Properties.HTML = &p
			changed = true
		}
	} else if len(spec.Children) > 0 {
		spec.Children = meteredSlice[*Node](ctx, len(spec.Children))
		for i, child := range n.spec.Children {
			childCtx := ctx
			if n.Kind() != DocumentKind {
				childCtx = withAncestor(ctx, ancestorDepth(ctx)+1)
			}
			next, err := filterHTMLTree(childCtx, child)
			if err != nil {
				return nil, err
			}
			spec.Children[i] = next
			changed = changed || next != child
		}
	}
	if interruption(ctx) != "" || !changed {
		return n, nil
	}
	next, err := meteredNode(ctx, spec)
	if next != nil {
		next.commentOpen, next.commentClose = n.commentOpen, n.commentClose
		next.rawTag, next.rawClosing, next.htmlTail = n.rawTag, n.rawClosing, n.htmlTail
	}
	return next, err
}

func filteredHTMLFacts(ctx context.Context, n *Node) ([]HTMLFilteredTag, error) {
	var text strings.Builder
	scanner := inlineScanner{ctx: ctx}
	mapping := emphasisContent{scanner: &scanner, parts: n.spec.Content, starts: meteredSlice[int](ctx, len(n.spec.Content)+1)}
	for i, part := range n.spec.Content {
		meterWork(ctx, 1)
		if interruption(ctx) != "" {
			return nil, nil
		}
		meteredWriteString(ctx, &text, part.Semantic.Value)
		mapping.starts[i+1] = mapping.starts[i] + len(part.Semantic.Value)
	}
	scanner.text = text.String()
	meterRecords(ctx, 1)
	meterWork(ctx, 1)
	scanner.filterTagCache = make(map[int]int)
	// Only existing confirmed comments exclude payload. Opaque raw interiors do
	// not manufacture nested comments. The form-2 physical closing-line tail is
	// outside the actual comment, including an unclosed confirmed tail comment.
	var excluded []Span
	addComment := func(node *Node) {
		open, close := node.commentOpen, node.commentClose
		for _, marker := range node.spec.Markers {
			meterWork(ctx, 1)
			if marker.Role == "html-comment-open" {
				open = marker.Source
			}
			if marker.Role == "html-comment-close" {
				close = marker.Source
			}
		}
		if open.Present() {
			end := node.Span().End()
			if close.Present() {
				end = close.End()
			}
			excluded = meteredAppend(ctx, excluded, Span{source: open.source, start: open.Start(), end: end, present: true})
		}
	}
	addComment(n)
	for _, tail := range n.htmlTail {
		meterWork(ctx, 1)
		addComment(tail)
	}
	var out []HTMLFilteredTag
	exclude := 0
	for i := 0; i < len(scanner.text); i++ {
		meterWork(ctx, 128) // bounded nine-name comparisons before inspecting candidates
		if i%256 == 0 && interruption(ctx) != "" {
			return nil, nil
		}
		if n.Kind() == InlineHTML && i != 0 {
			break
		}
		if scanner.text[i] != '<' {
			continue
		}
		name, closing, width := filteredHTMLPrefix(scanner.text[i:])
		if width == 0 {
			continue
		}
		prefix, err := mapping.slice(i, i+width)
		if err != nil || interruption(ctx) != "" {
			return nil, err
		}
		angle := prefix[0].Source
		angle.end = angle.start + 1
		for exclude < len(excluded) && excluded[exclude].End() <= angle.Start() {
			meterWork(ctx, 1)
			exclude++
		}
		if exclude < len(excluded) && excluded[exclude].Start() <= angle.Start() {
			continue
		}
		fact := HTMLFilteredTag{Name: name, Closing: closing, Prefix: Value{Segments: prefix, Semantic: Some(scanner.text[i : i+width])}, Affected: angle}
		if end, _ := scanner.inlineHTMLEnd(i); end != 0 {
			parts, err := mapping.slice(i, end)
			if err != nil || interruption(ctx) != "" {
				return nil, err
			}
			meterRecords(ctx, 1)
			fact.Tag = &Value{Segments: parts, Semantic: Some(scanner.text[i:end])}
		}
		out = meteredAppend(ctx, out, fact)
	}
	return out, nil
}

// Supplied facts prove coherence with actual Content, never recognition or
// operation authority. Indexed source association avoids rescanning an owner's
// full payload for every fact. Exact portions can be split; transformed portions
// must retain the identical atomic contribution. Consecutive logical portions
// cannot skip Content to fabricate syntax across a container gap.
func filteredAssociation(content []Segment, value Value) (index, within int, ok bool) {
	if len(value.Segments) == 0 || !value.Semantic.Present {
		return 0, 0, false
	}
	first := value.Segments[0].Source
	i := sort.Search(len(content), func(i int) bool { return content[i].Source.End() > first.Start() })
	offset := 0
	for _, part := range value.Segments {
		if i >= len(content) || !part.Source.Present() || !part.Semantic.Present {
			return 0, 0, false
		}
		owner := content[i]
		a := part.Source.Start() - owner.Source.Start()
		if part.Source.source != owner.Source.source || a < 0 || part.Source.End() > owner.Source.End() || part.Source.End() <= part.Source.Start() {
			return 0, 0, false
		}
		if offset != 0 && a != within {
			return 0, 0, false
		}
		if owner.Transformation == Exact {
			b := a + part.Source.End() - part.Source.Start()
			if part.Transformation != Exact || !owner.Semantic.Present || b > len(owner.Semantic.Value) || part.Semantic.Value != owner.Semantic.Value[a:b] {
				return 0, 0, false
			}
			within = b
		} else {
			if part != owner || within != 0 {
				return 0, 0, false
			}
			within = len(owner.Semantic.Value)
		}
		if len(part.Semantic.Value) > len(value.Semantic.Value)-offset || value.Semantic.Value[offset:offset+len(part.Semantic.Value)] != part.Semantic.Value {
			return 0, 0, false
		}
		offset += len(part.Semantic.Value)
		if within == len(owner.Semantic.Value) {
			i++
			within = 0
		}
	}
	return i, within, offset == len(value.Semantic.Value)
}
func filteredFollowing(content []Segment, i, within, original int) string {
	// Only two boundary bytes are needed. At most three exact contributions can
	// be visited: empty/synthetic contributions cannot manufacture raw spelling.
	var b [2]byte
	count := 0
	for i < len(content) && count < 2 {
		part := content[i]
		if part.Transformation != Exact || !part.Semantic.Present || within >= len(part.Semantic.Value) || part.Source.Start()+within != original {
			break
		}
		n := copy(b[count:], part.Semantic.Value[within:])
		count += n
		original += n
		i++
		within = 0
	}
	return string(b[:count])
}
func validateFilteredSpec(spec NodeSpec) error {
	if spec.Properties.HTML == nil || len(spec.Properties.HTML.FilteredTags) == 0 {
		return nil
	}
	// The additional inventory requires ordered real Content contributions.
	// This is narrow to supplied filtering facts; legacy empty HTML is unchanged.
	previousEnd := -1
	for _, part := range spec.Content {
		if !part.Source.Present() || !part.Semantic.Present || part.Source.End() < previousEnd {
			return ErrModel
		}
		previousEnd = part.Source.End()
	}
	previous := -1
	for _, f := range spec.Properties.HTML.FilteredTags {
		if f.Affected.Start() <= previous || !contains(spec.Envelope, f.Affected) || f.Affected.End()-f.Affected.Start() != 1 {
			return ErrModel
		}
		previous = f.Affected.Start()
		if err := validateValue(spec.Envelope, f.Prefix); err != nil {
			return err
		}
		for _, part := range f.Prefix.Segments {
			if part.Transformation != Exact {
				return ErrModel
			}
		}
		i, within, ok := filteredAssociation(spec.Content, f.Prefix)
		if !ok || f.Prefix.Segments[0].Source.Start() != f.Affected.Start() {
			return ErrModel
		}
		spelling := f.Prefix.Semantic.Value
		// Fixed-size metadata rejects unbounded caller strings before composing.
		if len(spelling) > 11 || len(f.Name) > 9 {
			return ErrModel
		}
		// The fixed prefix and its immediate boundary must really be adjacent
		// original bytes. Logical Content cannot omit bytes within a tag name
		// or after it to manufacture a match. Full Tag mappings may retain the
		// existing multiline/container gaps independently.
		prefixSpan := contentEnvelope(f.Prefix.Segments)
		rawPrefix, err := spec.Envelope.source.Text(prefixSpan)
		if err != nil || rawPrefix != spelling {
			return ErrModel
		}
		name, closing, width := filteredHTMLPrefix(spelling + filteredFollowing(spec.Content, i, within, prefixSpan.End()))
		if width != len(spelling) || name == "" || name != f.Name || closing != f.Closing {
			return ErrModel
		}
		raw, _ := spec.Envelope.source.Text(f.Affected)
		if raw != "<" {
			return ErrModel
		}
		if f.Tag != nil {
			if err := validateValue(spec.Envelope, *f.Tag); err != nil {
				return err
			}
			_, _, ok := filteredAssociation(spec.Content, *f.Tag)
			if !ok || f.Tag.Segments[0].Source.Start() != f.Affected.Start() || !strings.HasPrefix(f.Tag.Semantic.Value, spelling) || !strings.HasSuffix(f.Tag.Semantic.Value, ">") || len(f.Tag.Semantic.Value) <= len(spelling) {
				return ErrModel
			}
		}
	}
	return nil
}

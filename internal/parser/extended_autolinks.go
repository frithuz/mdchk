package parser

import "strings"

// bareIndex contains suffix endpoints and prefix facts for one eligible raw
// logical interval. Queries never rescan a shared failed domain or path suffix.
type bareIndex struct {
	web, domain, local, mail, resource              int
	dots, doubles, underscore, lastDot, previousDot int
	open, close, alnum                              int
	clean, closingChain                             int
}
type bareMatch struct {
	start, end int
	prefix     string
}
type bareImage struct {
	start, offset int
	parts         []Segment
	flat          []*Node
	delimiters    []inlineDelimiter
}

func (s *inlineScanner) extendedEnabled() bool {
	p, ok := s.ctx.(*parserContext)
	return ok && p.profile.enabled(bareOnlyProfile)
}
func bareWhitespace(c byte) bool { return c == ' ' || c >= '\t' && c <= '\r' }
func bareDomain(c byte) bool     { return asciiAlphanumeric(c) || c == '_' || c == '-' || c == '.' }
func bareLocal(c byte) bool      { return bareDomain(c) || c == '+' }
func bareResource(c byte) bool   { return asciiAlphanumeric(c) || c == '@' || c == '.' }
func barePunctuation(c byte) bool {
	switch c {
	case '?', '!', '.', ',', ':', '*', '_', '~':
		return true
	}
	return false
}

func (s *inlineScanner) bareIndexes(text string) ([]bareIndex, []int) {
	meterWork(s.ctx, 16*(len(text)+1)) // zero-initialize the fixed-width index records before allocation
	x := meteredSlice[bareIndex](s.ctx, len(text)+1)
	x[0].underscore, x[0].lastDot, x[0].previousDot = -1, -1, -1
	var closers []int
	for i := 0; i < len(text); i++ {
		meterWork(s.ctx, 32)
		if i%256 == 0 && interruption(s.ctx) != "" {
			return nil, nil
		}
		a := x[i]
		if text[i] == '.' {
			a.dots++
			a.previousDot, a.lastDot = a.lastDot, i
			if i > 0 && text[i-1] == '.' {
				a.doubles++
			}
		}
		if text[i] == '_' {
			a.underscore = i
		}
		if text[i] == '(' {
			a.open++
		}
		if text[i] == ')' {
			a.close++
			closers = meteredAppend(s.ctx, closers, i)
		}
		if !asciiAlphanumeric(text[i]) {
			a.alnum = i + 1
		}
		// Collapse punctuation/entity suffixes without parentheses. These
		// removals contain no parentheses, so terminal close events form a
		// chain with consecutively decreasing whole-prefix close counts.
		a.clean = i + 1
		if barePunctuation(text[i]) {
			a.clean = x[i].clean
		} else if text[i] == ';' {
			j := x[i].alnum
			if j < i && j > 0 && text[j-1] == '&' {
				a.clean = x[j-1].clean
			}
		}
		a.closingChain = 0
		if a.clean <= i {
			a.closingChain = x[a.clean].closingChain
		} else if text[i] == ')' {
			a.closingChain = 1 + x[i].closingChain
		}
		x[i+1] = a
	}
	for i := len(text) - 1; i >= 0; i-- {
		meterWork(s.ctx, 16)
		if i%256 == 0 && interruption(s.ctx) != "" {
			return nil, nil
		}
		x[i].web, x[i].domain, x[i].local, x[i].mail, x[i].resource = i, i, i, i, i
		if !bareWhitespace(text[i]) && text[i] != '<' {
			x[i].web = max(i+1, x[i+1].web)
		}
		if bareDomain(text[i]) {
			x[i].domain = max(i+1, x[i+1].domain)
		}
		if bareLocal(text[i]) {
			x[i].local = max(i+1, x[i+1].local)
		}
		if bareLocal(text[i]) || text[i] == '@' {
			x[i].mail = max(i+1, x[i+1].mail)
		}
		if bareResource(text[i]) {
			x[i].resource = max(i+1, x[i+1].resource)
		}
	}
	return x, closers
}

func validBareDomain(text string, x []bareIndex, a, b int, web bool) bool {
	if a >= b || text[a] == '.' || text[b-1] == '.' || x[b].dots-x[a].dots < 1 || x[b].doubles != x[a+1].doubles {
		return false
	}
	if web {
		// Underscores are permitted only before the last two segments.
		return x[b].underscore < max(a, x[b].previousDot+1)
	}
	return text[b-1] != '-' && text[b-1] != '_'
}

func (s *inlineScanner) bareMatches(a, b int) []bareMatch {
	text := s.text[a:b]
	x, closers := s.bareIndexes(text)
	if interruption(s.ctx) != "" {
		return nil
	}
	var out []bareMatch
	for i := 0; i < len(text); {
		meterWork(s.ctx, 8)
		if interruption(s.ctx) != "" {
			return nil
		}
		start, end, prefix := i, 0, ""
		boundary := a+i == 0 || bareWhitespace(s.text[a+i-1]) || strings.IndexByte("*_~(", s.text[a+i-1]) >= 0 || a+i > 0 && len(s.failedAngles) != 0 && s.failedAngles[a+i-1]
		domain := -1
		if boundary {
			switch {
			case meteredHasPrefix(s.ctx, text[i:], "www."):
				domain, prefix = i+4, "http://"
			case meteredHasPrefix(s.ctx, text[i:], "http://"):
				domain = i + 7
			case meteredHasPrefix(s.ctx, text[i:], "https://"):
				domain = i + 8
			}
		}
		if domain >= 0 && domain < len(text) {
			meterWork(s.ctx, 16)
			// Fixed-point punctuation/entity cleanup is indexed. Removing an
			// excess terminal ')' exposes the next cleaned prefix; each such
			// event decreases close-open by exactly one. Global close ordinals
			// select that endpoint directly, even for overlapping failed starts.
			end = x[x[i].web].clean
			excess := max(0, x[end].close-x[i].close-(x[end].open-x[i].open))
			remove := min(excess, x[end].closingChain)
			if remove > 0 {
				end = x[closers[x[end].close-remove]].clean
			}
			// Domain validity is a fact of the fully trimmed survivor, never
			// a gate that can prematurely reject a removable underscore tail.
			if !validBareDomain(text, x, domain, min(x[domain].domain, end), true) {
				end = 0
			}
		}
		if end == 0 {
			prefix = "mailto:"
			local, protocol := i, ""
			switch {
			case meteredHasPrefix(s.ctx, text[i:], "mailto:"):
				local, protocol, prefix = i+7, "mailto:", ""
			case meteredHasPrefix(s.ctx, text[i:], "xmpp:"):
				local, protocol, prefix = i+5, "xmpp:", ""
			}
			// A plain address starts at the maximal local token. Protocols may
			// start anywhere; a failed token never retries each local suffix.
			if local < len(text) && (protocol != "" || i == 0 || !bareLocal(text[i-1])) {
				at := x[local].local
				if at > local && at < len(text) && text[at] == '@' {
					stop := x[at+1].mail
					d := stop
					for d > at+1 && text[d-1] == '.' {
						meterWork(s.ctx, 1)
						d--
					}
					if interruption(s.ctx) != "" {
						return nil
					}
					if x[at+1].domain >= d && validBareDomain(text, x, at+1, d, false) {
						end = d
						if protocol == "xmpp:" && d == stop && d+1 < len(text) && text[d] == '/' && x[d+1].resource > d+1 {
							end = x[d+1].resource
						}
					} else {
						// Consume the invalid maximal address as one lexical unit.
						i = max(i+1, stop)
						continue
					}
				}
			}
		}
		if end > start {
			out = meteredAppend(s.ctx, out, bareMatch{a + start, a + end, prefix})
			i = end
		} else {
			i++
		}
	}
	return out
}

// logicalBoundary maps physical endpoints through the borrowed original block
// mappings. Only Exact segments may be split; transformed contributions are atomic.
func (s *inlineScanner) logicalBoundary(parts []Segment, starts []int, physical int) (int, error) {
	lo, hi := 0, len(parts)
	for lo < hi {
		meterWork(s.ctx, 1)
		m := lo + (hi-lo)/2
		if parts[m].Source.end < physical {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == len(parts) {
		return 0, ErrModel
	}
	p := parts[lo]
	if physical < p.Source.start {
		return 0, ErrModel
	}
	if physical == p.Source.end {
		return starts[lo+1], nil
	}
	if physical == p.Source.start {
		return starts[lo], nil
	}
	if p.Transformation != Exact {
		return 0, ErrModel
	}
	return starts[lo] + physical - p.Source.start, nil
}

func (s *inlineScanner) extendedAutolinks(parts []Segment, flat []*Node) ([]*Node, error) {
	if !s.extendedEnabled() {
		return flat, nil
	}
	// Image labels were scanned structurally once. Their retained flat/raw facts
	// allow extension pairing after the enclosing explicit link has won. Existing
	// completed core image children remain authoritative inside successful Links.
	var images map[int]*Node
	if len(s.images) != 0 {
		meterRecords(s.ctx, 1)
		meterWork(s.ctx, 8)
		images = make(map[int]*Node)
		stack := meteredAppend(s.ctx, []*Node(nil), flat...)
		for len(stack) != 0 {
			meterWork(s.ctx, 1)
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n.Kind() == Link {
				continue
			}
			if n.Kind() == Image {
				meterRecords(s.ctx, 1)
				meterWork(s.ctx, 8) // entry/bucket growth is amortized across insertions
				images[n.Span().Start()] = n
			}
			stack = meteredAppend(s.ctx, stack, n.spec.Children...)
		}
	}
	for _, image := range s.images {
		meterWork(s.ctx, 1)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		original := images[image.start]
		if original == nil {
			continue
		} // successful outer Link owns this label
		labelScanner := inlineScanner{ctx: s.ctx, text: s.text, delimiters: image.delimiters, failedAngles: s.failedAngles}
		children, err := labelScanner.barePass(image.parts, image.flat, image.offset, images)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		children, err = labelScanner.emphasisAbove(image.parts, children, -1, image.offset)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		spec := original.spec
		spec.Children = children
		images[image.start], err = meteredNode(s.ctx, spec)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
	}
	return s.barePass(parts, flat, 0, images)
}

func (s *inlineScanner) barePass(parts []Segment, flat []*Node, offset int, images map[int]*Node) ([]*Node, error) {
	content := emphasisContent{scanner: s, parts: parts, starts: meteredSlice[int](s.ctx, len(parts)+1)}
	content.starts[0] = offset
	for i, p := range parts {
		meterWork(s.ctx, 1)
		content.starts[i+1] = content.starts[i] + len(p.Semantic.Value)
	}
	var out []*Node
	var matches []bareMatch
	for i := 0; i < len(flat); {
		meterWork(s.ctx, 1)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		if flat[i].Kind() != Text {
			n := flat[i]
			if replacement := images[n.Span().Start()]; n.Kind() == Image && replacement != nil {
				n = replacement
			}
			out = meteredAppend(s.ctx, out, n)
			i++
			continue
		}
		first := i
		for i < len(flat) && flat[i].Kind() == Text {
			meterWork(s.ctx, 1)
			i++
		}
		a, err := s.logicalBoundary(parts, content.starts, flat[first].Span().Start())
		if err != nil {
			return nil, err
		}
		b, err := s.logicalBoundary(parts, content.starts, flat[i-1].Span().End())
		if err != nil {
			return nil, err
		}
		found := s.bareMatches(a, b)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		if len(found) == 0 {
			out = meteredAppend(s.ctx, out, flat[first:i]...)
			continue
		}
		matches = meteredAppend(s.ctx, matches, found...)
		// The structural scan retains discarded pre-soft-break spelling as
		// terminal Normalized/empty contributions. Borrow those established
		// facts while reconstructing raw escapes for the selected 615 priority.
		tail := flat[i-1].spec.Content
		firstDiscarded := len(tail)
		for firstDiscarded > 0 && tail[firstDiscarded-1].Transformation == Normalized && tail[firstDiscarded-1].Semantic == Some("") {
			meterWork(s.ctx, 1)
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			firstDiscarded--
		}
		discarded := tail[firstDiscarded:]
		discardStart := b
		if len(discarded) != 0 {
			discardStart, err = s.logicalBoundary(parts, content.starts, discarded[0].Source.Start())
			if err != nil {
				return nil, err
			}
		}
		appendText := func(end int) error {
			if a == end {
				return nil
			}
			raw, err := content.slice(a, min(end, discardStart))
			if err != nil || interruption(s.ctx) != "" {
				return err
			}
			value := decodeValue(s.ctx, raw, false)
			if end > discardStart {
				value.Segments = meteredAppend(s.ctx, value.Segments, discarded...)
			}
			if interruption(s.ctx) != "" {
				return nil
			}
			n, err := meteredNode(s.ctx, NodeSpec{Kind: Text, Envelope: contentEnvelope(value.Segments), Content: value.Segments, Properties: Properties{Literal: value.Semantic}})
			if err == nil {
				out = meteredAppend(s.ctx, out, n)
			}
			a = end
			return err
		}
		for _, m := range found {
			if err := appendText(m.start); err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			value, err := content.linkValue(m.start, m.end, false)
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			destination := value
			if m.prefix != "" {
				meterRecords(s.ctx, 1)
				destination = Value{Semantic: Some(meteredConcat(s.ctx, m.prefix, value.Semantic.Value)), Segments: meteredAppend(s.ctx, []Segment{{Transformation: Synthetic, Semantic: Some(m.prefix)}}, value.Segments...)}
			}
			child, err := meteredNode(s.ctx, NodeSpec{Kind: Text, Envelope: contentEnvelope(value.Segments), Content: value.Segments, Properties: Properties{Literal: value.Semantic}})
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			meterRecords(s.ctx, 2) // temporary property and child handle
			n, err := meteredNode(s.ctx, NodeSpec{Kind: Link, Envelope: contentEnvelope(value.Segments), Content: value.Segments, Properties: Properties{Link: &LinkProperties{Origin: "bare-gfm", Label: value, Destination: destination, Resolution: Available}}, Children: []*Node{child}})
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			out = meteredAppend(s.ctx, out, n)
			a = m.end
		}
		if err := appendText(b); err != nil {
			return nil, err
		}
	}
	// Retire covered delimiter bytes before final pairing, preserving trailing
	// punctuation runs that the recognizer left outside the visible Link.
	j := 0
	for i := range s.delimiters {
		meterWork(s.ctx, 1)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		d := &s.delimiters[i]
		if d.retired {
			continue
		}
		for j < len(matches) && matches[j].end <= d.offset {
			meterWork(s.ctx, 1)
			j++
		}
		if j == len(matches) || matches[j].start >= d.offset+d.width {
			continue
		}
		m := matches[j]
		if m.start <= d.offset && m.end >= d.offset+d.width {
			d.canOpen, d.canClose = false, false
		} else if m.start <= d.offset {
			d.width -= m.end - d.offset
			d.offset = m.end
			d.original = d.width
		} else {
			d.width = m.start - d.offset
			d.original = d.width
		}
	}
	return out, nil
}

// validateBareSpec validates supplied provenance, not Markdown recognition.
// Every visible contribution is owned by this exact snapshot; only the allowed
// destination prefix may be Synthetic and source absent.
func validateBareSpec(spec NodeSpec) error {
	p := spec.Properties.Link
	if p == nil || p.Origin != "bare-gfm" {
		return nil
	}
	if spec.Kind != Link || p.Resolution != Available || p.Title != nil || p.ReferenceLabel != nil || p.NormalizedLabel.Present || len(spec.Markers) != 0 || spec.Properties.Literal.Present || len(spec.Content) == 0 || !p.Label.Semantic.Present || p.Label.Semantic.Value == "" || !p.Destination.Semantic.Present {
		return ErrModel
	}
	if spec.Content[0].Source.Start() != spec.Envelope.Start() || spec.Content[len(spec.Content)-1].Source.End() != spec.Envelope.End() || len(p.Label.Segments) != len(spec.Content) {
		return ErrModel
	}
	check := func(parts []Segment, semantic string) bool {
		n, previous := 0, spec.Envelope.Start()
		for _, part := range parts {
			if !contains(spec.Envelope, part.Source) || part.Source.End() <= part.Source.Start() || part.Source.Start() < previous || !part.Semantic.Present || part.Transformation == Synthetic || part.Transformation == MappingUnavailable {
				return false
			}
			v := part.Semantic.Value
			if len(v) > len(semantic)-n || semantic[n:n+len(v)] != v {
				return false
			}
			n += len(v)
			previous = part.Source.End()
		}
		return n == len(semantic)
	}
	if !check(spec.Content, p.Label.Semantic.Value) {
		return ErrModel
	}
	for i, part := range spec.Content {
		if part != p.Label.Segments[i] {
			return ErrModel
		}
	}
	if len(spec.Children) != 1 || spec.Children[0] == nil {
		return ErrModel
	}
	child := spec.Children[0].spec
	if child.Kind != Text || child.Envelope != spec.Envelope || child.Properties.Literal != p.Label.Semantic || len(child.Content) != len(spec.Content) {
		return ErrModel
	}
	for i, part := range spec.Content {
		if child.Content[i] != part {
			return ErrModel
		}
	}
	d := p.Destination
	label := p.Label.Semantic.Value
	explicit := strings.HasPrefix(label, "http://") || strings.HasPrefix(label, "https://") || strings.HasPrefix(label, "mailto:") || strings.HasPrefix(label, "xmpp:")
	if len(d.Segments) > 0 && d.Segments[0].Transformation == Synthetic {
		s := d.Segments[0]
		prefix := s.Semantic.Value
		// The supplied prefix retains the recognizer's selected association.
		// A www-looking local part may be email even where web cannot open.
		allowed := prefix == "http://" && strings.HasPrefix(label, "www.") || prefix == "mailto:" && strings.IndexByte(label, '@') > 0
		if explicit || !allowed || s.Source.Present() || !s.Semantic.Present || !strings.HasPrefix(d.Semantic.Value, prefix) {
			return ErrModel
		}
		d.Segments = d.Segments[1:]
		d.Semantic = Some(d.Semantic.Value[len(prefix):])
	} else if !explicit {
		return ErrModel
	}

	if d.Semantic != p.Label.Semantic || len(d.Segments) != len(spec.Content) {
		return ErrModel
	}
	for i, part := range spec.Content {
		if part != d.Segments[i] {
			return ErrModel
		}
	}
	return nil
}

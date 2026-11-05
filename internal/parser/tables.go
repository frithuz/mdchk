package parser

import "context"

// TableAlignment is a delimiter cell's semantic alignment, separate from its
// exact colon/dash markers. None is an affirmative absence of alignment.
type TableAlignment string

const (
	AlignNone   TableAlignment = "none"
	AlignLeft   TableAlignment = "left"
	AlignCenter TableAlignment = "center"
	AlignRight  TableAlignment = "right"
)

type TableProperties struct {
	Width      int
	Alignments []TableAlignment
}

// TableRowProperties retains every actual separator, including optional outer
// pipes. RawCells counts source children before semantic truncation/padding.
// Padding is a spanless count, never a fabricated editable cell.
type TableRowProperties struct {
	Role                      string // header, delimiter, body
	RawCells, Padding         int
	Separators                []Span
	LeadingPipe, TrailingPipe Span
}

type TableCellProperties struct {
	EscapedPipes []Span                   // complete original backslash-pipe spellings
	Alignment    Optional[TableAlignment] // delimiter cells only
}

func validAlignment(a TableAlignment) bool {
	return a == AlignNone || a == AlignLeft || a == AlignCenter || a == AlignRight
}

func validateTableSpec(s NodeSpec) error {
	p := s.Properties
	if (p.Table != nil) != (s.Kind == Table) || (p.Row != nil) != (s.Kind == TableRow) || (p.Cell != nil) != (s.Kind == TableCell) {
		return ErrModel
	}
	if p.Row != nil && (len(s.Content) != 0 || len(s.Markers) != 0 || p.Literal.Present) {
		return ErrModel
	}
	if p.Table != nil {
		t := p.Table
		if t.Width < 1 || len(t.Alignments) != t.Width || len(s.Children) < 2 {
			return ErrModel
		}
		for _, a := range t.Alignments {
			if !validAlignment(a) {
				return ErrModel
			}
		}
		previousEnd := s.Envelope.Start()
		for i, row := range s.Children {
			if row == nil || row.Kind() != TableRow || row.spec.Properties.Row == nil {
				return ErrModel
			}
			if row.Span().Start() < previousEnd || i == 0 && row.Span().Start() != s.Envelope.Start() {
				return ErrModel
			}
			previousEnd = row.Span().End()
			r := row.spec.Properties.Row
			role := "body"
			if i == 0 {
				role = "header"
			}
			if i == 1 {
				role = "delimiter"
			}
			if r.Role != role || r.Padding != max(0, t.Width-r.RawCells) || i < 2 && r.RawCells != t.Width {
				return ErrModel
			}
			if i == 1 {
				for j, cell := range row.spec.Children {
					if cell.spec.Properties.Cell.Alignment != Some(t.Alignments[j]) {
						return ErrModel
					}
				}
			}
		}
		if len(s.Content) != len(s.Children) {
			return ErrModel
		}
		for i, segment := range s.Content {
			if segment.Source != s.Children[i].Span() || segment.Transformation != Exact {
				return ErrModel
			}
		}
		if previousEnd != s.Envelope.End() {
			return ErrModel
		}
	}
	if p.Row != nil {
		r := p.Row
		if r.Role != "header" && r.Role != "delimiter" && r.Role != "body" || r.RawCells < 1 || r.RawCells != len(s.Children) || r.Padding < 0 || r.Role != "body" && r.Padding != 0 {
			return ErrModel
		}
		previous := -1
		for _, pipe := range r.Separators {
			if !contains(s.Envelope, pipe) || pipe.End()-pipe.Start() != 1 || pipe.Start() <= previous || pipe.source.content[pipe.Start()] != '|' {
				return ErrModel
			}
			previous = pipe.Start()
		}
		for _, outer := range []Span{r.LeadingPipe, r.TrailingPipe} {
			if !outer.Present() {
				continue
			}
			found := false
			for _, pipe := range r.Separators {
				if pipe == outer {
					found = true
					break
				}
			}
			if !found {
				return ErrModel
			}
		}
		want := r.RawCells - 1
		if r.LeadingPipe.Present() {
			want++
		}
		if r.TrailingPipe.Present() {
			want++
		}
		if len(r.Separators) != want {
			return ErrModel
		}
		text := s.Envelope.source.content
		start, end := s.Envelope.Start(), s.Envelope.End()
		if end > start && text[end-1] == '\n' {
			end--
		}
		if end > start && text[end-1] == '\r' {
			end--
		}
		trimStart, trimEnd := start, end
		for trimStart < trimEnd && (text[trimStart] == ' ' || text[trimStart] == '\t') {
			trimStart++
		}
		for trimEnd > trimStart && (text[trimEnd-1] == ' ' || text[trimEnd-1] == '\t') {
			trimEnd--
		}
		pipeIndex, slashes := 0, 0
		for i := start; i < end; i++ {
			if text[i] == '\r' || text[i] == '\n' {
				return ErrModel
			}
			if text[i] == '|' && slashes%2 == 0 {
				if pipeIndex >= len(r.Separators) || r.Separators[pipeIndex].Start() != i {
					return ErrModel
				}
				pipeIndex++
			}
			if text[i] == '\\' {
				slashes++
			} else {
				slashes = 0
			}
		}
		if pipeIndex != len(r.Separators) {
			return ErrModel
		}
		fieldStart, fieldEnd := start, end
		first, last := 0, len(r.Separators)
		if r.LeadingPipe.Present() {
			if r.LeadingPipe != r.Separators[0] || r.LeadingPipe.Start() != trimStart {
				return ErrModel
			}
			fieldStart = r.LeadingPipe.End()
			first++
		}
		if r.TrailingPipe.Present() {
			if r.TrailingPipe != r.Separators[last-1] || r.TrailingPipe.End() != trimEnd {
				return ErrModel
			}
			fieldEnd = r.TrailingPipe.Start()
			last--
		}
		for i, cell := range s.Children {
			if cell == nil || cell.Kind() != TableCell || cell.spec.Properties.Cell == nil || cell.spec.Properties.Cell.Alignment.Present != (r.Role == "delimiter") {
				return ErrModel
			}
			end := fieldEnd
			if first+i < last {
				end = r.Separators[first+i].Start()
			}
			if cell.Span().Start() != fieldStart || cell.Span().End() != end {
				return ErrModel
			}
			if first+i < last {
				fieldStart = r.Separators[first+i].End()
			}
		}
	}
	if p.Cell != nil {
		if p.Cell.Alignment.Present && (!validAlignment(p.Cell.Alignment.Value) || len(s.Children) != 0) {
			return ErrModel
		}
		if p.Cell.Alignment.Present {
			if !validateDelimiterCell(s) {
				return ErrModel
			}
		} else if len(s.Markers) != 0 {
			return ErrModel
		}
		text := s.Envelope.source.content
		start, end := s.Envelope.Start(), s.Envelope.End()
		escapeIndex, slashes := 0, 0
		for i := start; i < end; i++ {
			if text[i] == '|' {
				if slashes%2 == 0 || escapeIndex >= len(p.Cell.EscapedPipes) || p.Cell.EscapedPipes[escapeIndex].Start() != i-1 {
					return ErrModel
				}
				escapeIndex++
			}
			if text[i] == '\\' {
				slashes++
			} else {
				slashes = 0
			}
		}
		if escapeIndex != len(p.Cell.EscapedPipes) {
			return ErrModel
		}
		if !p.Cell.Alignment.Present {
			for start < end && (text[start] == ' ' || text[start] == '\t') {
				start++
			}
			for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
				end--
			}
			position := start
			for _, segment := range s.Content {
				if segment.Source.Start() != position {
					return ErrModel
				}
				position = segment.Source.End()
			}
			if position != end {
				return ErrModel
			}
		}
		previous := -1
		for _, escape := range p.Cell.EscapedPipes {
			if !contains(s.Envelope, escape) || escape.End()-escape.Start() != 2 || escape.Start() <= previous || escape.source.content[escape.Start():escape.End()] != "\\|" {
				return ErrModel
			}
			previous = escape.Start()
		}
	}
	return nil
}

type tableBuilder struct {
	start, end int
	width      int
	alignments []TableAlignment
	rows       []*Node
	content    []Segment
}

type tableRowCandidate struct {
	spec  NodeSpec
	cells []NodeSpec
}

func (p *blockParser) freezeTableRow(candidate *tableRowCandidate) (*Node, error) {
	depth := p.containerSyntaxDepth()
	checkDepth(p.ctx, depth+3)
	spec := candidate.spec
	for i, cell := range candidate.cells {
		if i%256 == 0 && interruption(p.ctx) != "" {
			return nil, nil
		}
		node, err := meteredNode(withAncestor(p.ctx, depth+2), cell)
		if err != nil {
			return nil, err
		}
		spec.Children = meteredAppend(p.ctx, spec.Children, node)
	}
	return meteredNode(withAncestor(p.ctx, depth+1), spec)
}

func (p *blockParser) appendTableRow(row *Node) {
	p.table.rows = meteredAppend(p.ctx, p.table.rows, row)
	span := row.Span()
	p.table.content = meteredAppend(p.ctx, p.table.content, Segment{Source: span, Transformation: Exact, Semantic: Some(p.index.source.content[span.Start():span.End()])})
	p.table.end = span.End()
}

func (p *blockParser) extensionEnabled(extension parserProfile) bool {
	c, ok := p.ctx.(*parserContext)
	return ok && c.profile.enabled(extension)
}

// pipeRowShape is the separator classification of one row-candidate line.
// Start and End exclude edge spaces and tabs. Separators counts structural
// pipes, and Cells is the source cell count the table splitter produces.
// Delimiter reports that every cell is a valid delimiter cell; it is only
// computed when requested.
type pipeRowShape struct {
	start, end, separators, cells int
	leading, trailing, delimiter  bool
}

// trimPipeRowEdges removes spaces and tabs at both edges of text[start:end].
// tick is called once per removed byte with that edge's offset and stops the
// trim when it returns false.
func trimPipeRowEdges(text string, start, end int, tick func(int) bool) (int, int, bool) {
	for start < end && (text[start] == ' ' || text[start] == '\t') {
		if !tick(start) {
			return 0, 0, false
		}
		start++
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		if !tick(end) {
			return 0, 0, false
		}
		end--
	}
	return start, end, true
}

// pipeDelimiterCell matches a trimmed cell text[start:end] against :?-+:? and
// returns the dash run bounds and its colons.
func pipeDelimiterCell(text string, start, end int, tick func(int) bool) (left, right int, hasLeft, hasRight, ok bool) {
	left, right = start, end
	hasLeft = left < right && text[left] == ':'
	if hasLeft {
		left++
	}
	if left < right && text[right-1] == ':' {
		right--
		hasRight = true
	}
	if left == right {
		return 0, 0, false, false, false
	}
	for j := left; j < right; j++ {
		if !tick(j - left) {
			return 0, 0, false, false, false
		}
		if text[j] != '-' {
			return 0, 0, false, false, false
		}
	}
	return left, right, hasLeft, hasRight, true
}

// classifyPipeRow is the separator classification shared by the table splitter
// and Document.PipeRowCandidates. It works on original bytes: a pipe is escaped
// when the backslash run before it has odd length, and backticks and emphasis
// markers have no separator authority. pipe, when not nil, receives each pipe
// in order. tick meters each examined byte; a false result stops the scan.
func classifyPipeRow(text string, start, end int, delimiter bool, tick func(int) bool, pipe func(at int, escaped bool)) (pipeRowShape, bool) {
	start, end, ok := trimPipeRowEdges(text, start, end, tick)
	if !ok {
		return pipeRowShape{}, false
	}
	shape := pipeRowShape{start: start, end: end, delimiter: delimiter}
	// A delimiter cell is checked as soon as its closing separator is known;
	// the final field is checked after the scan unless a trailing pipe closes
	// the row.
	cell := func(a, b int) bool {
		a, b, ok := trimPipeRowEdges(text, a, b, tick)
		if !ok {
			return false
		}
		if shape.delimiter {
			_, _, _, _, shape.delimiter = pipeDelimiterCell(text, a, b, tick)
		}
		return true
	}
	slashes, field, last := 0, start, -1
	for i := start; i < end; i++ {
		if !tick(i - start) {
			return pipeRowShape{}, false
		}
		if text[i] == '|' {
			if pipe != nil {
				pipe(i, slashes%2 != 0)
			}
			if slashes%2 == 0 {
				if shape.separators == 0 && i == start {
					shape.leading = true
				} else if delimiter && !cell(field, i) {
					return pipeRowShape{}, false
				}
				shape.separators++
				field, last = i+1, i
			}
		}
		if text[i] == '\\' {
			slashes++
		} else {
			slashes = 0
		}
	}
	interior := shape.separators
	if shape.leading {
		interior--
	}
	shape.trailing = interior > 0 && last == end-1
	if !shape.trailing && delimiter && !cell(field, end) {
		return pipeRowShape{}, false
	}
	shape.cells = interior + 1
	if shape.trailing {
		shape.cells--
	}
	return shape, true
}

// splitTableRow works on original logical bytes before inline parsing, with
// the separator classification of classifyPipeRow. mdchk explicitly chooses
// odd/even backslash-run grouping consistent with core punctuation escapes;
// GFM 4.10 does not unambiguously specify longer-run precedence. See
// docs/engineering/gfm-compatibility.md for this recorded interpretation.
func (p *blockParser) splitTableRow(line blockLine, role string, width int) (*tableRowCandidate, []TableAlignment, bool, error) {
	text := p.index.source.content
	tick := func(at int) bool {
		meterWork(p.ctx, 1)
		return at%256 != 0 || interruption(p.ctx) == ""
	}
	var separators, escapes []Span
	shape, ok := classifyPipeRow(text, line.start, line.end, false, tick, func(i int, escaped bool) {
		if escaped {
			escapes = meteredAppend(p.ctx, escapes, p.index.span(i-1, i+1))
		} else {
			separators = meteredAppend(p.ctx, separators, p.index.span(i, i+1))
		}
	})
	if !ok {
		return nil, nil, false, nil
	}
	start, end := shape.start, shape.end
	meterRecords(p.ctx, 1)
	row := &TableRowProperties{Role: role, Separators: separators}
	fieldStart, fieldEnd := line.start, line.end
	first, last := 0, len(separators)
	if shape.leading {
		row.LeadingPipe = separators[first]
		fieldStart = start + 1
		first++
	}
	if shape.trailing {
		row.TrailingPipe = separators[last-1]
		fieldEnd = end - 1
		last--
	}
	// A single outer pipe still denotes one real empty field, not zero cells.
	if fieldEnd < fieldStart {
		fieldEnd = fieldStart
	}
	var cells []NodeSpec
	var alignments []TableAlignment
	escapeIndex := 0
	for i := first; i <= last; i++ {
		meterWork(p.ctx, 1)
		if (i-first)%256 == 0 && interruption(p.ctx) != "" {
			return nil, nil, false, nil
		}
		b := fieldEnd
		if i < last {
			b = separators[i].Start()
		}
		a := fieldStart
		trimA, trimB, ok := trimPipeRowEdges(text, a, b, tick)
		if !ok {
			return nil, nil, false, nil
		}
		meterRecords(p.ctx, 1)
		cell := &TableCellProperties{}
		for escapeIndex < len(escapes) && escapes[escapeIndex].Start() < b {
			if escapeIndex%256 == 0 && interruption(p.ctx) != "" {
				return nil, nil, false, nil
			}
			cell.EscapedPipes = meteredAppend(p.ctx, cell.EscapedPipes, escapes[escapeIndex])
			escapeIndex++
		}
		var markers []Marker
		var content []Segment
		if role == "delimiter" {
			left, right, hasLeft, hasRight, ok := pipeDelimiterCell(text, trimA, trimB, tick)
			if !ok {
				return nil, nil, false, nil
			}
			if hasLeft {
				markers = meteredAppend(p.ctx, markers, Marker{Role: "left-colon", Source: p.index.span(trimA, trimA+1)})
			}
			markers = meteredAppend(p.ctx, markers, Marker{Role: "dash-run", Source: p.index.span(left, right)})
			if hasRight {
				markers = meteredAppend(p.ctx, markers, Marker{Role: "right-colon", Source: p.index.span(right, right+1)})
			}
			alignment := AlignNone
			if hasLeft {
				alignment = AlignLeft
			}
			if hasRight {
				alignment = AlignRight
			}
			if hasLeft && hasRight {
				alignment = AlignCenter
			}
			cell.Alignment = Some(alignment)
			meterRecords(p.ctx, 1)
			alignments = meteredAppend(p.ctx, alignments, alignment)
		} else {
			content = tableInlinePipes(p.ctx, p.index.semanticText(trimA, trimB).Segments)
		}
		cells = meteredAppend(p.ctx, cells, NodeSpec{Kind: TableCell, Envelope: p.index.span(a, b), Markers: markers, Content: content, Properties: Properties{Cell: cell}})
		if i < last {
			fieldStart = separators[i].End()
		}
	}
	row.RawCells = len(cells)
	if role == "body" {
		row.Padding = max(0, width-row.RawCells)
	}
	meterRecords(p.ctx, 1)
	return &tableRowCandidate{spec: NodeSpec{Kind: TableRow, Envelope: p.index.span(line.start, line.next), Properties: Properties{Row: row}}, cells: cells}, alignments, true, nil
}

func (p *blockParser) promoteTable(line blockLine) (bool, error) {
	if !p.extensionEnabled(tablesOnlyProfile) || !p.paragraphOpen() || line.lazy || line.indent >= 4 || line.first != '|' && line.first != ':' && line.first != '-' {
		return false, nil
	}
	paragraph := p.stack[len(p.stack)-1]
	// No growing-paragraph rescans: only its final physical line is a possible
	// header, and a multi-line ordinary paragraph cannot be promoted.
	if len(paragraph.content) != 0 && paragraph.reference == nil || paragraph.last.lazy {
		return false, nil
	}
	delimiter, alignments, ok, err := p.splitTableRow(line, "delimiter", 0)
	if !ok || err != nil {
		return false, err
	}
	header, _, ok, err := p.splitTableRow(paragraph.last, "header", 0)
	if !ok || err != nil {
		return false, err
	}
	if len(header.spec.Properties.Row.Separators) == 0 || len(header.cells) != len(delimiter.cells) {
		return false, nil
	}
	if paragraph.reference != nil {
		if !p.tableHeaderAfterReferences(paragraph.reference) {
			return false, nil
		}
		if err := p.stripReferences(); err != nil {
			return false, err
		}
		if !p.paragraphOpen() || p.referenceInterrupted {
			return false, nil
		}
		paragraph = p.stack[len(p.stack)-1]
	}
	if len(paragraph.content) != 0 {
		return false, nil
	}
	checkDepth(p.ctx, p.containerSyntaxDepth()+3)
	headerNode, err := p.freezeTableRow(header)
	if err != nil || headerNode == nil {
		return false, err
	}
	delimiterNode, err := p.freezeTableRow(delimiter)
	if err != nil || delimiterNode == nil {
		return false, err
	}
	p.stack = p.stack[:len(p.stack)-1]
	meterRecords(p.ctx, 1)
	p.table = &tableBuilder{start: paragraph.last.start, end: line.next, width: len(alignments), alignments: alignments}
	p.appendTableRow(headerNode)
	p.appendTableRow(delimiterNode)
	p.inlinePayload = true
	return true, nil
}

func (p *blockParser) closeTable() error {
	if p.table == nil {
		return nil
	}
	t := p.table
	node, err := meteredNode(withAncestor(p.ctx, p.containerSyntaxDepth()), NodeSpec{Kind: Table, Envelope: p.index.span(t.start, t.end), Children: t.rows, Content: t.content, Properties: Properties{Table: &TableProperties{Width: t.width, Alignments: t.alignments}}})
	if err != nil {
		return err
	}
	p.table = nil
	p.attach(node)
	return nil
}

// tableInlinePipes maps only the table's escaped-pipe contribution before the
// shared inline parser. Its semantic byte is not inline punctuation syntax;
// code, autolinks and raw HTML consume the same mapping without text decoding.
// The complete original backslash-pipe remains the source of each contribution.
func tableInlinePipes(ctx context.Context, parts []Segment) []Segment {
	var out []Segment
	for partIndex, part := range parts {
		meterWork(ctx, 1)
		if partIndex%256 == 0 && interruption(ctx) != "" {
			return nil
		}
		if part.Transformation != Exact {
			out = meteredAppend(ctx, out, part)
			continue
		}
		raw := part.Semantic.Value
		start := 0
		for i := 0; i+1 < len(raw); i++ {
			meterWork(ctx, 1)
			if i%256 == 0 && interruption(ctx) != "" {
				return nil
			}
			if raw[i] != '\\' || raw[i+1] != '|' {
				continue
			}
			if start < i {
				out = meteredAppend(ctx, out, Segment{Source: Span{source: part.Source.source, start: part.Source.start + start, end: part.Source.start + i, present: true}, Transformation: Exact, Semantic: Some(raw[start:i])})
			}
			out = meteredAppend(ctx, out, Segment{Source: Span{source: part.Source.source, start: part.Source.start + i, end: part.Source.start + i + 2, present: true}, Transformation: Escape, Semantic: Some("|")})
			i++
			start = i + 1
		}
		if start < len(raw) {
			out = meteredAppend(ctx, out, Segment{Source: Span{source: part.Source.source, start: part.Source.start + start, end: part.Source.end, present: true}, Transformation: Exact, Semantic: Some(raw[start:])})
		}
	}
	return out
}

func validateDelimiterCell(s NodeSpec) bool {
	if len(s.Content) != 0 || s.Properties.Literal.Present || len(s.Properties.Cell.EscapedPipes) != 0 {
		return false
	}
	text := s.Envelope.source.content
	a, b := s.Envelope.Start(), s.Envelope.End()
	for a < b && (text[a] == ' ' || text[a] == '\t') {
		a++
	}
	for b > a && (text[b-1] == ' ' || text[b-1] == '\t') {
		b--
	}
	marker := 0
	left, right := false, false
	matches := func(role string, start, end int) bool {
		if marker >= len(s.Markers) {
			return false
		}
		m := s.Markers[marker]
		marker++
		return m.Role == role && m.Source.source == s.Envelope.source && m.Source.Start() == start && m.Source.End() == end
	}
	if a < b && text[a] == ':' {
		left = true
		if !matches("left-colon", a, a+1) {
			return false
		}
		a++
	}
	if a < b && text[b-1] == ':' {
		right = true
		b--
	}
	if a == b || !matches("dash-run", a, b) {
		return false
	}
	for i := a; i < b; i++ {
		if text[i] != '-' {
			return false
		}
	}
	if right && !matches("right-colon", b, b+1) {
		return false
	}
	alignment := AlignNone
	if left {
		alignment = AlignLeft
	}
	if right {
		alignment = AlignRight
	}
	if left && right {
		alignment = AlignCenter
	}
	return marker == len(s.Markers) && s.Properties.Cell.Alignment == Some(alignment)
}

// A failed table candidate must not close or mutate a still-open reference
// paragraph. Resume long title probes and cache the proved ordinary remainder;
// table lookahead never repeatedly scans a growing title from its opener.
type tableReferenceProbe struct {
	offset    int
	title     *referenceTitleScan
	remainder Optional[int]
}

func (p *blockParser) tableHeaderAfterReferences(r *referenceParagraph) bool {
	probe := &r.tableProbe
	text := r.text.String()
	last := r.parts[len(r.parts)-1].start
	if probe.remainder.Present {
		return probe.remainder.Value == last
	}
	for probe.offset < len(text) {
		meterWork(p.ctx, 1)
		if interruption(p.ctx) != "" {
			return false
		}
		var result referenceScan
		if probe.title != nil {
			result = resumeReferenceTitle(p.ctx, text, probe.title)
		} else {
			result = probeReference(p.ctx, text, probe.offset)
		}
		if result.more {
			probe.title = result.title
			// A single ordinary header with an unfinished label is still eligible;
			// a pending definition title or multiline label cannot be extracted.
			return last == 0 && !result.valid && result.title == nil
		}
		probe.title = nil
		if !result.valid {
			probe.remainder = Some(probe.offset)
			return probe.offset == last
		}
		// A final definition may acquire a next-line title. Keep its opener until
		// another line establishes whether it is a complete leading definition.
		if result.syntax.end == len(text) {
			return false
		}
		probe.offset = result.syntax.end
	}
	return false
}

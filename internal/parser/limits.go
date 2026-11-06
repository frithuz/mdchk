package parser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// Limits is an immutable-by-copy parser profile. Every field must be positive.
// Other application/wire/filesystem limits belong to the owning adapters.
type Limits struct {
	DocumentBytes int64
	SyntaxDepth   int64
	DocumentNodes int64
	TotalNodes    int64
	WorkUnits     int64
	OperationTime time.Duration
}

func DefaultLimits() Limits {
	return Limits{1048576, 256, 250000, 2000000, 250000000, 60 * time.Second}
}

func (l Limits) Validate() error {
	if l.DocumentBytes <= 0 || l.SyntaxDepth <= 0 || l.DocumentNodes <= 0 || l.TotalNodes <= 0 || l.WorkUnits <= 0 || l.OperationTime <= 0 {
		return errors.New("parser limits must be positive finite values")
	}
	maxDocumentBytes := min(int64(1<<30), int64(int(^uint(0)>>1))/16)
	if l.DocumentBytes > maxDocumentBytes || l.SyntaxDepth > 4096 || l.DocumentNodes > l.TotalNodes || l.TotalNodes > 1<<40 || l.WorkUnits > 1<<60 {
		return errors.New("incoherent or unsupported parser limits")
	}
	return nil
}

// LimitExceeded identifies the exhausted budget without requiring message parsing.
// Observed is the attempted amount, capped at MaxInt64 if addition overflows.
type LimitExceeded struct {
	Name              string
	Observed, Allowed int64
}

func (e *LimitExceeded) Error() string {
	return fmt.Sprintf("%s: observed %d, allowed %d", e.Name, e.Observed, e.Allowed)
}

// A private token binds completed syntax facts to their existing operation.
type operationIdentity struct{ _ byte }

// OperationBudget shares cumulative work, construction and elapsed time across
// parses/reparses and future adapters. It is safe for concurrent charging. A new
// parse resets only document-local construction; exhaustion is sticky.
type OperationBudget struct {
	identity    *operationIdentity
	mu          sync.Mutex
	limits      Limits
	started     time.Time
	work, nodes int64
	checkpoints int64
	exceeded    *LimitExceeded
	scale       contentScale
	// encodingErrors counts invalid UTF-8 records retained by this operation. It
	// is a retention allowance across per-target analyses, not a resource limit.
	encodingErrors int64
}

// Per-byte allowances granted for acquired content when scaling is enabled.
// Measured analysis with every rule enabled costs about 2,300 work units and
// 9 nodes per source byte; these rates keep headroom for fix rounds while
// operation-time remains the wall-clock backstop.
const (
	ContentWorkPerByte  = 8192
	ContentNodesPerByte = 32
)

// contentScale grows the work and node allowances with acquired content. A zero
// rate leaves that limit at its fixed allowance.
type contentScale struct {
	workPerByte, nodesPerByte int64
	content                   int64
	workBonus, nodesBonus     int64
}

func NewOperationBudget(l Limits) (*OperationBudget, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &OperationBudget{identity: &operationIdentity{}, limits: l, started: time.Now()}, nil
}

// ScaleWithContent makes the work-units and total-nodes allowances grow by the
// given amounts for every byte later reported to GrantContent, on top of the
// configured limits. A zero rate keeps that limit fixed, which is how an
// operator-set limit stays a hard cap. Call it before any charge.
func (b *OperationBudget) ScaleWithContent(workPerByte, nodesPerByte int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.scale.workPerByte, b.scale.nodesPerByte = max(workPerByte, 0), max(nodesPerByte, 0)
	b.scale.grant(0)
}

// GrantContent raises the scaled allowances for newly acquired source bytes. It
// is a no-op unless ScaleWithContent enabled a rate, and it never lowers an
// allowance or revives an exhausted budget.
func (b *OperationBudget) GrantContent(bytes int64) {
	if b == nil || bytes <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.scale.grant(bytes)
}

func (c *contentScale) grant(bytes int64) {
	c.content = addBounded(c.content, bytes)
	c.workBonus = mulBounded(c.content, c.workPerByte)
	c.nodesBonus = mulBounded(c.content, c.nodesPerByte)
}

func mulBounded(a, n int64) int64 {
	if a != 0 && n > math.MaxInt64/a {
		return math.MaxInt64
	}
	return a * n
}

// Renewed returns a fresh operation budget with the same limits and scaling,
// already granted the content this one acquired. Saved fix runs use it for
// their second half, which re-analyzes the same bytes.
func (b *OperationBudget) Renewed() (*OperationBudget, error) {
	b.mu.Lock()
	scale := b.scale
	b.mu.Unlock()
	fresh, err := NewOperationBudget(b.limits)
	if err != nil {
		return nil, err
	}
	fresh.ScaleWithContent(scale.workPerByte, scale.nodesPerByte)
	fresh.GrantContent(scale.content)
	return fresh, nil
}

// OperationToken is immutable authority identifying one shared budget. Its zero
// value never identifies an operation; callers cannot manufacture token contents.
type OperationToken struct{ identity *operationIdentity }

func (b *OperationBudget) Token() OperationToken {
	if b == nil {
		return OperationToken{}
	}
	return OperationToken{b.identity}
}
func (b *OperationBudget) Limits() Limits { return b.limits }
func addBounded(a, n int64) int64 {
	if n > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + n
}
func (b *OperationBudget) charge(name string, n int64) *LimitExceeded {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chargeLocked(name, n)
}

// Parse holds mu for the accepted single-worker operation. Adapters charge
// between parses; simultaneous users of one operation serialize without races.
func (b *OperationBudget) chargeLocked(name string, n int64) *LimitExceeded {
	if b.exceeded != nil {
		return b.exceeded
	}
	b.checkpoints = addBounded(b.checkpoints, max(n, 1))
	if b.checkpoints >= 256 || n == 0 {
		b.checkpoints = 0
		elapsed := time.Since(b.started)
		if elapsed > b.limits.OperationTime {
			b.exceeded = &LimitExceeded{"operation-time", int64(elapsed), int64(b.limits.OperationTime)}
			return b.exceeded
		}
	}
	var counter *int64
	var allowed int64
	switch name {
	case "work-units":
		counter, allowed = &b.work, addBounded(b.limits.WorkUnits, b.scale.workBonus)
	case "total-nodes":
		counter, allowed = &b.nodes, addBounded(b.limits.TotalNodes, b.scale.nodesBonus)
	default:
		return nil
	}
	observed := addBounded(*counter, n)
	if observed > allowed {
		b.exceeded = &LimitExceeded{name, observed, allowed}
		return b.exceeded
	}
	*counter = observed
	return nil
}

// ChargeWork meters byte inspections/comparisons/copies or candidate/structural
// visits outside Parse. Negative charges are rejected and never refund work.
func (b *OperationBudget) ChargeWork(units int64) error {
	if b == nil {
		return errors.New("nil parser operation budget")
	}
	if err := b.limits.Validate(); err != nil {
		return err
	}
	if units < 0 {
		return errors.New("negative parser work charge")
	}
	if exceeded := b.charge("work-units", units); exceeded != nil {
		copy := *exceeded
		return &copy
	}
	return nil
}

// ChargeNodes meters source records constructed by adapters outside Parse.
// Reusing an immutable record does not charge again. Exhaustion shares Parse's
// operation-wide counter, including previously constructed Markdown nodes.
func (b *OperationBudget) ChargeNodes(records int64) error {
	if b == nil {
		return errors.New("nil parser operation budget")
	}
	if err := b.limits.Validate(); err != nil {
		return err
	}
	if records < 0 {
		return errors.New("negative parser node charge")
	}
	if exceeded := b.charge("total-nodes", records); exceeded != nil {
		copy := *exceeded
		return &copy
	}
	return nil
}

// EncodingErrorsRetained reports the invalid UTF-8 records this operation has
// retained so far, across every analysis sharing the budget.
func (b *OperationBudget) EncodingErrorsRetained() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.encodingErrors
}

// RetainEncodingErrors adds newly retained invalid UTF-8 records to the count.
func (b *OperationBudget) RetainEncodingErrors(records int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.encodingErrors = addBounded(b.encodingErrors, records)
}

type budgetKey struct{}
type parserContext struct {
	context.Context
	budget      *parseBudget
	ancestors   int
	profile     parserProfile
	frontmatter FrontmatterOptions
}

func withAncestor(ctx context.Context, n int) context.Context {
	if p, ok := ctx.(*parserContext); ok {
		return &parserContext{Context: p.Context, budget: p.budget, ancestors: n, profile: p.profile, frontmatter: p.frontmatter}
	}
	return ctx
}

type parseBudget struct {
	operation     *OperationBudget
	nodes         int64
	documentLimit *LimitExceeded
}

// budgetStop is recovered only at the public parse boundary. Deep scanner and
// construction helpers can therefore stop before allocation without changing
// their ordinary syntax/model return values or swallowing unrelated panics.
type budgetStop struct{ exceeded *LimitExceeded }

func currentBudget(ctx context.Context) *parseBudget {
	if ctx == nil {
		return nil
	}
	if p, ok := ctx.(*parserContext); ok {
		return p.budget
	}
	b, _ := ctx.Value(budgetKey{}).(*parseBudget)
	return b
}
func meterWork(ctx context.Context, n int) {
	if b := currentBudget(ctx); b != nil {
		if e := b.operation.chargeLocked("work-units", int64(n)); e != nil {
			panic(budgetStop{e})
		}
	}
}

// New HTML cost expressions can exceed a host int even for admitted input.
// Keep their checked costs wide through the shared operation charge.
func meterHTMLWork(ctx context.Context, n int64) {
	if n < 0 {
		panic("parser: negative HTML work charge")
	}
	if b := currentBudget(ctx); b != nil {
		if e := b.operation.chargeLocked("work-units", n); e != nil {
			panic(budgetStop{e})
		}
	}
}

// filteredValueWork reserves the extra supplied-fact validation and deep-copy
// work. Saturation fails closed against every validated WorkUnits allowance.
// Convert lengths before arithmetic: a 32-bit MaxInt/16 source admission bound
// alone does not cover NUL expansion times eight or the indexed-search factor.
func filteredValueWork(semanticBytes, segments int64, searchBits int) int64 {
	if semanticBytes < 0 || segments < 0 || searchBits < 0 {
		return math.MaxInt64
	}
	scale := func(n, factor int64) int64 {
		if n > math.MaxInt64/factor {
			return math.MaxInt64
		}
		return n * factor
	}
	factor := addBounded(32, int64(searchBits))
	return addBounded(scale(semanticBytes, 8), scale(segments, factor))
}

func meterRecords(ctx context.Context, n int) {
	if b := currentBudget(ctx); b != nil {
		if b.documentLimit != nil {
			copy := *b.documentLimit
			panic(budgetStop{&copy})
		}
		observed := addBounded(b.nodes, int64(n))
		if observed > b.operation.limits.DocumentNodes {
			b.documentLimit = &LimitExceeded{"document-nodes", observed, b.operation.limits.DocumentNodes}
			copy := *b.documentLimit
			panic(budgetStop{&copy})
		}
		if e := b.operation.chargeLocked("total-nodes", int64(n)); e != nil {
			panic(budgetStop{e})
		}
		b.nodes = observed
	}
}
func checkDepth(ctx context.Context, depth int) {
	if b := currentBudget(ctx); b != nil && int64(depth) > b.operation.limits.SyntaxDepth {
		panic(budgetStop{&LimitExceeded{"syntax-depth", int64(depth), b.operation.limits.SyntaxDepth}})
	}
}
func ancestorDepth(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if p, ok := ctx.(*parserContext); ok {
		return p.ancestors
	}
	return 0
}
func meteredNode(ctx context.Context, spec NodeSpec) (*Node, error) {
	depth := 0
	for _, child := range spec.Children {
		meterWork(ctx, 1)
		if child != nil {
			depth = max(depth, child.depth)
		}
	}
	if spec.Kind != DocumentKind && spec.Kind != Text && spec.Kind != SoftBreak && spec.Kind != HardBreak {
		depth++
	}
	checkDepth(ctx, ancestorDepth(ctx)+depth)
	meterRecords(ctx, 1+len(spec.Markers))
	// Reserve structural validation and freezing visits, including empty values.
	meterWork(ctx, 8*(1+len(spec.Markers)+len(spec.Content)+len(spec.Children)))
	meterValue(ctx, Value{Segments: spec.Content, Semantic: spec.Properties.Literal})
	p := spec.Properties
	if spec.Kind == Strike {
		// Narrow marker spelling, payload/child containment and metadata copy.
		// Never scan the aggregate envelope across container gaps.
		meterRecords(ctx, 1)
		meterWork(ctx, 16+len(spec.Content)+len(spec.Children))
	}
	if p.Task != nil {
		// Fixed token validation, association comparison and immutable copies.
		meterRecords(ctx, 7+len(p.Task.Segments))
		meterWork(ctx, 64+16*len(p.Task.Segments))
	}
	if p.Table != nil {
		meterRecords(ctx, 1+len(p.Table.Alignments))
		meterWork(ctx, 8*(1+len(p.Table.Alignments)))
	}
	if p.Row != nil {
		meterWork(ctx, spec.Envelope.End()-spec.Envelope.Start())
		meterRecords(ctx, 1+len(p.Row.Separators))
		meterWork(ctx, 8*(1+len(p.Row.Separators)))
	}
	if p.Cell != nil {
		meterWork(ctx, spec.Envelope.End()-spec.Envelope.Start())
		meterRecords(ctx, 1+len(p.Cell.EscapedPipes))
		meterWork(ctx, 8*(1+len(p.Cell.EscapedPipes)))
	}
	if p.HTML != nil && len(p.HTML.FilteredTags) != 0 {
		meterRecords(ctx, 1+len(p.HTML.FilteredTags))
		// Bound indexed Content association, fixed prefix/boundary checks,
		// semantic comparisons and recursive immutable copies before NewNode.
		bits := 1
		for size := len(spec.Content); size > 0; size >>= 1 {
			bits++
		}
		for _, f := range p.HTML.FilteredTags {
			meterHTMLWork(ctx, addBounded(256, filteredValueWork(int64(len(f.Prefix.Semantic.Value)), int64(len(f.Prefix.Segments)), bits)))
			meterValue(ctx, f.Prefix)
			if f.Tag != nil {
				meterRecords(ctx, 1)
				meterHTMLWork(ctx, addBounded(32, filteredValueWork(int64(len(f.Tag.Semantic.Value)), int64(len(f.Tag.Segments)), bits)))
				meterValue(ctx, *f.Tag)
			}
		}
	}
	if p.Link != nil {
		if p.Link.Origin == "bare-gfm" {
			meterRecords(ctx, 1)
			meterWork(ctx, 32+len(p.Link.Label.Semantic.Value)+8*(len(spec.Content)+len(p.Link.Label.Segments)+len(p.Link.Destination.Segments)+len(spec.Children)))
		}
		meterValue(ctx, p.Link.Label)
		meterValue(ctx, p.Link.Destination)
		if p.Link.Title != nil {
			meterValue(ctx, *p.Link.Title)
		}
		if p.Link.ReferenceLabel != nil {
			meterValue(ctx, *p.Link.ReferenceLabel)
		}
	}
	if p.List != nil && p.List.Prefix != nil {
		meterValue(ctx, *p.List.Prefix)
	}
	if p.Code != nil && p.Code.Info != nil {
		meterValue(ctx, *p.Code.Info)
	}
	n, err := NewNode(spec)
	if n != nil {
		n.depth = depth
	}
	return n, err
}
func meterValue(ctx context.Context, v Value) {
	meterRecords(ctx, len(v.Segments))
	meterWork(ctx, 8*len(v.Segments)+len(v.Semantic.Value))
	for _, part := range v.Segments {
		meterWork(ctx, len(part.Semantic.Value)+part.Source.end-part.Source.start)
	}
}
func meteredReference(ctx context.Context, spec ReferenceSpec) (*ReferenceDefinition, error) {
	meterRecords(ctx, 1+len(spec.Markers))
	meterWork(ctx, 8*(1+len(spec.Markers)))
	meterValue(ctx, spec.Label)
	meterValue(ctx, spec.Destination)
	if spec.Title != nil {
		meterValue(ctx, *spec.Title)
	}
	return NewReferenceDefinition(spec)
}

// meteredAppend charges newly retained records before append can grow storage.
// Index integers and immutable node handles cost copying work, not semantic nodes.
func meteredAppend[T any](ctx context.Context, dst []T, values ...T) []T {
	meterWork(ctx, len(values))
	if len(values) > cap(dst)-len(dst) {
		meterWork(ctx, len(dst))
	}
	var zero T
	switch any(zero).(type) {
	case NodeSpec, Segment, Marker, Span, physicalLine, referencePart, openBlock, parsedBlock, inlineDelimiter, inlineBracket, emphasisPair, bareIndex, bareMatch, bareImage, HTMLFilteredTag:
		meterRecords(ctx, len(values))
	}
	return append(dst, values...)
}

func meteredSlice[T any](ctx context.Context, n int, capacity ...int) []T {
	c := n
	if len(capacity) != 0 {
		c = capacity[0]
	}
	meterWork(ctx, c)
	var zero T
	switch any(zero).(type) {
	case NodeSpec, Segment, Marker, Span, physicalLine, referencePart, openBlock, parsedBlock, inlineDelimiter, inlineBracket, emphasisPair, bareIndex, bareMatch, bareImage, HTMLFilteredTag:
		meterRecords(ctx, n)
	}
	return make([]T, n, c)
}

// scannerInterrupted bounds cancellation/deadline latency in bulk-reserved byte
// scans. Nil contexts occur only in context-free public source helpers.
func scannerInterrupted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	meterWork(ctx, 0)
	return interruption(ctx) != ""
}

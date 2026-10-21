package directives_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/directives"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func policy(t *testing.T, yaml string) *config.Configuration {
	t.Helper()
	reg, err := engine.NewRegistry(engine.Catalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := config.DecodeYAML(".mdchk.yaml", []byte("version: 1\nflavor: commonmark\n"+yaml))
	if err != nil {
		t.Fatal(err)
	}
	v, err := config.ValidateDefinition(d, reg, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	return v.Configuration(config.Capabilities{})
}
func process(t *testing.T, text string, c *config.Configuration) directives.Result {
	t.Helper()
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	parsed := parser.ParseWithBudget(context.Background(), parser.NewSource([]byte(text)), budget)
	r := directives.Process(context.Background(), parsed, c, budget)
	if !r.Complete() {
		t.Fatalf("incomplete: %v %+v", r.Err(), r.Notes())
	}
	return r
}
func enabled(t *testing.T, r directives.Result, id string, at int, want bool, owner int) {
	t.Helper()
	got, err := r.StateAt(id, at)
	if err != nil || got.Enabled != want || got.Owner != owner {
		t.Fatalf("%s at %d got %+v %v want %t owner %d", id, at, got, err, want, owner)
	}
}
func TestIntervalsAndLastInstructionOwnership(t *testing.T) {
	const text = "x<!-- mdchk:disable MD001 MD040 MD001 -->A<!-- mdchk:disable MD001 -->B<!-- mdchk:enable MD001 -->C"
	r := process(t, text, policy(t, ""))
	cs := r.Candidates()
	if len(cs) != 3 || !reflect.DeepEqual(cs[0].IDs, []string{"MD001", "MD040"}) || len(cs[0].Named) != 3 || cs[0].Closing.End() != 41 {
		t.Fatalf("candidates %+v", cs)
	}
	for _, tt := range []struct {
		at    int
		on    bool
		owner int
	}{{0, true, -1}, {40, true, -1}, {41, false, 0}, {69, false, 0}, {70, false, 1}, {97, false, 1}, {98, true, 2}, {99, true, 2}} {
		enabled(t, r, "MD001", tt.at, tt.on, tt.owner)
	}
	enabled(t, r, "MD040", len(text), false, 0)
	if len(r.Intervals("MD001")) != 4 {
		t.Fatal(r.Intervals("MD001"))
	}
	if text[cs[0].Effects[0].Token.Start():cs[0].Effects[0].Token.End()] != "MD001" {
		t.Fatal("wrong first token")
	}
}
func TestAtomicGrammarAndKnownUnavailableCatalog(t *testing.T) {
	c := policy(t, "")
	for _, bad := range []string{"mdchk:disable", "mdchk:Disable MD001", "MDCHK:disable MD001", "mdchk:wat MD001", "mdchk disable MD001", "mdchk:disable MD001 MD999", "mdchk:disable MD001 MD008", "mdchk:disable md001", "mdchk:disable heading-increment", "mdchk:disable MD001,MD040", "mdchk:disable *", "mdchk:disable all"} {
		t.Run(bad, func(t *testing.T) {
			r := process(t, "<!-- "+bad+" -->\ntail", c)
			if len(r.Diagnostics()) != 1 || r.Diagnostics()[0].Rule != "directive" || len(r.Candidates()) != 1 || r.Candidates()[0].Disposition != directives.Invalid {
				t.Fatalf("not exact invalid: %+v %+v", r.Candidates(), r.Diagnostics())
			}
			enabled(t, r, "MD001", r.Source().Len(), true, -1)
		})
	}
	valid := process(t, "<!-- mdchk:enable MD013 MD087 -->", c)
	if len(valid.Diagnostics()) != 0 || len(valid.Candidates()[0].Effects) != 2 {
		t.Fatal(valid.Candidates())
	}
	enabled(t, valid, "MD013", valid.Source().Len(), true, 0)
	enabled(t, valid, "MD087", valid.Source().Len(), true, 0)
	// The otherwise-identical valid control changes state; an invalid-ID check
	// cannot pass merely because all directives or a capability are disabled.
	control := process(t, "<!-- mdchk:disable MD001 MD040 -->", c)
	enabled(t, control, "MD001", control.Source().Len(), false, 0)
}
func TestMappedSpellingEndingsAndEOF(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		text := "> <!-- mdchk:disable MD001" + ending + ">   MD040 -->tail" + ending
		r := process(t, text, policy(t, ""))
		cs := r.Candidates()
		if len(cs) != 1 || cs[0].Raw != "<!-- mdchk:disable MD001"+ending+">   MD040 -->" || cs[0].Payload != " mdchk:disable MD001"+ending+"  MD040 " {
			t.Fatalf("mapped comment %+v", cs)
		}
		closeEnd := len(text) - len("tail") - len(ending)
		enabled(t, r, "MD001", closeEnd-1, true, -1)
		enabled(t, r, "MD001", closeEnd, false, 0)
		tok := cs[0].Effects[1].Token
		raw, _ := r.Source().Text(tok)
		if raw != "MD040" {
			t.Fatal(raw)
		}
		pos, _ := parser.NewLineIndex(r.Source()).Position(tok.Start())
		if pos != (parser.Position{Line: 2, Column: 5}) {
			t.Fatal(pos)
		}
	}
	r := process(t, "é<!--\n mdchk:disable\tMD001\u00a0MD040 -->", policy(t, ""))
	if len(r.Diagnostics()) != 0 || len(r.Candidates()) != 1 {
		t.Fatal(r.Candidates(), r.Diagnostics())
	}
	enabled(t, r, "MD001", r.Source().Len(), false, 0)
	eof := process(t, "<!-- mdchk:disable MD001 -->", policy(t, ""))
	intervals := eof.Intervals("MD001")
	if intervals[1].Start != 28 || intervals[1].End != 28 {
		t.Fatal(intervals)
	}
	enabled(t, eof, "MD001", 28, false, 0)
}
func TestPermissionStatesAndSpecialEffects(t *testing.T) {
	c := policy(t, "rules:\n  MD001: {enabled: true, allow_directives: false}\n  MD013: {enabled: false, length: 101}\n  MD040: {enabled: false, allow_directives: false}\n")
	r := process(t, "<!-- mdchk:disable MD001 MD040 MD074 MD094 -->\n<!-- mdchk:enable MD013 -->", c)
	enabled(t, r, "MD001", r.Source().Len(), true, -1)
	enabled(t, r, "MD040", r.Source().Len(), false, -1)
	enabled(t, r, "MD074", r.Source().Len(), false, -1)
	enabled(t, r, "MD094", r.Source().Len(), true, -1)
	enabled(t, r, "MD013", r.Source().Len(), true, 1)
	if len(r.Notes()) != 4 || len(r.Diagnostics()) != 0 || r.Candidates()[0].Effects[2].Disposition != directives.Inapplicable {
		t.Fatal(r.Notes(), r.Candidates())
	}
	state, _ := r.StateAt("MD013", r.Source().Len())
	if state.Options["length"] != 101 {
		t.Fatal(state)
	}
	disabled := process(t, "<!-- mdchk:wat MD999 -->", policy(t, "directives: {enabled: false}\n"))
	if len(disabled.Candidates()) != 1 || len(disabled.Diagnostics()) != 0 || disabled.Candidates()[0].Disposition != directives.Inactive {
		t.Fatal(disabled)
	}
	mixed := process(t, "<!-- mdchk:disable MD001 MD074 MD094 -->", policy(t, ""))
	enabled(t, mixed, "MD001", mixed.Source().Len(), false, 0)
}
func TestLiteralAndUnclosedComments(t *testing.T) {
	c := policy(t, "")
	for _, text := range []string{"```\n<!-- mdchk:disable MD001 -->\n```", "    <!-- mdchk:disable MD001 -->", "x `<!-- mdchk:disable MD001 -->`", "x \\<!-- mdchk:disable MD001 -->", "x &lt;!-- mdchk:disable MD001 -->", "x <a title='<!-- mdchk:disable MD001 -->'>z</a>", "<script>\n<!-- mdchk:disable MD001 -->\n</script>", "<pre>\n<!-- mdchk:disable MD001 -->\n</pre>", "<style>\n<!-- mdchk:disable MD001 -->\n</style>", "<textarea>\n<!-- mdchk:disable MD001 -->\n</textarea>", "<!-->", "<!--->"} {
		t.Run(text, func(t *testing.T) {
			r := process(t, text, c)
			if len(r.Candidates()) != 0 {
				t.Fatal(r.Candidates())
			}
			enabled(t, r, "MD001", len(text), true, -1)
		})
	}
	inline := process(t, "x <script><!-- mdchk:disable MD001 --></script><!-- mdchk:disable MD040 -->", c)
	if len(inline.Candidates()) != 2 || inline.Candidates()[0].Disposition != directives.Uncertain {
		t.Fatal(inline.Candidates())
	}
	enabled(t, inline, "MD001", inline.Source().Len(), true, -1)
	enabled(t, inline, "MD040", inline.Source().Len(), false, 1)
	open := process(t, "<!-- mdchk:disable MD001", c)
	if len(open.Candidates()) != 1 || open.Candidates()[0].Disposition != directives.Uncertain || len(open.Notes()) != 1 {
		t.Fatal(open.Candidates(), open.Notes())
	}
	enabled(t, open, "MD001", open.Source().Len(), true, -1)
	nested := process(t, "<!-- mdchk:disable MD001 <!-- mdchk:enable MD001 -->", c)
	if len(nested.Diagnostics()) != 1 {
		t.Fatal(nested.Candidates())
	}
}
func TestImmutableAuthorityAndReturnedRecords(t *testing.T) {
	c := policy(t, "rules: {MD013: {enabled: false, length: 101, allow_directives: false}}\n")
	c.Settings.DirectivesEnabled = false
	c.Rules["MD001"] = engine.ResolvedRule{Enabled: false}
	c.Rules["MD013"].Options["length"] = 999
	c.EffectivePermissions.RuleDirectives = nil
	r := process(t, "<!-- mdchk:disable MD001 -->\n<!-- mdchk:enable MD013 -->", c)
	enabled(t, r, "MD001", r.Source().Len(), false, 0)
	enabled(t, r, "MD013", r.Source().Len(), false, -1)
	state, _ := r.StateAt("MD013", r.Source().Len())
	if state.Options["length"] != 101 {
		t.Fatal(state)
	}
	state.Options["length"] = 1
	cs := r.Candidates()
	cs[0].IDs[0] = "MD999"
	cs[0].Effects[0].Rule = "MD999"
	cs[1].Effects[0].Restrictions[0].Position.Line = 999
	r.Intervals("MD001")[1].Enabled = true
	r.Notes()[0].Restrictions[0].Position.Line = 999
	again := r.Candidates()
	if again[0].IDs[0] != "MD001" || again[1].Effects[0].Restrictions[0].Position.Line == 999 {
		t.Fatal(again)
	}
	empty := process(t, "", c)
	enabled(t, empty, "MD001", 0, true, -1)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				s, e := r.StateAt("MD013", r.Source().Len())
				if e != nil || s.Options["length"] != 101 {
					t.Error(s, e)
				}
				_ = r.Candidates()
			}
		})
	}
	wg.Wait()
}
func TestUnresolvedAndPartialProofs(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte("<!-- mdchk:disable MD001 -->"))
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	c := policy(t, "")
	bad := []parser.ParseResult{parsed, parsed, parsed}
	bad[0].Source = parser.NewSource(source.Bytes())
	bad[1].AST = parser.Partial
	bad[2].Uncertainty = []parser.Uncertainty{{Region: mustSpan(t, source, 0, source.Len()), Reason: parser.SyntaxUnimplemented}}
	root := parsed.Document.Root()
	constructed, err := parser.NewDocument(root, parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	forged := parsed
	forged.Document = constructed
	bad = append(bad, forged)
	for _, p := range bad {
		r := directives.Process(context.Background(), p, c, budget)
		if r.Complete() || len(r.Intervals("MD001")) != 0 || len(r.Notes()) == 0 {
			t.Fatal(r)
		}
	}
	for _, view := range []*config.Configuration{nil, {}} {
		r := directives.Process(context.Background(), parsed, view, budget)
		if r.Complete() || r.Notes()[0].Code != "directive-policy-unresolved" {
			t.Fatal(r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := directives.Process(ctx, parsed, c, budget)
	if !errors.Is(r.Err(), context.Canceled) || r.Complete() {
		t.Fatal(r.Err())
	}
}
func mustSpan(t *testing.T, s parser.Source, a, b int) parser.Span {
	t.Helper()
	v, e := s.Span(a, b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestSharedBudgetNoResetAndFiniteStorm(t *testing.T) {
	c := policy(t, "")
	source := parser.NewSource([]byte(strings.Repeat("x<!-- mdchk:disable MD001 MD001 -->", 200)))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	// Exhaust the existing operation after parsing; processing must not replace it.
	if err := budget.ChargeWork(parser.DefaultLimits().WorkUnits); err == nil {
		t.Fatal("expected cumulative exhaustion")
	}
	r := directives.Process(context.Background(), parsed, c, budget)
	var limit *parser.LimitExceeded
	if r.Complete() || !errors.As(r.Err(), &limit) || len(r.Intervals("MD001")) != 0 {
		t.Fatal(r.Err())
	}
	good := process(t, string(source.Bytes()), c)
	if len(good.Candidates()) != 200 || len(good.Intervals("MD001")) != 201 {
		t.Fatal("storm lost ownership")
	}
	// Enough budget for collection but not validation/intervals. No partial state
	// may be published as a successful all-enabled result.

	// Observe parse work with a separate exhausted measurement operation, then
	// allocate only a small residual allowance to the actual same-operation
	// parse/processor pair. No fresh budget can be substituted after parsing.
	parseWork := limit.Observed - parser.DefaultLimits().WorkUnits
	for _, allow := range []int64{1, 100, 1000, 10000} {
		small := parser.DefaultLimits()
		small.WorkUnits = parseWork + allow
		b, _ := parser.NewOperationBudget(small)
		p := parser.ParseWithBudget(context.Background(), source, b)
		if p.Status != parser.Complete {
			t.Fatalf("residual parse incomplete %d %+v", allow, p)
		}
		partial := directives.Process(context.Background(), p, c, b)
		if partial.Complete() || len(partial.Intervals("MD001")) != 0 {
			t.Fatal("partial effects escaped", allow)
		}
	}
	replacement, _ := parser.NewOperationBudget(parser.DefaultLimits())
	reset := directives.Process(context.Background(), parsed, c, replacement)
	if reset.Complete() || len(reset.Intervals("MD001")) != 0 {
		t.Fatal("replacement budget accepted")
	}
}

func TestInheritedDenialsRemainImmutable(t *testing.T) {
	reg, err := engine.NewRegistry(engine.Catalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	state := config.PolicyState{}
	for _, name := range []string{"ancestor", "middle"} {
		d, e := config.DecodeYAML(name+"/.mdchk.yaml", []byte("version: 1\ndirectives: {enabled: false}\nrules: {MD001: {allow_directives: false}}\n"))
		if e != nil {
			t.Fatal(e)
		}
		state, _, e = state.Apply(d, reg, engine.ValidationProviders{}, config.Capabilities{})
		if e != nil {
			t.Fatal(e)
		}
	}
	d, err := config.DecodeYAML("leaf/.mdchk.yaml", []byte("version: 1\ndirectives: {enabled: true}\nrules: {MD001: {allow_directives: true}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, view, err := state.Apply(d, reg, engine.ValidationProviders{}, config.Capabilities{})
	if err != nil {
		t.Fatal(err)
	}
	decision := view.DirectivePermission("MD001")
	if len(decision.Restrictions) != 4 {
		t.Fatal(decision)
	}
	view.Settings.DirectivesEnabled = true
	view.EffectivePermissions.Directives = nil
	view.OwnPermissions.Directives = nil
	r := process(t, "<!-- mdchk:wat MD999 -->\n<!-- mdchk:disable MD001 -->", view)
	if len(r.Candidates()) != 2 || len(r.Diagnostics()) != 0 {
		t.Fatal(r.Candidates(), r.Diagnostics())
	}
	enabled(t, r, "MD001", r.Source().Len(), true, -1)
}

func TestAdjacentCommentsAndListMapping(t *testing.T) {
	r := process(t, "x<!-- mdchk:disable MD001 --><!-- mdchk:enable MD001 -->", policy(t, ""))
	enabled(t, r, "MD001", 29, false, 0)
	enabled(t, r, "MD001", 56, true, 1)
	mapped := process(t, "- <!-- mdchk:disable MD001\n  MD040 -->tail", policy(t, ""))
	cs := mapped.Candidates()
	if len(cs) != 1 || len(cs[0].Effects) != 2 {
		t.Fatal(cs)
	}
	if cs[0].Payload != " mdchk:disable MD001\nMD040 " {
		t.Fatal(cs[0].Payload)
	}
	token := cs[0].Effects[1].Token
	pos, _ := parser.NewLineIndex(mapped.Source()).Position(token.Start())
	if pos != (parser.Position{Line: 2, Column: 3}) {
		t.Fatal(pos)
	}
}

func FuzzDirectiveIntervals(f *testing.F) {
	for _, s := range []string{"x<!-- mdchk:disable MD001 -->tail", "<!-- mdchk:enable MD013 -->", "> <!-- mdchk:disable MD001\r\n> MD040 -->", "<!-- mdchk:disable MD001 MD999 -->", "x<script><!-- mdchk:disable MD001 --></script>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 16384 {
			t.Skip()
		}
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		source := parser.NewSource([]byte(text))
		parsed := parser.ParseWithBudget(context.Background(), source, budget)
		r := directives.Process(context.Background(), parsed, policy(t, ""), budget)
		if !r.Complete() {
			if len(r.Intervals("MD001")) != 0 {
				t.Fatal("incomplete state")
			}
			return
		}
		intervals := r.Intervals("MD001")
		cs := r.Candidates()
		for i, v := range intervals {
			if v.Start < 0 || v.End < v.Start || v.End > len(text) || i > 0 && intervals[i-1].End != v.Start {
				t.Fatal(intervals)
			}
			if v.Owner >= 0 {
				if v.Owner >= len(cs) || cs[v.Owner].Disposition != directives.Applied || cs[v.Owner].Closing.End() != v.Start {
					t.Fatal(v, cs)
				}
			}
		}
		if len(intervals) == 0 || intervals[0].Start != 0 || intervals[len(intervals)-1].End != len(text) {
			t.Fatal(intervals)
		}
	})
}

func TestBlockTailCommentsKeepEveryInstruction(t *testing.T) {
	c := policy(t, "")
	for _, tt := range []struct {
		text         string
		on           bool
		owner, count int
	}{
		{"<!-- mdchk:disable MD001 --><!-- mdchk:enable MD001 -->", true, 1, 2},
		{"<!-- ordinary --><!-- mdchk:disable MD001 -->", false, 0, 1},
		{"<!-- mdchk:disable MD001 -->tail<!-- mdchk:disable MD001 -->", false, 1, 2},
		{"<!-- mdchk:disable MD001 --><!-- mdchk:disable MD001 MD999 --><!-- mdchk:enable MD001 -->", true, 2, 3},
		{"> <!-- ordinary -->tail<!-- mdchk:disable MD001 --><!-- mdchk:enable MD001 -->", true, 1, 2},
		{"- <!-- ordinary -->tail<!-- mdchk:disable MD001 --><!-- mdchk:enable MD001 -->", true, 1, 2},
		{"<!-- ordinary --><a title='<!-- mdchk:disable MD001 -->'>text</a>", true, -1, 0},
		{"<!-- ordinary --><script><!-- mdchk:disable MD001 --></script><!-- mdchk:disable MD040 -->", true, -1, 2},
	} {
		t.Run(tt.text, func(t *testing.T) {
			r := process(t, tt.text, c)
			cs := r.Candidates()
			if len(cs) != tt.count {
				t.Fatalf("%d candidates want%d: %+v", len(cs), tt.count, cs)
			}
			enabled(t, r, "MD001", len(tt.text), tt.on, tt.owner)
			for _, candidate := range cs {
				raw, _ := r.Source().Text(candidate.Span)
				if raw != candidate.Raw || candidate.Closing.End() != candidate.Span.End() {
					t.Fatal(candidate)
				}
			}
			if tt.owner >= 0 {
				at := cs[tt.owner].Closing.End()
				enabled(t, r, "MD001", at, tt.on, tt.owner)
			}
		})
	}
}

func TestRawHTMLClosersAcrossBlockAndInlineNodes(t *testing.T) {
	for _, tag := range []string{"script", "pre", "style", "textarea"} {
		t.Run(tag, func(t *testing.T) {
			c := policy(t, "")
			text := "x <" + tag + ">\n\n</" + tag + ">\n\n<!-- mdchk:disable MD001 -->"
			r := process(t, text, c)
			cs := r.Candidates()
			if len(cs) != 1 || cs[0].Disposition != directives.Applied {
				t.Fatal(cs)
			}
			enabled(t, r, "MD001", len(text)-1, true, -1)
			enabled(t, r, "MD001", len(text), false, 0)
			inside := process(t, "x <"+tag+">\n\n<!-- mdchk:disable MD001 -->\n\n</"+strings.ToUpper(tag)+">\n\n<!-- mdchk:disable MD040 -->", c)
			if len(inside.Candidates()) != 2 || inside.Candidates()[0].Disposition != directives.Uncertain || inside.Candidates()[1].Disposition != directives.Applied {
				t.Fatal(inside.Candidates())
			}
			enabled(t, inside, "MD001", inside.Source().Len(), true, -1)
			enabled(t, inside, "MD040", inside.Source().Len(), false, 1)
		})
	}
}

func TestPermissionProvenanceUsesResidualOperationBudget(t *testing.T) {
	reg, err := engine.NewRegistry(engine.Catalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte(strings.Repeat("x<!-- mdchk:disable MD001 -->\n", 100)))
	for _, global := range []bool{true, false} {
		kind := "rule"
		denial := "rules: {MD001: {allow_directives: false}}\n"
		if global {
			kind = "global"
			denial = "directives: {enabled: false}\n"
		}
		t.Run(kind, func(t *testing.T) {
			var small, large *config.Configuration
			state := config.PolicyState{}
			d, e := config.DecodeYAML(strings.Repeat("long-parent/", 10)+".mdchk.yaml", []byte("version: 1\nflavor: commonmark\n"+denial))
			if e != nil {
				t.Fatal(e)
			}
			for i := range 1280 {
				state, large, e = state.Apply(d, reg, engine.ValidationProviders{}, config.Capabilities{})
				if e != nil {
					t.Fatal(e)
				}
				if i == 0 {
					small = large
				}
			}
			// Measure the complete small-policy operation, then allow exactly that work
			// for the same source and many denials. Large policy must exhaust before an
			// uncharged origin expansion can be retained as a successful result.
			measure, _ := parser.NewOperationBudget(parser.DefaultLimits())
			p := parser.ParseWithBudget(context.Background(), source, measure)
			first := directives.Process(context.Background(), p, small, measure)
			if !first.Complete() {
				t.Fatal(first.Err())
			}
			var observed *parser.LimitExceeded
			if !errors.As(measure.ChargeWork(parser.DefaultLimits().WorkUnits), &observed) {
				t.Fatal("measurement missing")
			}
			consumed := observed.Observed - parser.DefaultLimits().WorkUnits
			limits := parser.DefaultLimits()
			limits.WorkUnits = consumed
			b, _ := parser.NewOperationBudget(limits)
			p = parser.ParseWithBudget(context.Background(), source, b)
			if p.Status != parser.Complete {
				t.Fatal(p)
			}
			partial := directives.Process(context.Background(), p, large, b)
			var exhausted *parser.LimitExceeded
			if partial.Complete() || !errors.As(partial.Err(), &exhausted) || len(partial.Intervals("MD001")) != 0 {
				t.Fatal("large provenance bypassed work budget", partial.Err())
			}
			if len(partial.Candidates()) >= 100 {
				t.Fatal("all candidate origins allocated before exhaustion")
			}
			full := process(t, string(source.Bytes()), large)
			notes := full.Notes()
			if len(notes) != 100 || len(notes[0].Restrictions) != 1280 {
				t.Fatalf("lost retained authority %d %+v", len(notes), notes[0])
			}
			notes[0].Restrictions[0].Position.Line = 999
			if full.Notes()[0].Restrictions[0].Position.Line == 999 {
				t.Fatal("origin alias escaped")
			}
			requested := "MD001"
			if global {
				requested = ""
			}
			few, _ := small.DirectivePermissionWork(requested)
			many, _ := large.DirectivePermissionWork(requested)
			if many <= few*1000 {
				t.Fatal("estimate lost provenance count/size", few, many)
			}
		})
	}
}

func TestUnclosedBlockTailRemainsInspectable(t *testing.T) {
	r := process(t, "<!-- ordinary -->tail<!-- mdchk:disable MD001", policy(t, ""))
	cs := r.Candidates()
	if len(cs) != 1 || cs[0].Disposition != directives.Uncertain || cs[0].Closing.Present() {
		t.Fatal(cs)
	}
	enabled(t, r, "MD001", r.Source().Len(), true, -1)
}

func TestTerminatedHTMLBlockTails(t *testing.T) {
	c := policy(t, "")
	for _, tag := range []string{"script", "pre", "style", "textarea"} {
		t.Run(tag, func(t *testing.T) {
			for _, container := range []struct{ name, first, continuation string }{
				{"plain", "", ""}, {"quote", "> ", "> "}, {"list", "- ", "  "},
			} {
				t.Run(container.name, func(t *testing.T) {
					// The interior comment is literal; only the comment after the raw close is
					// eligible. Marker anchors come from these independent original byte strings.
					prefix := container.first + "<" + tag + ">\n" + container.continuation + "<!-- mdchk:disable MD003 -->\n" + container.continuation + "</" + strings.ToUpper(tag) + ">"
					command := "<!-- mdchk:disable MD001 -->"
					text := prefix + command + "tail"
					r := process(t, text, c)
					cs := r.Candidates()
					if len(cs) != 1 || cs[0].Raw != command || cs[0].Disposition != directives.Applied || cs[0].Span.Start() != len(prefix) || cs[0].Closing.End() != len(prefix)+len(command) {
						t.Fatal(cs)
					}
					enabled(t, r, "MD001", len(prefix)+len(command)-1, true, -1)
					enabled(t, r, "MD001", len(prefix)+len(command), false, 0)
					enabled(t, r, "MD003", len(text), true, -1)
					// Repeated and adjacent tail instructions each retain ownership; quoted
					// attribute lookalikes cannot introduce another request.
					tail := "<a title='<!-- mdchk:enable MD001 -->'>x</a>" + command + "<!-- mdchk:enable MD001 -->"
					repeated := process(t, prefix+command+tail, c)
					if len(repeated.Candidates()) != 3 {
						t.Fatal(repeated.Candidates())
					}
					cs = repeated.Candidates()
					enabled(t, repeated, "MD001", cs[1].Closing.End(), false, 1)
					enabled(t, repeated, "MD001", cs[2].Closing.End(), true, 2)
				})
			}
		})
	}
	// Forms 3/4/5 have the same physical closing-line envelope but different
	// opaque terminators. Their literal payload is not directive Markdown.
	for _, tt := range []struct{ prefix string }{
		{"<?processing <!-- mdchk:disable MD003 -->?>"},
		{"<!X opaque>"},
		{"<![CDATA[<!-- mdchk:disable MD003 -->]]>"},
	} {
		t.Run(tt.prefix, func(t *testing.T) {
			command := "<!-- mdchk:disable MD001 -->"
			text := tt.prefix + command
			r := process(t, text, c)
			cs := r.Candidates()
			if len(cs) != 1 || cs[0].Raw != command || cs[0].Span.Start() != len(tt.prefix) || cs[0].Closing.End() != len(text) {
				t.Fatal(cs)
			}
			enabled(t, r, "MD001", len(text)-1, true, -1)
			enabled(t, r, "MD001", len(text), false, 0)
			enabled(t, r, "MD003", len(text), true, -1)
		})
	}
}

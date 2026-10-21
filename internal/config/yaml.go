package config

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	source "github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

const configBytes = 1048576
const structuredDepth = 64

// YAML 1.2.2 section 8.1.1: one indentation and one chomping indicator,
// in either order. Validate the maintained lexer's header token spelling.
var blockHeader = regexp.MustCompile(`^[|>](?:[1-9][+-]?|[+-][1-9]?)?$`)

var decimalInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// YAML 1.2.2 section 10.3.2 core scalar resolution. The pinned parser misses
// exponent-only floats and decimal integers outside uint64/int64 (and 08/09).
// Resolve these plain scalars without changing quoted or explicitly tagged
// strings. General YAML syntax remains entirely owned by the dependency.
var coreInteger = regexp.MustCompile(`^[-+]?[0-9]+$`)
var coreNumber = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
var coreSpecialNumber = regexp.MustCompile(`^([-+]?\.(inf|Inf|INF)|\.(nan|NaN|NAN))$`)

// DecodeYAML validates syntax and the fixed version-1 structure, retaining only
// supplied settings. Rule IDs/options/domains and build capabilities still need
// semantic validation. sourcePath is diagnostic identity only; no file is read.
// The accepted default config-byte, structured-depth and document-node limits
// bound source parsing and alias expansion. No defaults or partial results escape.
func DecodeYAML(sourcePath string, content []byte) (*Definition, error) {
	origin := Origin{Path: sourcePath}
	if len(content) > configBytes {
		return nil, &DecodeError{Message: "config-bytes limit exceeded", Origin: origin}
	}
	if !utf8.Valid(content) {
		return nil, &DecodeError{Message: "configuration must be UTF-8", Origin: origin}
	}
	text := string(content)
	tokens := lexer.Tokenize(text)
	// Preflight the library's tokens before its recursive syntax construction.
	// Indentation levels and flow indicators come from the YAML lexer, not an
	// independent YAML grammar. The complete AST walk checks actual containers.
	flow := 0
	for _, t := range tokens {
		if (t.Type == token.LiteralType || t.Type == token.FoldedType) && !blockHeader.MatchString(t.Value) {
			return nil, &DecodeError{Message: "invalid YAML block scalar header", Origin: tokenOrigin(sourcePath, "", t)}
		}
		if t.Type == token.MappingStartType || t.Type == token.SequenceStartType {
			flow++
		} else if t.Type == token.MappingEndType || t.Type == token.SequenceEndType {
			flow--
		}
		if flow > structuredDepth || t.Position.IndentLevel > structuredDepth {
			return nil, &DecodeError{Message: "structured-input-depth limit exceeded", Origin: tokenOrigin(sourcePath, "", t)}
		}
	}
	if len(tokens) > 250000 {
		return nil, &DecodeError{Message: "document-nodes limit exceeded by YAML tokens", Origin: origin}
	}
	tokens = normalizeEmptyBlocks(tokens)
	file, err := yamlparser.Parse(tokens, 0, yamlparser.AllowDuplicateMapKey())
	if err != nil {
		return nil, &DecodeError{Message: "invalid YAML: " + err.Error(), Origin: origin}
	}
	if len(file.Docs) != 1 {
		return nil, &DecodeError{Message: "expected exactly one nonempty YAML document", Origin: origin}
	}
	if file.Docs[0].Body == nil {
		return nil, &DecodeError{Message: "expected a nonempty mapping document", Origin: origin}
	}
	b := yamlBuilder{path: sourcePath, content: text, anchors: map[string]ast.Node{}, bindings: map[*ast.AliasNode]ast.Node{}, active: map[ast.Node]bool{}, blocks: map[*ast.LiteralNode]blockScalar{}, conversions: map[ast.Node]*scalarConversion{}, tags: map[*ast.TagNode]bool{}, work: int64(len(text) + 2*len(tokens))}
	b.lineStarts = yamlLineStarts(text)
	root, err := b.value(file.Docs[0].Body, "", 0, 0, nil)
	if err != nil {
		return nil, err
	}
	return definition(root)
}

// blockScalar shares immutable source/semantic storage across alias uses. Each
// LocatedValue still owns its use-site Origin; no body-sized copy is expanded.
type blockScalar struct{ spelling, text string }

type yamlBuilder struct {
	conversions map[ast.Node]*scalarConversion
	tags        map[*ast.TagNode]bool
	content     string
	lineStarts  []int
	bindings    map[*ast.AliasNode]ast.Node
	blocks      map[*ast.LiteralNode]blockScalar
	path        string
	anchors     map[string]ast.Node
	active      map[ast.Node]bool
	nodes       int
	work        int64
}

func tokenOrigin(path, property string, t *token.Token) Origin {
	o := Origin{Path: path, PropertyPath: property}
	if t != nil && t.Position != nil {
		o.Position = &source.Position{Line: t.Position.Line, Column: t.Position.Column}
	}
	return o
}
func childPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
func (b *yamlBuilder) value(n ast.Node, path string, depth, aliases int, use *token.Token) (LocatedValue, error) {
	if n == nil {
		return LocatedValue{}, &DecodeError{Message: "null settings are not permitted", Origin: Origin{Path: b.path, PropertyPath: path}}
	}
	t := n.GetToken()
	if use != nil {
		t = use
	}
	origin := tokenOrigin(b.path, path, t)
	fail := func(message string) (LocatedValue, error) {
		return LocatedValue{}, &DecodeError{Message: message, Origin: origin}
	}
	b.work += int64(len(path) + len(n.GetToken().Value) + 1)
	if b.work > 250000000 {
		return fail("work-units limit exceeded during YAML/alias validation")
	}
	b.nodes++
	if b.nodes > 250000 {
		return fail("document-nodes limit exceeded during YAML/alias validation")
	}
	if aliases > 256 {
		return fail("syntax-depth limit exceeded during alias traversal")
	}
	if b.active[n] {
		return fail("cyclic alias is not permitted")
	}
	b.active[n] = true
	defer delete(b.active, n)
	v := LocatedValue{node: &syntaxValue{origin: origin}}
	v.node.spelling = strings.TrimSpace(n.GetToken().Origin)
	switch n := n.(type) {
	case *ast.MappingKeyNode:
		return b.value(n.Value, path, depth, aliases, use)
	case *ast.AnchorNode:
		// Anchors are registered in source order. Alias expansion must not rebind
		// names to anchors from a prior subtree.
		if use == nil {
			b.anchors[n.Name.String()] = n.Value
		}
		return b.value(n.Value, path, depth, aliases, use)
	case *ast.AliasNode:
		target, ok := b.bindings[n]
		if !ok {
			// YAML aliases bind to the most recent anchor at their original
			// source occurrence, not when an enclosing collection is reused.
			target, ok = b.anchors[n.Value.String()]
			if !ok {
				return fail("unresolved alias " + n.Value.String())
			}
			b.bindings[n] = target
		}
		if use == nil {
			use = n.GetToken()
		}
		return b.value(target, path, depth, aliases+1, use)
	case *ast.TagNode:
		tag := strings.TrimPrefix(n.Start.Value, "!!")
		tag = strings.TrimPrefix(strings.TrimSuffix(tag, ">"), "!<tag:yaml.org,2002:")
		kinds := map[string]ValueKind{"str": StringValue, "int": IntegerValue, "float": NumberValue, "bool": BooleanValue, "map": MappingValue, "seq": SequenceValue}
		expected, ok := kinds[tag]
		if !ok {
			return fail("custom or unsupported YAML tag " + n.Start.Value)
		}
		if expected == MappingValue || expected == SequenceValue {
			child, err := b.value(n.Value, path, depth, aliases, use)
			if err != nil {
				return LocatedValue{}, err
			}
			if child.Kind() != expected {
				return fail("YAML core collection tag has the wrong value kind")
			}
			child.node.origin = origin
			return child, nil
		}
		// An explicit core tag determines the scalar's kind (YAML 1.2.2
		// section 6.9.1). Its quoting is presentation, not implicit resolution.
		// Validate the lexical content instead of using decoder coercion.
		text, ok := scalarText(n.Value)
		if !ok {
			return fail("YAML core scalar tag requires a scalar")
		}
		v.node.kind = expected
		v.node.text = text
		v.node.spelling = strings.TrimSpace(n.Value.GetToken().Origin)
		if block, ok := n.Value.(*ast.LiteralNode); ok {
			snapshot, err := b.block(block)
			if err != nil {
				return fail(err.Error())
			}
			v.node.spelling = snapshot.spelling
			v.node.text = snapshot.text
		}
		if b.tags[n] {
			return b.finishScalar(n, v)
		}
		// Successful immutable tag validation is cached, so repeated aliases
		// do not rescan long numeric block contents. Charge the first lexical
		// inspection before running the type-specific validators.
		b.work += int64(len(text))
		if b.work > 250000000 {
			return fail("work-units limit exceeded during YAML/alias validation")
		}
		switch expected {
		case StringValue, MappingValue, SequenceValue:
		// Strings need no lexical restriction; collections returned above.
		case IntegerValue:
			if !decimalInteger.MatchString(text) {
				return fail("integer must use decimal notation without leading zeros")
			}
		case BooleanValue:
			if text != "true" && text != "false" {
				return fail("boolean must be spelled true or false")
			}
		case NumberValue:
			if !coreNumber.MatchString(text) && !coreSpecialNumber.MatchString(text) {
				return fail("invalid floating-point scalar")
			}
		}
		b.tags[n] = true
	case *ast.MappingNode:
		depth++
		if depth > structuredDepth {
			return fail("structured-input-depth limit exceeded")
		}
		v.node.kind = MappingValue
		v.node.spelling = ""
		v.node.entries = make([]LocatedEntry, 0, len(n.Values))
		seen := map[string]Origin{}
		for _, e := range n.Values {
			key, err := b.value(e.Key, path, depth, aliases, use)
			if err != nil {
				return LocatedValue{}, err
			}
			if key.Kind() != StringValue {
				return LocatedValue{}, &DecodeError{Message: "mapping keys must be strings; merge keys are not permitted", Origin: key.Origin()}
			}
			name := key.StringValue()
			b.work += int64(len(path) + len(name) + 1)
			if b.work > 250000000 {
				return fail("work-units limit exceeded during YAML/alias validation")
			}
			kp := childPath(path, name)
			ko := key.Origin()
			ko.PropertyPath = kp
			if name == "<<" {
				return LocatedValue{}, &DecodeError{Message: "merge keys are not permitted", Origin: ko}
			}
			if previous, ok := seen[name]; ok {
				return LocatedValue{}, &DecodeError{Message: "duplicate key " + strconv.Quote(name), Origin: ko, Previous: &previous}
			}
			seen[name] = ko
			child, err := b.value(e.Value, kp, depth, aliases, use)
			if err != nil {
				return LocatedValue{}, err
			}
			v.node.entries = append(v.node.entries, LocatedEntry{Key: name, Origin: ko, Value: child})
		}
	case *ast.SequenceNode:
		depth++
		if depth > structuredDepth {
			return fail("structured-input-depth limit exceeded")
		}
		v.node.kind = SequenceValue
		v.node.spelling = ""
		v.node.elements = make([]LocatedValue, 0, len(n.Values))
		for i, e := range n.Values {
			child, err := b.value(e, fmt.Sprintf("%s[%d]", path, i), depth, aliases, use)
			if err != nil {
				return LocatedValue{}, err
			}
			v.node.elements = append(v.node.elements, child)
		}
	case *ast.StringNode:
		v.node.kind = StringValue
		v.node.text = n.Value
		if n.Token.Type == token.StringType {
			if coreInteger.MatchString(n.Value) {
				if !decimalInteger.MatchString(n.Value) {
					return fail("integer must use decimal notation without leading zeros")
				}
				v.node.kind = IntegerValue
			} else if coreNumber.MatchString(n.Value) || coreSpecialNumber.MatchString(n.Value) {
				v.node.kind = NumberValue
			}
		}
	case *ast.LiteralNode:
		v.node.kind = StringValue
		snapshot, err := b.block(n)
		if err != nil {
			return fail(err.Error())
		}
		v.node.text = snapshot.text
		v.node.spelling = snapshot.spelling
	case *ast.IntegerNode:
		if !decimalInteger.MatchString(n.Token.Value) {
			return fail("integer must use decimal notation without leading zeros")
		}
		v.node.kind = IntegerValue
		v.node.spelling = n.Token.Value
		v.node.text = n.Token.Value
	case *ast.FloatNode:
		v.node.kind = NumberValue
		v.node.spelling = n.Token.Value
		v.node.text = n.Token.Value
	case *ast.InfinityNode:
		v.node.kind = NumberValue
		v.node.spelling = n.GetToken().Value
		v.node.text = n.GetToken().Value
	case *ast.NanNode:
		v.node.kind = NumberValue
		v.node.spelling = n.GetToken().Value
		v.node.text = n.GetToken().Value
	case *ast.BoolNode:
		if n.Token.Value != "true" && n.Token.Value != "false" {
			return fail("boolean must be spelled true or false")
		}
		v.node.kind = BooleanValue
		v.node.spelling = n.Token.Value
		v.node.text = n.Token.Value
	case *ast.NullNode:
		return fail("null settings are not permitted")
	case *ast.MergeKeyNode:
		return fail("merge keys are not permitted")
	default:
		return fail("unsupported YAML syntax node")
	}
	return b.finishScalar(n, v)
}

func requireKind(v LocatedValue, k ValueKind) error {
	if v.Kind() != k {
		return invalid(v, "expected "+string(k)+", got "+string(v.Kind()))
	}
	return nil
}
func stringSetting(v LocatedValue) (*string, error) {
	if err := requireKind(v, StringValue); err != nil {
		return nil, err
	}
	s := v.StringValue()
	return &s, nil
}
func boolSetting(v LocatedValue) (*bool, error) {
	if err := requireKind(v, BooleanValue); err != nil {
		return nil, err
	}
	b := v.node.text == "true"
	return &b, nil
}
func stringsSetting(v LocatedValue) (*[]string, error) {
	if err := requireKind(v, SequenceValue); err != nil {
		return nil, err
	}
	s := make([]string, 0, len(v.node.elements))
	for _, e := range v.node.elements {
		item, err := stringSetting(e)
		if err != nil {
			return nil, err
		}
		s = append(s, *item)
	}
	return &s, nil
}
func fields(v LocatedValue, allowed string, visit func(string, LocatedValue) error) error {
	if err := requireKind(v, MappingValue); err != nil {
		return err
	}
	for _, e := range v.node.entries {
		if !slices.Contains(strings.Fields(allowed), e.Key) {
			return &DecodeError{Message: "unknown field " + strconv.Quote(e.Key), Origin: cloneOrigin(e.Origin)}
		}
		if err := visit(e.Key, e.Value); err != nil {
			return err
		}
	}
	return nil
}
func definition(root LocatedValue) (*Definition, error) {
	d := &Definition{syntax: root, Provenance: map[string]Origin{}}
	var err error
	err = fields(root, "version flavor rules exclude directives allow_nested_excludes resources frontmatter documents integrations", func(k string, v LocatedValue) error {
		switch k {
		case "version":
			if e := requireKind(v, IntegerValue); e != nil {
				return e
			}
			if v.node.text != "1" {
				return invalid(v, "unsupported schema version; expected integer 1")
			}
			x := Version
			d.Version = &x
		case "flavor":
			d.Flavor, err = stringSetting(v)
		case "exclude":
			d.Exclude, err = stringsSetting(v)
		case "allow_nested_excludes":
			d.AllowNestedExcludes, err = boolSetting(v)
		case "directives":
			d.Directives = &Directives{}
			err = fields(v, "enabled", func(_ string, x LocatedValue) error { var e error; d.Directives.Enabled, e = boolSetting(x); return e })
		case "resources":
			d.Resources = &Resources{}
			err = fields(v, "index_file anchor_style", func(k string, x LocatedValue) error {
				p, e := stringSetting(x)
				if k == "index_file" {
					d.Resources.IndexFile = p
				} else {
					d.Resources.AnchorStyle = p
				}
				return e
			})
		case "frontmatter":
			d.Frontmatter = &Frontmatter{}
			err = fields(v, "enabled formats", func(k string, x LocatedValue) error {
				var e error
				if k == "enabled" {
					d.Frontmatter.Enabled, e = boolSetting(x)
				} else {
					d.Frontmatter.Formats, e = stringsSetting(x)
				}
				return e
			})
		case "documents":
			d.Documents = &Documents{}
			err = fields(v, "slides", func(_ string, x LocatedValue) error { var e error; d.Documents.Slides, e = stringsSetting(x); return e })
		case "integrations":
			d.Integrations = &Integrations{}
			err = fields(v, "quarto", func(_ string, x LocatedValue) error {
				d.Integrations.Quarto = &Quarto{}
				return fields(x, "enabled", func(_ string, y LocatedValue) error {
					var e error
					d.Integrations.Quarto.Enabled, e = boolSetting(y)
					return e
				})
			})
		case "rules":
			if e := requireKind(v, MappingValue); e != nil {
				return e
			}
			d.Rules = make(map[string]RuleSetting, len(v.node.entries))
			for _, entry := range v.node.entries {
				if e := requireKind(entry.Value, MappingValue); e != nil {
					return e
				}
				setting := RuleSetting{Options: engine.Options{}}
				for _, option := range entry.Value.node.entries {
					switch option.Key {
					case "enabled":
						setting.Enabled, err = boolSetting(option.Value)
					case "allow_directives":
						setting.AllowDirectives, err = boolSetting(option.Value)
					default:
						setting.Options[option.Key] = optionValue(option.Value)
					}
					if err != nil {
						return err
					}
				}
				d.Rules[entry.Key] = setting
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if d.Version == nil {
		return nil, invalid(root, "required version field is missing")
	}
	collectOrigins(root, d.Provenance)
	return d, nil
}

// optionValue provides familiar scalar/string-list values to existing passive
// types. Unsupported shapes and oversized integers remain LocatedValue rather
// than being coerced or truncated; Syntax is authoritative for semantic checks.
func optionValue(v LocatedValue) any {
	switch v.Kind() {
	case MappingValue:
		return v
	case StringValue:
		return v.StringValue()
	case BooleanValue:
		return v.node.text == "true"
	case IntegerValue, NumberValue:
		if c := v.node.conversion; c != nil && c.representable {
			return c.value
		}
	case SequenceValue:
		r := make([]string, 0, len(v.node.elements))
		for _, e := range v.node.elements {
			if e.Kind() != StringValue {
				return v
			}
			r = append(r, e.StringValue())
		}
		return r
	}
	return v
}
func collectOrigins(v LocatedValue, m map[string]Origin) {
	if p := v.node.origin.PropertyPath; p != "" {
		m[p] = v.Origin()
	}
	for _, e := range v.node.entries {
		collectOrigins(e.Value, m)
	}
	for _, e := range v.node.elements {
		collectOrigins(e, m)
	}
}

// scalarText extracts content without implicit kind coercion. This is used only
// when a recognized explicit core tag has already specified the scalar kind.
func scalarText(n ast.Node) (string, bool) {
	switch n := n.(type) {
	case *ast.StringNode:
		return n.Value, true
	case *ast.LiteralNode:
		return n.Value.Value, true
	case *ast.IntegerNode:
		return n.Token.Value, true
	case *ast.FloatNode:
		return n.Token.Value, true
	case *ast.BoolNode:
		return n.Token.Value, true
	case *ast.NullNode:
		return n.Token.Value, true
	case *ast.InfinityNode:
		return n.GetToken().Value, true
	case *ast.NanNode:
		return n.GetToken().Value, true
	}
	return "", false
}

// block retains exact presentation bytes using parser source boundaries. Header
// positions use code-point columns, whereas source slicing uses byte offsets.
// The body token's successor starts after the scalar, so its line start excludes
// unrelated next-key/comment indentation without stripping significant blank
// lines or chomping notation. No YAML syntax is reparsed here.
func (b *yamlBuilder) block(n *ast.LiteralNode) (blockScalar, error) {
	if cached, ok := b.blocks[n]; ok {
		return cached, nil
	}
	if n == nil {
		return blockScalar{}, fmt.Errorf("invalid YAML block scalar source boundary")
	}
	if n.Start == nil || n.Start.Position == nil || n.Value == nil || n.Value.Token == nil {
		return blockScalar{}, fmt.Errorf("invalid YAML block scalar source boundary")
	}
	start, err := b.sourceOffset(n.Start.Position.Line, n.Start.Position.Column)
	if err != nil {
		return blockScalar{}, err
	}
	// The pinned parser can position a tagged block token on separating
	// horizontal whitespace. Exclude that separator from scalar notation.
	for start < len(b.content) && (b.content[start] == ' ' || b.content[start] == '\t') {
		start++
	}
	if start >= len(b.content) || (b.content[start] != '|' && b.content[start] != '>') {
		return blockScalar{}, fmt.Errorf("invalid YAML block scalar source boundary")
	}
	end := len(b.content)
	if next := n.Value.Token.Next; next != nil {
		if next.Position == nil || next.Position.Line < 1 || next.Position.Line > len(b.lineStarts) {
			return blockScalar{}, fmt.Errorf("invalid YAML block scalar source boundary")
		}
		end = b.lineStarts[next.Position.Line-1]
	}
	if end < start || end > len(b.content) {
		return blockScalar{}, fmt.Errorf("invalid YAML block scalar source boundary")
	}
	spelling := b.content[start:end]
	text := n.Value.Value
	// Both strings are shared immutable storage already owned by this decode.
	// Charge source/semantic inspection once before caching rather than charging
	// or allocating a body-sized copy for every alias use.
	b.work += int64(len(spelling) + len(text))
	if b.work > 250000000 {
		return blockScalar{}, fmt.Errorf("work-units limit exceeded during YAML/alias validation")
	}
	cached := blockScalar{spelling: spelling, text: text}
	b.blocks[n] = cached
	return cached, nil
}

// yamlLineStarts follows YAML's three ASCII line break forms. CRLF advances one
// logical line, while preserving both original bytes for exact source slices.
func yamlLineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' {
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		} else if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// sourceOffset checks parser-derived positions before indexing or slicing. A
// code-point column may identify the end of a line, but cannot cross a break.
func (b *yamlBuilder) sourceOffset(line, column int) (int, error) {
	invalid := func() (int, error) { return 0, fmt.Errorf("invalid YAML block scalar source boundary") }
	if line < 1 || line > len(b.lineStarts) || column < 1 {
		return invalid()
	}
	offset := b.lineStarts[line-1]
	if offset < 0 || offset > len(b.content) {
		return invalid()
	}
	for c := 1; c < column; c++ {
		if offset >= len(b.content) || b.content[offset] == '\r' || b.content[offset] == '\n' {
			return invalid()
		}
		_, width := utf8.DecodeRuneInString(b.content[offset:])
		offset += width
	}
	return offset, nil
}

// finishScalar admits a potentially body-sized primitive conversion once per
// source scalar. Aliases share the immutable result, including overflow failure,
// while unrepresentable options return their own located node at each use.
func (b *yamlBuilder) finishScalar(n ast.Node, v LocatedValue) (LocatedValue, error) {
	if v.Kind() != IntegerValue && v.Kind() != NumberValue {
		return v, nil
	}
	if c, ok := b.conversions[n]; ok {
		v.node.conversion = c
		return v, nil
	}
	b.work += int64(len(v.node.text))
	if b.work > 250000000 {
		return LocatedValue{}, invalid(v, "work-units limit exceeded during YAML numeric conversion")
	}
	c := &scalarConversion{}
	if v.Kind() == IntegerValue {
		value, err := strconv.Atoi(v.node.text)
		if err == nil {
			c.value = value
			c.representable = true
		}
	} else {
		value, err := strconv.ParseFloat(v.node.text, 64)
		if err == nil {
			c.value = value
			c.representable = true
		}
	}
	b.conversions[n] = c
	v.node.conversion = c
	return v, nil
}

// normalizeEmptyBlocks adapts one pinned-lexer defect without parsing source
// grammar. After a recognized block header and a whitespace-only body token,
// v1.19.2 may emit a spurious dedent Invalid. Its line/offset must match the body
// extent and its position must exactly match the following token. Only that
// marker is removed. A synthetic no-body value is normalized to empty; actual
// blank-line content retains the lexer's decoded value and original source.
// All unrelated invalid markers remain for the maintained parser to reject.
// Source/token limits are admitted before this bounded in-place token pass.
func normalizeEmptyBlocks(tokens token.Tokens) token.Tokens {
	var header, empty *token.Token
	out := tokens[:0]
	removed := false
	for _, tk := range tokens {
		if tk.Type == token.LiteralType || tk.Type == token.FoldedType {
			header = tk
			empty = nil
		} else if tk.Type == token.CommentType && header != nil && tk.Position != nil && header.Position != nil && tk.Position.Line == header.Position.Line {
			// A header comment does not separate its synthetic empty body token.
		} else if tk.Type == token.StringType && header != nil && blankBlockToken(tk) && tk.Position != nil && header.Position != nil && tk.Position.Line > header.Position.Line && tk.Position.Column == 1 {
			empty = tk
			header = nil
			if !strings.ContainsAny(tk.Origin, "\r\n") {
				tk.Value = ""
			}
		} else if tk.Type == token.InvalidType && empty != nil && empty.Next == tk && tk.Origin == "" && tk.Value == "" && tk.Error == "could not find multi-line content" && emptyBlockErrorPosition(empty, tk) {
			empty = nil
			header = nil
			removed = true
			continue
		} else {
			header = nil
			empty = nil
		}
		out = append(out, tk)
	}
	if removed {
		for i, tk := range out {
			tk.Prev = nil
			tk.Next = nil
			if i > 0 {
				tk.Prev = out[i-1]
			}
			if i+1 < len(out) {
				tk.Next = out[i+1]
			}
		}
	}
	return out
}
func blankBlockToken(t *token.Token) bool {
	for i := range len(t.Origin) {
		if t.Origin[i] != ' ' && t.Origin[i] != '\r' && t.Origin[i] != '\n' {
			return false
		}
	}
	for i := range len(t.Value) {
		if t.Value[i] != '\n' {
			return false
		}
	}
	return true
}
func emptyBlockErrorPosition(empty, marker *token.Token) bool {
	if empty.Position == nil || marker.Position == nil || marker.Next == nil || marker.Next.Position == nil {
		return false
	}
	a, b, c := empty.Position, marker.Position, marker.Next.Position
	lines := yamlBreakCount(empty.Origin)
	lastBreak := strings.LastIndexAny(empty.Origin, "\r\n")
	indent := len(empty.Origin) - lastBreak - 1
	return a.Line+lines == b.Line && a.Offset+lines == b.Offset && b.Column == 1+indent && b.Line == c.Line && b.Column == c.Column && b.Offset == c.Offset
}

func yamlBreakCount(s string) int {
	lines := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			lines++
		} else if s[i] == '\n' {
			lines++
		}
	}
	return lines
}

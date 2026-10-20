package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNamedEntitiesAgainstAuthoritativeData(t *testing.T) {
	data, err := os.ReadFile("testdata/html5/entities.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "d741d877ac77c4194c4ad526b5b4a19aef8dfe411ab840a466891cdbb9f362e6" {
		t.Fatal("authoritative entity source drift")
	}
	var entries map[string]struct {
		Codepoints []rune
		Characters string
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	checked, longest := 0, 0
	for name, e := range entries {
		if string(e.Codepoints) != e.Characters {
			t.Fatal("inconsistent independent oracle", name)
		}
		got, width, kind := decodeToken(name)
		if !strings.HasSuffix(name, ";") {
			if width != 0 {
				t.Fatal("browser-only semicolonless entity accepted", name)
			}
			continue
		}
		checked++
		longest = max(longest, len(name))
		if got != e.Characters || width != len(name) || kind != Entity {
			t.Fatalf("%s: %q/%d/%s want %q", name, got, width, kind, e.Characters)
		}
		// Actual parser output must have the entire raw token as provenance,
		// including names whose semantic value is punctuation or two scalars.
		r := Parse(context.Background(), NewSource([]byte(name)))
		if r.Status != Complete || r.Failure != nil {
			t.Fatal(name, r)
		}
		text := r.Document.Root().Children()[0].Children()[0]
		parts := text.Content()
		if text.Properties().Literal != Some(e.Characters) || len(parts) != 1 || parts[0].Transformation != Entity || parts[0].Source.Start() != 0 || parts[0].Source.End() != len(name) || parts[0].Semantic != Some(e.Characters) {
			t.Fatal("named entity lost source/semantic identity", name, text)
		}
	}
	if checked != 2125 || checked != len(namedEntities) || longest != maxEntityToken {
		t.Fatal("incomplete table or lookahead bound", checked, longest)
	}
}

func TestDecodingNumericGrammarAndLiteralFallback(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"&#0;", "\ufffd"}, {"&#x0;", "\ufffd"}, {"&#X41;", "A"},
		{"&#0000065;", "A"}, {"&#x000041;", "A"},
		{"&#55295;", "\ud7ff"}, {"&#55296;", "\ufffd"}, {"&#57343;", "\ufffd"}, {"&#57344;", "\ue000"},
		{"&#1114111;", "\U0010ffff"}, {"&#1114112;", "\ufffd"}, {"&#9999999;", "\ufffd"},
		{"&#xD800;", "\ufffd"}, {"&#xDFFF;", "\ufffd"}, {"&#x110000;", "\ufffd"}, {"&#xFFFFFF;", "\ufffd"},
		{"&#128;", "\u0080"}, {"&#x80;", "\u0080"}, {"&#11;", "\v"}, {"&#xFFFE;", "\ufffe"},
		{"&#9;", "\t"}, {"&#10;", "\n"}, {"&#13;", "\r"},
		{"&amp;", "&"}, {"&AMP;", "&"}, {"&NotEqualTilde;", "\u2242\u0338"},
	} {
		got, width, kind := decodeToken(tc.input)
		if got != tc.want || width != len(tc.input) || kind != Entity {
			t.Fatalf("%q: %q/%d/%s want %q", tc.input, got, width, kind, tc.want)
		}
	}
	for _, input := range []string{"&", "&x;", "&Amp;", "&notit;", "&notinvaX;", "&amp", "&madeup;", "&#;", "&#x;", "&#-1;", "&# 1;", "&#12a;", "&#00000065;", "&#x0000041;", "&#65", "\\a", "\\é", "\\", "\\\n", "\\\r\n", "&" + strings.Repeat("a", 10000) + ";", "&#" + strings.Repeat("1", 10000) + ";"} {
		if _, width, _ := decodeToken(input); width != 0 {
			t.Fatal("malformed/unrecognized token decoded", input)
		}
	}
}

func TestDecodingInterruptionAndEncodingPrecedence(t *testing.T) {
	for _, input := range []string{
		"> " + strings.Repeat("é&amp;\\*", 300) + "\n",
		"~~~ " + strings.Repeat("&amp;", 300) + "\nraw &amp;\n~~~\n",
		"[a]: /url \"" + strings.Repeat("&amp;", 300) + "\"\n",
	} {
		source := NewSource([]byte(input))
		// Direct decoder checkpoints prove that the newly added work itself,
		// rather than merely an earlier block pass, honors both outcomes.
		index := NewLineIndex(source)
		parts := index.semanticText(0, source.Len()).Segments
		for _, deadline := range []bool{false, true} {
			ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: 5}, deadline: deadline}
			if value := decodeValue(ctx, parts, false); value.Semantic.Present || len(value.Segments) != 0 {
				t.Fatal("interrupted decoder published partial value", value)
			}
			observed := 0
			for checkpoint := 1; checkpoint < 120; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				r := Parse(ctx, source)
				if r.Interruption == "" {
					continue
				}
				observed++
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if r.Interruption != want || r.Failure != nil || r.Status != Incomplete || r.Document.References().LookupAvailability != Unavailable || string(source.Bytes()) != input {
					t.Fatal("decoder interruption leaked complete state", r)
				}
				bad := Parse(ctx, NewSource([]byte("&amp;é\xff")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Interruption != "" || bad.Failure.Location.Start() != 7 {
					t.Fatal("encoding precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("insufficient decoder checkpoint coverage")
			}
		}
	}
}

func TestDecodingPreservesIndentationContributions(t *testing.T) {
	source := NewSource([]byte("\t&amp;\x00"))
	index := NewLineIndex(source)
	cursor, err := index.Indentation(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cursor.Consume(2); err != nil {
		t.Fatal("partial-tab fixture", err)
	}
	value := decodeValue(context.Background(), cursor.Remaining().Segments, false)
	if value.Semantic != Some("  &\ufffd") || len(value.Segments) != 3 {
		t.Fatal(value)
	}
	wants := []struct {
		a, b int
		kind Transformation
		text string
	}{
		{0, 1, PartialTab, "  "}, {1, 6, Entity, "&"}, {6, 7, Normalized, "\ufffd"},
	}
	for i, w := range wants {
		part := value.Segments[i]
		if part.Source.Start() != w.a || part.Source.End() != w.b || part.Transformation != w.kind || part.Semantic != Some(w.text) {
			t.Fatal("lost helper provenance", part, w)
		}
	}
	if string(source.Bytes()) != "\t&amp;\x00" {
		t.Fatal("source changed")
	}
}

package main

// Coverage for the two encoders and the line splitter.
//
// These are small and they are load-bearing in different directions: Quote and
// RenderValue decide what a human reads in a diagnostic, EncodeJSONString
// decides whether the `--json` document is parseable at all, and SplitLines
// decides the line number a reader is sent to.

import (
	"encoding/json"
	"strings"
	"testing"
)

// The --json document must be parseable, and it must round-trip the string it
// was given. An encoder that drops an escape produces a document a consumer
// cannot read; one that over-escapes produces a document that reads but says
// something else.
func TestEncodeJSONStringRoundTrips(t *testing.T) {
	inputs := []string{
		"",
		"plain",
		`quotes " and \ backslash`,
		"control\x00\x01\x1f",
		"tab\there\nnewline\rcr\bbs\fff",
		"em dash — and é and 🚀",  // non-ASCII stays literal
		"<script>&amp;</script>", // HTML metacharacters are NOT escaped
		"could not read/parse JSON: unexpected content after the end of the JSON value (line 1, column 3)",
	}
	for _, in := range inputs {
		encoded := EncodeJSONString(in)

		var decoded string
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			t.Errorf("EncodeJSONString(%q) = %s, which is not a JSON string: %v", in, encoded, err)
			continue
		}
		if decoded != in {
			t.Errorf("EncodeJSONString(%q) round-tripped to %q", in, decoded)
		}
	}

	// The two specific escapes worth naming, because encoding/json disagrees
	// about both and reaching for it is the obvious "simplification".
	if got := EncodeJSONString("a<b>&c"); got != `"a<b>&c"` {
		t.Errorf(`EncodeJSONString("a<b>&c") = %s, want the metacharacters literal`, got)
	}
	if got := EncodeJSONString("—"); got != `"—"` {
		t.Errorf(`EncodeJSONString("—") = %s, want the em dash literal UTF-8`, got)
	}
}

// A whole report must parse. This is the assertion the per-string test cannot
// make: the escaping can be right and the document still be malformed, because
// the document is assembled by hand around it.
func TestJSONReportIsParseable(t *testing.T) {
	report := &JSONReport{Mode: "source", Files: 2, Annotations: 3}
	report.Add(JSONFinding{File: "spec/a — b.rb", Line: 7, HasLine: true, OK: true})
	report.Add(JSONFinding{
		File: `spec/"quoted".rb`, Line: 9, HasLine: true, OK: false, Kind: KindParse,
		Errors: []string{`could not parse annotation: unpaired surrogate escape \uD800`},
	})
	report.NoMatch("spec/**/*.ts")

	out := captureStdout(t, func() { report.Emit(1) })

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("the --json document does not parse: %v\n%s", err, out)
	}
	if parsed["ok"] != false {
		t.Errorf(`"ok" = %v, want false`, parsed["ok"])
	}
	findings, ok := parsed["findings"].([]any)
	if !ok || len(findings) != 3 {
		t.Fatalf(`"findings" = %#v, want three entries`, parsed["findings"])
	}
	// An empty findings list must also be a list, not null — a consumer should
	// never have to branch on the type.
	empty := captureStdout(t, func() { (&JSONReport{Mode: "stdin"}).Emit(0) })
	if !strings.Contains(empty, `"findings": []`) {
		t.Errorf("an empty report did not render findings as []:\n%s", empty)
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"one", []string{"one"}},
		{"one\ntwo", []string{"one", "two"}},
		{"one\ntwo\n", []string{"one", "two"}}, // no trailing empty line
		{"one\r\ntwo", []string{"one", "two"}}, // CRLF is ONE terminator
		{"one\rtwo", []string{"one", "two"}},   // a lone CR is a terminator
		{"one\n\nthree", []string{"one", "", "three"}},
		{"\n", []string{""}},
		// NOT terminators. A form feed or a U+2028 inside a source file does not
		// start a new line in any editor a reader will open it in, so counting
		// one here would report a line number they cannot find.
		{"a\x0cb", []string{"a\x0cb"}},
		{"a b", []string{"a b"}},
		{"ab", []string{"ab"}},
	}
	for _, tc := range cases {
		got := SplitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SplitLines(%q) = %q, want %q", tc.in, got, tc.want)
				break
			}
		}
	}
}

// The line numbers --source reports must match what an editor shows. This walks
// the shipped fixture and checks one known annotation, so the splitter is
// pinned against a file a reader can open rather than only against synthetic
// strings.
func TestSplitLinesAgreesWithTheShippedFixture(t *testing.T) {
	sites := ExtractIntents("# one\n# @intent: {a: 1}\n# three\r\n# @intent: {b: 2}\n")
	if len(sites) != 2 {
		t.Fatalf("extracted %d sites, want 2", len(sites))
	}
	if sites[0].Line != 2 || sites[1].Line != 4 {
		t.Errorf("lines = %d, %d; want 2, 4", sites[0].Line, sites[1].Line)
	}
}

func TestCharCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"café", 4},             // 5 bytes
		{"a — b", 5},            // the em dash is 3 bytes
		{"🚀", 1},                // 4 bytes, one character
		{"returns 402 pay", 15}, // exactly minLength for `behavior`
	}
	for _, tc := range cases {
		if got := CharCount(tc.in); got != tc.want {
			t.Errorf("CharCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	// The one that matters to a verdict: a 15-CHARACTER behavior sentence
	// carrying an em dash is 17 bytes, so a byte count would admit a sentence
	// the schema forbids.
	if CharCount("a — sentence !!") != 15 {
		t.Error("CharCount is counting bytes, not characters")
	}
}

// The spelling here is the ecosystem's shared message vocabulary, pinned
// against specguard-rspec's own renderer by that gem's message_parity_spec.rb.
// It is not free to change: `layer: value 'e2e' is not one of [...]` is a line
// CI logs get diffed on, and the two tools build it from different code.
func TestRenderValue(t *testing.T) {
	cases := []struct{ doc, want string }{
		{`null`, `None`},
		{`true`, `True`},
		{`"x"`, `'x'`},
		{`1`, `1`},
		{`1.50`, `1.50`}, // the literal as written, not a re-derived 1.5
		{`1e2`, `1e2`},
		{`["unit", "integration"]`, `['unit', 'integration']`},
		{`{"b": 1, "a": 2}`, `{'b': 1, 'a': 2}`}, // document order
	}
	for _, tc := range cases {
		var v Value
		var err error
		if v, err = DecodeJSONString(tc.doc); err != nil {
			t.Fatalf("DecodeJSONString(%s): %v", tc.doc, err)
		}
		if got := RenderValue(v); got != tc.want {
			t.Errorf("RenderValue(%s) = %s, want %s", tc.doc, got, tc.want)
		}
	}
	// RenderBare drops the quotes from a top-level string only.
	if got := RenderBare("string"); got != "string" {
		t.Errorf("RenderBare(%q) = %s, want it bare", "string", got)
	}
	if got := RenderBare([]Value{"a"}); got != `['a']` {
		t.Errorf("RenderBare of a list = %s, want its elements quoted", got)
	}
}

// Quote must make an unprintable path visible rather than writing it through.
// A diagnostic naming a file is the one place a raw control character does the
// most damage: it reformats the reader's terminal and hides the name it was
// supposed to show.
func TestQuoteMakesTheInvisibleVisible(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain.json", `'plain.json'`},
		{"with space.json", `'with space.json'`},
		{"café.json", `'café.json'`}, // printable non-ASCII stays literal
		{"line\nbreak.json", `'line\nbreak.json'`},
		{"esc\x1b[2J.json", `'esc\x1b[2J.json'`},
		// The quote flips to double only when the string carries a single one
		// and no double, so the result is always readable without escaping.
		{"it's.json", `"it's.json"`},
		{`it's "quoted".json`, `'it\'s "quoted".json'`},
	} {
		if got := Quote(tc.in); got != tc.want {
			t.Errorf("Quote(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

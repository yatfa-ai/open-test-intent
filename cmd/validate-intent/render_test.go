package main

// Coverage for the two encoders and the line splitter.
//
// These are small and they are load-bearing in different directions: Quote and
// RenderValue decide what a human reads in a diagnostic, EncodeJSONString
// decides whether the `--json` document is parseable at all, and SplitLines
// decides the line number a reader is sent to.

import (
	"encoding/json"
	"strconv"
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

// A path the operating system hands us is a byte string, and nothing guarantees
// it is UTF-8. Both renderers have to say so rather than repair it, and the
// `--json` document additionally has to keep two such paths apart — the failure
// this pins is not a crash but a QUIET MERGE: three files, one `"file"` value,
// `summary.files: 3`, and a consumer with no way to notice.
//
// The pair that makes it a real defect rather than a cosmetic one is the last
// two below: a file whose name CONTAINS an undecodable 0xE9, and a file
// genuinely NAMED `a\xe9.json`. Any encoding that writes the first as those four
// characters and leaves the second alone reports them identically.
func TestJSONPathEncodingIsLosslessForOSBytes(t *testing.T) {
	paths := []string{
		"spec/plain.json",
		"spec/em — dash.json",
		"spec/a\xe9.json",
		"spec/a\xfe.json",
		"spec/a\xff.json",
		`spec/a\xe9.json`, // literal backslash-x-e-9, not the byte
		`spec/back\slash.json`,
		"spec/\xed\xa0\x80.json", // CESU-8 high surrogate: three bytes, none decodable
	}

	seen := map[string]string{}
	for _, path := range paths {
		encoded := EncodeJSONPath(path)

		var decoded string
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			t.Errorf("EncodeJSONPath(%q) = %s, which is not a JSON string: %v", path, encoded, err)
			continue
		}
		if strings.ContainsRune(decoded, '\uFFFD') {
			t.Errorf("EncodeJSONPath(%q) = %s — U+FFFD is the repair PROTOCOL.md §1.1(a) forbids", path, encoded)
		}
		if was, clash := seen[decoded]; clash {
			t.Errorf("EncodeJSONPath collapsed two paths onto %q:\n  %q\n  %q", decoded, was, path)
		}
		seen[decoded] = path

		// The documented inverse: `\xHH` is a byte, `\\` is a backslash,
		// everything else is itself. It has to give back the OS's bytes exactly,
		// or `file` is not a name a consumer can act on.
		if back := decodeJSONPath(t, decoded); back != path {
			t.Errorf("EncodeJSONPath(%q) decoded back to %q", path, back)
		}
	}
}

// decodeJSONPath is the inverse EncodeJSONPath's comment promises a consumer.
// It is written here, in the test, on purpose: an inverse that shares code with
// the encoder proves the two agree, not that either is right.
func decodeJSONPath(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\\' {
				b.WriteByte('\\')
				i += 2
				continue
			}
			if s[i+1] == 'x' && i+3 < len(s) {
				n, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
				if err != nil {
					t.Fatalf("%q is not the documented encoding: %v", s, err)
				}
				b.WriteByte(byte(n))
				i += 4
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// The human renderer must not repair either. Before this, `Quote` ranged over
// the string, so every undecodable byte decoded to U+FFFD and the no-match
// diagnostic named a pattern the user had never typed.
func TestQuoteShowsUndecodableBytesRatherThanRepairingThem(t *testing.T) {
	got := Quote("x\xe9*.json")
	if want := `'x\xe9*.json'`; got != want {
		t.Errorf("Quote(%q) = %s, want %s", "x\xe9*.json", got, want)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("Quote(%q) = %s — the byte was repaired, not shown", "x\xe9*.json", got)
	}
	// A real U+FFFD in the input is still a real character and stays literal, so
	// the escape above is not a second spelling of it.
	if got := Quote("x\uFFFD.json"); got != "'x\uFFFD.json'" {
		t.Errorf("Quote of a literal U+FFFD = %s, want it left alone", got)
	}
}

// The whole document, not just the encoder: three findings whose paths differ
// only in one undecodable byte must arrive as three distinguishable entries,
// and `summary` must agree with what `findings` can express.
func TestReportKeepsPathsWithOSBytesApart(t *testing.T) {
	report := &JSONReport{Mode: "adopter", Files: 3, Annotations: 3}
	for _, path := range []string{"spec/a\xe9.json", "spec/a\xfe.json", "spec/a\xff.json"} {
		report.Add(JSONFinding{File: path, OK: false, Kind: KindSchema,
			Errors: []string{"<root>: missing required property 'entity'"}})
	}

	out := captureStdout(t, func() { report.Emit(1) })

	var parsed struct {
		Summary struct {
			Files int `json:"files"`
		} `json:"summary"`
		Findings []struct {
			File string `json:"file"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("the --json document does not parse: %v\n%s", err, out)
	}

	distinct := map[string]bool{}
	for _, f := range parsed.Findings {
		distinct[f.File] = true
	}
	if len(distinct) != parsed.Summary.Files {
		t.Errorf("summary.files = %d but findings name only %d distinguishable files:\n%s",
			parsed.Summary.Files, len(distinct), out)
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

// The spelling here is the ecosystem's shared message vocabulary — a
// CONVENTION specguard-rspec's renderer follows too. The gem pins that
// convention in one direction only: its validator_backend_spec.rb asserts its
// own rendered output against reports RECORDED from this binary, and its
// message_parity_spec.rb asserts its `Schema#violations` output against four
// strings hand-copied from here, under a header that refuses to be read as a
// proof about two programs — rightly, since four strings are not a second
// process. Both are snapshots of this binary's text, so they do cover the two
// shapes below the gem can actually reach — a string (`'x'`) and an array of
// them (the enum list) — but only one way round: a change on the GEM's side
// goes red at once, a change HERE goes red once those snapshots are re-taken.
// No spec in the gem executes this binary. The gem itself does, on the opt-in
// `ValidatorBackend` arm, but that arm reports this binary's rendered strings
// INSTEAD of running its own renderer, so it substitutes one spelling for the
// other rather than comparing them.
// The remaining cases below are unreachable from the gem in any event: the
// annotation schema types every property `string` (or an array of strings), so
// a null/bool/number/object value produces a type-mismatch line and the only
// message that would interpolate the value is dropped as shadowed by it. This
// binary still has to render them because it decodes before it validates — and
// for those arms the spelling is held by this repo's own tests, not by anything
// on the gem's side. How widely varies: blanking the number arm reds
// TestValidateMessages and TestObjectDuplicateKeyKeepsFirstPosition as well as
// this test, because both render numbers through it.
// The spelling is not free to change regardless: `layer: value 'e2e' is not
// one of [...]` is a line CI logs get diffed on, and the two tools build it
// from different code.
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

package main

// Unit tests for the extraction/normalization scanner (source.go).
//
// The self-test grades the CLI's verdicts against the corpus. These tests assert
// the things a verdict cannot show: what the scanner actually
// captured, and what the normalizer actually produced. A port that swallowed
// the wrong substring but still validated it would sail through the harness.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractIntents_line57CapturedSubstring is the ticket's success criterion 2.
//
// examples/sources/order_spec.rb:57 is the roadmap's named "thin ice", exercised
// in shipped code: an annotation followed by trailing prose that contains ITS OWN
// brace pair (`— see ADR-14 {§3} ...`). A greedy regex (`\{.*\}`) captures
// through that pair; a lazy one (`\{.*?\}`) truncates the payload at the first
// `}` inside the behavior string.
//
// The assertion is deliberately on the CAPTURED SUBSTRING, not on the PASS line.
// A port that swallowed the tail would still produce a valid payload for the
// leading object and still print PASS — so asserting the PASS line would be a
// vacuous positive (SPGD-78) that passes for the wrong reason.
func TestExtractIntents_line57CapturedSubstring(t *testing.T) {
	const want = `{ entity: "Order", action: "refund", behavior: "restores stock levels when a paid order is refunded", layer: "unit" }`

	text := readRepoFile(t, filepath.Join("examples", "sources", "order_spec.rb"))

	var got string
	found := false
	for _, site := range ExtractIntents(text) {
		if site.Line == 57 {
			got, found = site.Raw, true
		}
	}
	if !found {
		t.Fatal("no annotation extracted at order_spec.rb:57 — the fixture or the line numbering moved")
	}
	if got != want {
		t.Errorf("captured the wrong substring at order_spec.rb:57\n want: %s\n  got: %s", want, got)
	}
	if strings.Contains(got, "ADR-14") {
		t.Error("the capture swallowed the trailing prose — a greedy scan through the {§3} pair")
	}
	if !strings.Contains(got, `layer: "unit"`) {
		t.Error("the capture was truncated before the end of the payload — a lazy scan")
	}
}

// TestExtractIntents_shippedCorpusLineNumbers pins every finding in the corpus,
// so a change to pySplitlines that shifted line numbers cannot pass unnoticed
// even if the fixtures themselves were edited to match.
func TestExtractIntents_shippedCorpusLineNumbers(t *testing.T) {
	text := readRepoFile(t, filepath.Join("examples", "sources", "order_spec.rb"))
	want := []int{10, 17, 24, 32, 41, 49, 57}
	sites := ExtractIntents(text)
	if len(sites) != len(want) {
		t.Fatalf("extracted %d annotations from order_spec.rb, want %d", len(sites), len(want))
	}
	for i, site := range sites {
		if site.Line != want[i] {
			t.Errorf("annotation %d is at line %d, want %d", i, site.Line, want[i])
		}
		if site.Problem != "" {
			t.Errorf("annotation %d reported a problem: %s", i, site.Problem)
		}
	}
}

// TestExtractIntents_stringLiteralDefectIsReproduced is success criterion 5.
//
// extract_intents cannot distinguish an @intent: inside a string literal from
// one in a real comment. That is an ACCEPTED LIMITATION rather than a bug to
// fix here: telling the two apart needs a parser for every language a test can
// be written in, which is the framework coupling PROTOCOL.md §6 rules out. See
// ExtractIntents. Changing the behaviour is a protocol-level decision for
// bin/validate-intent first.
//
// The real-comment positive control runs in the same test on purpose. Without
// it, a port that had broken extraction entirely would also produce "no
// difference between the two" and read as a pass.
func TestExtractIntents_stringLiteralDefectIsReproduced(t *testing.T) {
	const payload = `{ entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }`

	// PROBE HYGIENE: the host must be SINGLE-quoted. A double-quoted Ruby host
	// makes the line carry backslash-escaped {\"entity\"...}, which the
	// validator rejects as an unterminated object literal — a result that reads
	// exactly like the defect being absent when in fact the payload never
	// reached the string-literal question at all.
	phantom := ExtractIntents("x = '# @intent: " + payload + "'\n")
	real := ExtractIntents("# @intent: " + payload + "\n")

	if len(real) != 1 || real[0].Raw != payload {
		t.Fatalf("positive control failed: the real comment did not extract cleanly (%+v)", real)
	}
	if len(phantom) != 1 {
		t.Fatalf("the string-literal defect was 'fixed': expected 1 phantom extraction, got %d", len(phantom))
	}
	if phantom[0].Raw != real[0].Raw {
		t.Errorf("the port began distinguishing a string literal from a comment\n phantom: %s\n real:    %s",
			phantom[0].Raw, real[0].Raw)
	}
}

// --------------------------------------------------------------------------- //
// scanString / scanObject
// --------------------------------------------------------------------------- //

func TestScanObject_balancedCapture(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"trailing prose with its own braces",
			`# @intent: {a: "b"} — see ADR-14 {§3}`, `{a: "b"}`},
		{"brace inside a double-quoted value",
			`# @intent: {behavior: "a { brace } inside"}`, `{behavior: "a { brace } inside"}`},
		{"brace inside a single-quoted value",
			`# @intent: {behavior: 'a } here'}`, `{behavior: 'a } here'}`},
		{"nested object",
			`# @intent: {a: {b: {c: 1}}} tail`, `{a: {b: {c: 1}}}`},
		{"nested array",
			`# @intent: {a: [1, [2, 3]]} tail`, `{a: [1, [2, 3]]}`},
		{"escaped quote inside a string",
			`# @intent: {a: "say \"hi\" }"} tail`, `{a: "say \"hi\" }"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := ExtractIntents(tc.line)
			if len(sites) != 1 {
				t.Fatalf("expected 1 site, got %d (%+v)", len(sites), sites)
			}
			if sites[0].Raw != tc.want {
				t.Errorf("captured %q, want %q", sites[0].Raw, tc.want)
			}
		})
	}
}

func TestScanObject_problems(t *testing.T) {
	cases := []struct{ line, want string }{
		{`# @intent: no brace here`, "no '{...}' object literal follows the @intent: token"},
		{`# @intent: {a: 1`, "unterminated object literal (an annotation must fit on one line)"},
		{`# @intent: {a: "b`, `unterminated "-quoted string`},
		{`# @intent: {a: 'b`, "unterminated '-quoted string"},
		{`# @intent: {a: 1]`, "unbalanced brackets in the annotation payload"},
	}
	for _, tc := range cases {
		sites := ExtractIntents(tc.line)
		if len(sites) != 1 {
			t.Fatalf("%q: expected 1 site, got %d", tc.line, len(sites))
		}
		if sites[0].Problem != tc.want {
			t.Errorf("%q: problem is %q, want %q", tc.line, sites[0].Problem, tc.want)
		}
		if sites[0].Raw != "" {
			t.Errorf("%q: a failed capture must carry no payload, got %q", tc.line, sites[0].Raw)
		}
	}
}

// TestScanString_backslashBeforeMultibyte pins DIVERGENCE 4 at its sharpest
// point: `i += 2` steps over a backslash and ONE CODE POINT.
//
// It also records the honest finding this port established by measurement: in
// the scanner alone the divergence is latent for ALL valid UTF-8, not merely for
// the shipped corpus. Every byte of a multi-byte UTF-8 sequence is >= 0x80, so
// no continuation byte can be mistaken for a quote, a brace or a backslash —
// meaning a consistently byte-indexed scanner would return byte offsets that
// slice to the same substring. The divergence is genuinely LIVE one layer down,
// in the JSON parser's character offsets, which are reported to the reader
// directly (`café` vs `ascii` before the same syntax error).
//
// The test is kept regardless. "Latent" is a property of today's inputs and
// today's call graph, and the distance from latent to live is one commit by
// someone who has not read this comment.
func TestScanString_backslashBeforeMultibyte(t *testing.T) {
	cases := []string{
		`# @intent: {a: "x\é y"} tail`,
		`# @intent: {a: "x\😀 y"} tail`,
		`# @intent: {a: "é\"}\" z"} tail`,
		`# @intent: {a: 'é\'b'} tail`,
	}
	for _, line := range cases {
		sites := ExtractIntents(line)
		if len(sites) != 1 || sites[0].Problem != "" {
			t.Errorf("%q: expected one clean capture, got %+v", line, sites)
			continue
		}
		// The capture must end at the payload's own closing brace, never in the
		// tail — and must be a whole number of code points.
		if strings.Contains(sites[0].Raw, "tail") {
			t.Errorf("%q: the capture ran into the tail: %q", line, sites[0].Raw)
		}
		if !strings.HasSuffix(sites[0].Raw, "}") {
			t.Errorf("%q: the capture did not end at a brace: %q", line, sites[0].Raw)
		}
	}
}

// --------------------------------------------------------------------------- //
// normalize_payload
// --------------------------------------------------------------------------- //

func TestNormalizePayload(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare-word keys are quoted", `{entity:"Order"}`, `{"entity":"Order"}`},
		{"single-quoted strings are requoted", `{'entity':'Order'}`, `{"entity":"Order"}`},
		{"a bare word in VALUE position is left alone (so it still fails)",
			`{layer: request}`, `{"layer": request}`},
		{"trailing comma before }", `{"a": 1,}`, `{"a": 1}`},
		{"trailing comma before ]", `{"a": [1,]}`, `{"a": [1]}`},
		{"quoted content is never rewritten", `{a: "it's a {brace}"}`, `{"a": "it's a {brace}"}`},
		{`\' becomes a bare ' inside a requoted string`, `{a: 'it\'s'}`, `{"a": "it's"}`},
		{`a " inside a single-quoted string is escaped`, `{a: 'say "hi"'}`, `{"a": "say \"hi\""}`},
		{"other escapes survive requoting", `{a: 'a\nb'}`, `{"a": "a\nb"}`},

		// DIVERGENCE 3. _BARE_WORD_RE is ASCII, so `café` matches only `caf` —
		// which is left UNQUOTED because the next character is `é`, not `:`.
		{"divergence 3: café is not a bare word", `{café: "x"}`, `{café: "x"}`},
		// Only `c` is in key position; `a` and `b` are bare words followed by
		// `<` and `>`, so they stay bare.
		{"divergence 3: a<b>&c quotes only c", `{a<b>&c: "y"}`, `{a<b>&"c": "y"}`},

		// WHITESPACE IS THE UNICODE PROPERTY, and nothing wider. PROTOCOL.md §1
		// says the keys are "whitespace-tolerant"; a C0 information separator is
		// not whitespace under any Unicode definition, so it does not separate a
		// bare key from its colon and the word is left unquoted. The payload
		// then fails to parse, which is the loud answer — the quiet one would be
		// normalizing a control character out of the middle of a key and
		// validating whatever came out.
		{"a unit separator does not make a bare word a key",
			"{entity\u001f: \"x\"}", "{entity\u001f: \"x\"}"},
		{"a unit separator does not reach the trailing-comma lookahead",
			"{\"a\": 1,\u001f}", "{\"a\": 1,\u001f}"},
		// Real whitespace, as the control.
		{"whitespace: a plain space", `{entity : "x"}`, `{"entity" : "x"}`},
		{"whitespace: NBSP", "{entity\u00a0: \"x\"}", "{\"entity\"\u00a0: \"x\"}"},
		{"whitespace: a tab before the colon", "{entity\t: \"x\"}", "{\"entity\"\t: \"x\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizePayload([]rune(tc.in))
			if err != nil {
				t.Fatalf("NormalizePayload(%q) errored: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NormalizePayload(%q)\n want %q\n  got %q", tc.in, tc.want, got)
			}
		})
	}
}

func TestNormalizePayload_unterminatedString(t *testing.T) {
	for _, in := range []string{`{a: "b`, `{a: 'b`} {
		if _, err := NormalizePayload([]rune(in)); err == nil {
			t.Errorf("NormalizePayload(%q) should have reported an unterminated string", in)
		}
	}
}

// --------------------------------------------------------------------------- //
// helpers
// --------------------------------------------------------------------------- //

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	// The test binary runs in the package directory, so the repo root is two
	// levels up from cmd/validate-intent.
	path := filepath.Join("..", "..", rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

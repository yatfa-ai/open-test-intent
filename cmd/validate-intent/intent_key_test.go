package main

// The `intent` key across ALL FOUR --json renderers.
//
// WHY THIS IS ONE FILE RATHER THAN AN ASSERTION ADDED TO EACH MODE'S TESTS.
// The key's contract is not "source mode reports the payload" — it is that
// EVERY finding has the same shape whatever produced it (report.go's
// JSONFinding doc). A consumer written against that promise never branches on
// `mode`, so a key present under --source and absent under stdin hands that
// branch straight back and does it silently: each mode's own tests would still
// be green, because each would be asserting only about itself. The invariant
// lives BETWEEN the modes, so the test has to as well.
//
// The other half of the contract is that `intent` reports WHAT THE PAYLOAD SAID
// and not WHETHER IT WAS GOOD — `ok` and `kind` already answer that. So the
// interesting row here is the SCHEMA-REJECTED one, which must carry its payload
// (it parsed) while reporting ok:false. A renderer that populated `intent` only
// on success would pass a test that checked the valid and the unreadable cases
// alone, and would have thrown away the one payload a consumer most wants to
// see: the annotation someone got wrong.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// intentOf returns the text of every `"intent": ...` value in a document, in
// document order, paired with the `"ok"` that preceded it.
//
// Scanned out of the RAW TEXT rather than decoded, for the reason
// TestRunStdinJSON_documentShape gives about key order: decoding would answer
// the same for a document that emitted the key correctly and for one that
// emitted it in a different position, and position is part of what a consumer
// diffing two reports depends on.
func intentValues(t *testing.T, out string) []string {
	t.Helper()
	var values []string
	for _, chunk := range strings.Split(out, `"intent": `)[1:] {
		// A finding's intent is the last key in its object, so the value runs
		// to the line that closes the finding — the first `\n    }` after it.
		end := strings.Index(chunk, "\n    }")
		if end < 0 {
			t.Fatalf("an `intent` value is not terminated by a finding close; document was:\n%s", out)
		}
		values = append(values, strings.TrimSpace(chunk[:end]))
	}
	return values
}

// countFindings is the number of findings the document reports, so a test can
// prove it saw an `intent` for EVERY one of them rather than for some.
func countFindings(out string) int {
	return strings.Count(out, `      "file": `)
}

// assertEveryFindingCarriesAnIntent is the all-modes invariant itself.
func assertEveryFindingCarriesAnIntent(t *testing.T, mode, out string) []string {
	t.Helper()
	findings := countFindings(out)
	values := intentValues(t, out)
	if findings == 0 {
		t.Fatalf("%s: the document reports no findings, so this test would be vacuous; document was:\n%s", mode, out)
	}
	if len(values) != findings {
		t.Fatalf("%s: %d finding(s) but %d `intent` key(s) — the key is not in every finding; document was:\n%s",
			mode, findings, len(values), out)
	}
	return values
}

// --------------------------------------------------------------------------- //
// --source
// --------------------------------------------------------------------------- //

// A valid annotation carries what it parsed to.
func TestIntentKey_sourceModeCarriesTheValidatedPayload(t *testing.T) {
	schema := repoSchema(t)
	out, _ := runSourceJSON(t, []string{sourceFixture(t, "examples/sources/order_spec.rb")}, schema)

	values := assertEveryFindingCarriesAnIntent(t, "source", out)
	for i, v := range values {
		if v == "null" {
			t.Errorf("finding %d carries `intent: null` on a file of valid annotations; document was:\n%s", i, out)
		}
	}

	// The payload itself, not merely something non-null. All three of this
	// fixture's leading forms are the SAME intent written three ways
	// (PROTOCOL.md §1's "Equivalent forms"), so the carried value proves the
	// normalizer ran and that what is reported is the parse RESULT rather than
	// the author's surface syntax.
	for _, want := range []string{
		`"entity": "Order"`,
		`"action": "checkout"`,
		`"behavior": "returns 402 payment required on expired card"`,
		`"layer": "request"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the carried intent is missing %s; document was:\n%s", want, out)
		}
	}
}

// THE LOAD-BEARING ROW: a payload the SCHEMA rejected still carries its intent,
// because it parsed. `ok` reports the verdict; `intent` reports the content.
//
// The fixture's five annotations fall into two classes, and the expected
// `intent` differs by CLASS rather than by verdict — which is the distinction a
// renderer keyed on `ok` would flatten, since all five are ok:false:
//
//	schema-rejected (3)  parsed -> intent PRESENT, ok false
//	extraction  (2)      the payload was never captured, so it never became a
//	                     value -> intent null
//
// Both of the last two are KindExtraction, NOT one parse failure and one
// extraction failure: an unterminated object literal and a bare `@intent:` are
// both payloads the EXTRACTOR could not capture, so neither ever reaches the
// parser. Verified against CheckSourceText rather than assumed — the two read
// like different failures in the fixture's own comments, and a KindParse row
// from this file is unreachable.
func TestIntentKey_sourceModeCarriesASchemaRejectedPayloadButNotAnUnparsedOne(t *testing.T) {
	schema := repoSchema(t)
	out, code := runSourceJSON(t,
		[]string{sourceFixture(t, "examples/sources/invalid/broken_intent_spec.rb")}, schema)

	if code == 0 {
		t.Fatalf("a file of invalid annotations exited 0; document was:\n%s", out)
	}
	values := assertEveryFindingCarriesAnIntent(t, "source", out)
	if len(values) != 5 {
		t.Fatalf("want 5 findings from the invalid fixture, got %d; document was:\n%s", len(values), out)
	}

	// The three schema failures parsed, so they carry their payload.
	for i, v := range values[:3] {
		if v == "null" {
			t.Errorf("schema-rejected finding %d carries `intent: null`; it PARSED, so the payload "+
				"is exactly what a consumer needs to see. Document was:\n%s", i, out)
		}
	}
	// ...and the payload is the AUTHOR'S, typo included. `entiity` is what
	// `additionalProperties: false` rejected, and reporting the key the author
	// actually wrote is the difference between a report they can act on and one
	// that only says "no".
	if !strings.Contains(values[0], `"entiity": "Order"`) {
		t.Errorf("the first finding should carry the author's typo'd key verbatim; got:\n%s", values[0])
	}
	if !strings.Contains(values[1], `"layer": "model"`) {
		t.Errorf("the second finding should carry the out-of-enum value verbatim; got:\n%s", values[1])
	}

	// The two that never became a value carry null.
	for i, v := range values[3:] {
		if v != "null" {
			t.Errorf("finding %d never parsed, so its intent must be null, got %s; document was:\n%s",
				i+3, v, out)
		}
	}
}

// A payload that EXTRACTS but does not PARSE carries no intent.
//
// This is the KindParse row of --source mode, and it is unreachable from
// examples/sources/invalid/broken_intent_spec.rb — both of that fixture's
// non-schema failures are KindExtraction, so a mutation that fabricated an
// intent on this branch survived the test above. Written from payloads probed
// against CheckSourceText rather than assumed, because the difference between
// "the extractor could not capture it" and "the parser refused it" is invisible
// in the source text.
//
// The surrogate row is THE PAYLOAD THAT MOTIVATED THIS TICKET, and it belongs
// here rather than in a fixture asserting a verdict: PROTOCOL.md §1.1(a) makes
// it a PARSE failure, so it never becomes a value and the `intent` key reports
// nothing for it. That is the whole answer to "what does the formatter show for
// a lone surrogate" — the question is dissolved upstream, not represented.
func TestIntentKey_sourceModeParseFailureCarriesNull(t *testing.T) {
	schema := repoSchema(t)

	for _, tc := range []struct{ name, annotation, wantClause string }{
		{"truncated value", `# @intent: { entity: "Order", action: }`, ""},
		{"unpaired surrogate escape", `# @intent: { "entity": "\ud800" }`, "PROTOCOL.md §1.1(a)"},
		{"non-finite literal", `# @intent: { "a": NaN }`, "PROTOCOL.md §1.1(b)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, "parse_fail_spec.rb", []byte(tc.annotation+"\nit \"x\" do\nend\n"))
			out, code := runSourceJSON(t, []string{path}, schema)

			if code == 0 {
				t.Fatalf("an unparseable annotation exited 0; document was:\n%s", out)
			}
			if !strings.Contains(out, `"kind": "`+KindParse+`"`) {
				t.Fatalf("want a %s finding — this test is about the parse branch, and a row of "+
					"another kind means it is exercising something else; document was:\n%s", KindParse, out)
			}
			if tc.wantClause != "" && !strings.Contains(out, tc.wantClause) {
				t.Errorf("the diagnostic does not cite %s; document was:\n%s", tc.wantClause, out)
			}
			for _, v := range assertEveryFindingCarriesAnIntent(t, "source", out) {
				if v != "null" {
					t.Errorf("a payload that never parsed must carry `intent: null`, got %s; "+
						"document was:\n%s", v, out)
				}
			}
		})
	}
}

// A file that could not be read is a finding with no payload.
func TestIntentKey_sourceModeReadFailureCarriesNull(t *testing.T) {
	schema := repoSchema(t)
	missing := filepath.Join(t.TempDir(), "unreadable_spec.rb")
	if err := os.WriteFile(missing, []byte("# @intent: {}\n"), 0o000); err != nil {
		t.Fatalf("writing the unreadable fixture: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read a 0o000 file — the read failure is unreachable")
	}

	out, _ := runSourceJSON(t, []string{missing}, schema)
	for _, v := range assertEveryFindingCarriesAnIntent(t, "source", out) {
		if v != "null" {
			t.Errorf("an unreadable file must carry `intent: null`, got %s; document was:\n%s", v, out)
		}
	}
}

// A glob matching nothing is a finding too, and it has no payload.
func TestIntentKey_noMatchCarriesNull(t *testing.T) {
	schema := repoSchema(t)
	out, _ := runSourceJSON(t, []string{filepath.Join(t.TempDir(), "nope-*.rb")}, schema)

	if !strings.Contains(out, `"kind": "`+KindNoMatch+`"`) {
		t.Fatalf("expected a no-match finding; document was:\n%s", out)
	}
	for _, v := range assertEveryFindingCarriesAnIntent(t, "source", out) {
		if v != "null" {
			t.Errorf("a no-match finding must carry `intent: null`, got %s; document was:\n%s", v, out)
		}
	}
}

// --------------------------------------------------------------------------- //
// adopter and stdin — the two modes the key was NOT motivated by
// --------------------------------------------------------------------------- //

// Adopter mode carries the payload for a valid document and null for one that
// could not be read.
//
// This mode is why the key is emitted everywhere rather than only where it was
// wanted: nothing in --source's motivation implies it, and leaving it out here
// is the shape-divergence the JSONFinding contract forbids.
func TestIntentKey_adopterModeCarriesThePayloadAndNullForAnUnreadableFile(t *testing.T) {
	schema := repoSchema(t)

	valid := sourceFixture(t, "examples/unit-order-total.json")
	var out string
	out = captureStdout(t, func() { RunAdopterJSON([]string{valid}, schema) })
	values := assertEveryFindingCarriesAnIntent(t, "adopter", out)
	if len(values) != 1 || values[0] == "null" {
		t.Fatalf("a valid document should carry its payload; document was:\n%s", out)
	}
	if !strings.Contains(values[0], `"entity": "Order"`) || !strings.Contains(values[0], `"layer": "unit"`) {
		t.Errorf("the carried intent is not the fixture's content; got:\n%s", values[0])
	}

	// A document that is not JSON at all: parsed nothing, so it carries nothing.
	malformed := writeTemp(t, "malformed.json", []byte("{not json"))
	out = captureStdout(t, func() { RunAdopterJSON([]string{malformed}, schema) })
	for _, v := range assertEveryFindingCarriesAnIntent(t, "adopter", out) {
		if v != "null" {
			t.Errorf("an unparseable document must carry `intent: null`, got %s; document was:\n%s", v, out)
		}
	}
}

// Stdin mode, across all three of its outcomes.
func TestIntentKey_stdinModeAcrossReadParseAndSchema(t *testing.T) {
	schema := repoSchema(t)
	valid, err := os.ReadFile(sourceFixture(t, "examples/unit-order-total.json"))
	if err != nil {
		t.Fatalf("reading a valid fixture: %v", err)
	}

	for _, tc := range []struct {
		name       string
		input      []byte
		wantIntent bool
	}{
		// Parsed and valid.
		{"valid", valid, true},
		// Parsed, then rejected by the schema — carries its payload.
		{"schema violation", []byte(`{"entity": "Order"}`), true},
		// Never parsed.
		{"malformed", []byte("{not json"), false},
		// Never decoded: not well-formed UTF-8, so it is a READ failure.
		{"undecodable bytes", []byte{0xff, 0xfe, 0xfd}, false},
		// §1.1(a): refused at parse time, so no value exists to report.
		{"unpaired surrogate escape", []byte(`{"entity": "\ud800"}`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out string
			withStdin(t, tc.input, func() {
				out = captureStdout(t, func() { RunStdinJSON(schema) })
			})

			values := assertEveryFindingCarriesAnIntent(t, "stdin", out)
			if len(values) != 1 {
				t.Fatalf("stdin mode reports exactly one finding, got %d; document was:\n%s", len(values), out)
			}
			got := values[0] != "null"
			if got != tc.wantIntent {
				t.Errorf("carried intent = %v, want %v; document was:\n%s", got, tc.wantIntent, out)
			}
		})
	}
}

// --------------------------------------------------------------------------- //
// the shape invariant itself
// --------------------------------------------------------------------------- //

// `intent` sits in the same POSITION in every mode's findings — last, after
// `errors`.
//
// Asserted as order of appearance rather than membership, for the reason
// assertJSONOrder exists: a document that emitted the key in a different slot
// decodes identically and is still a different document to anyone diffing two
// reports.
func TestIntentKey_keyOrderIsIdenticalInEveryMode(t *testing.T) {
	schema := repoSchema(t)
	wantOrder := []string{`"file"`, `"line"`, `"ok"`, `"kind"`, `"errors"`, `"intent"`}

	var stdinOut string
	withStdin(t, []byte(`{"entity": "Order"}`), func() {
		stdinOut = captureStdout(t, func() { RunStdinJSON(schema) })
	})
	sourceOut, _ := runSourceJSON(t, []string{sourceFixture(t, "examples/sources/order_spec.rb")}, schema)
	adopterOut := captureStdout(t, func() {
		RunAdopterJSON([]string{sourceFixture(t, "examples/unit-order-total.json")}, schema)
	})

	for _, tc := range []struct{ mode, out string }{
		{"stdin", stdinOut},
		{"source", sourceOut},
		{"adopter", adopterOut},
	} {
		t.Run(tc.mode, func(t *testing.T) { assertJSONOrder(t, tc.out, wantOrder) })
	}
}

// Whatever the document says, it must itself be parseable — by THIS parser,
// which is the strictest reader the ecosystem has (PROTOCOL.md §1.1).
//
// This is the guard against the failure mode the `intent` key introduces and
// no other key can: every other value in the document is composed from the
// port's own literals, while this one is ARBITRARY AUTHOR INPUT echoed back
// out. An encoder that mangled it would produce a document that still looks
// right in a terminal and cannot be consumed.
func TestIntentKey_documentStaysParseableWithAnEchoedPayload(t *testing.T) {
	schema := repoSchema(t)

	// Characters that are the usual suspects for an encoder bug: the two
	// mandatory escapes, a control character, the HTML three, non-ASCII, and an
	// astral character written as a surrogate PAIR (which §1.1(a) allows).
	payload := `{"entity": "a\"b\\c\td", "action": "<x>&y", "behavior": "café — \ud83d\ude80", "layer": "unit"}`

	var out string
	withStdin(t, []byte(payload), func() {
		out = captureStdout(t, func() { RunStdinJSON(schema) })
	})

	if _, err := DecodeJSONString(strings.TrimSpace(out)); err != nil {
		t.Fatalf("the emitted document does not parse: %v\ndocument was:\n%s", err, out)
	}
}

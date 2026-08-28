package main

// Tests for renderJSONValue — the `intent` key's encoder.
//
// WHY THIS NEEDS ITS OWN FILE. The mode tests exercise whole documents, but the
// SHIPPED SCHEMA constrains what a run can produce to almost nothing: every
// `--source` payload that survives validation is a flat object of four strings,
// so a run over the repo's fixtures never renders an array, a nested object, a
// number, a boolean or an empty container — and a renderer that got all five
// wrong would still be byte-identical on the corpus. The `intent` key carries a
// SCHEMA-REJECTED payload too, which is exactly how those shapes reach the
// document, so they need pinning here rather than through the corpus.
//
// THESE EXPECTATIONS ARE DERIVED FROM THE PARSER, NOT FROM CPYTHON. An earlier
// revision of this file transcribed `json.dumps(value, indent=2)` output,
// because the port was then held against a Python reference. SPGD-403 deleted
// that reference and made PROTOCOL.md §1.1 normative, so the oracle is gone and
// three of its answers are now WRONG here:
//
//	`Infinity`/`NaN`   §1.1(b) refuses those literals, so no accepted payload
//	                   can hold one — and emitting one would make THIS document
//	                   invalid JSON, which is the same rule pointed inward.
//	`"caf\u00e9"`      EncodeJSONString does not escape non-ASCII (RFC 8259
//	                   §8.1 makes UTF-8 the encoding); `ensure_ascii=True` was
//	                   CPython's default, not a protocol rule.
//	`"\ud800Or"`       §1.1(a) refuses an unpaired surrogate at PARSE time, so
//	                   it can no longer reach a renderer at all.
//
// So the values under test are DECODED FROM SOURCE TEXT wherever a decoder can
// produce them, rather than hand-built as structs. A hand-built Number can carry
// a Raw the parser would never emit — `Number{Float: 1}` with an empty Raw
// renders as nothing at all — and a test built on one asserts the behaviour of a
// value that cannot occur while missing the one that can.

import (
	"math/big"
	"strings"
	"testing"
)

func obj(pairs ...interface{}) *Object {
	o := NewObject()
	for i := 0; i < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func intNum(raw string) Number {
	n, _ := new(big.Int).SetString(raw, 10)
	return Number{Raw: raw, IsInt: true, Int: n}
}

// decoded parses `text` the way a real payload is parsed. Using the parser
// rather than a literal struct is what keeps these tests about values that can
// actually reach the renderer.
func decoded(t *testing.T, text string) Value {
	t.Helper()
	v, err := DecodeJSONString(text)
	if err != nil {
		t.Fatalf("fixture %s does not parse, so it cannot reach the renderer: %v", text, err)
	}
	return v
}

// TestRenderJSONValue_scalars pins the leaf spellings.
func TestRenderJSONValue_scalars(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value Value
		want  string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"string", "hi", `"hi"`},
		{"empty string", "", `""`},
		{"int", intNum("42"), "42"},
		{"negative int", intNum("-7"), "-7"},
		// RFC 8259 §6 sets no limit on a number's magnitude, and Number keeps
		// the integer view in a big.Int so this is not rounded to a float64.
		{"big int", intNum("123456789012345678901234567890"), "123456789012345678901234567890"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderJSONValue(tc.value, 6); got != tc.want {
				t.Errorf("renderJSONValue(%v) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

// A number is echoed as the author WROTE it, which is a correctness property
// and not a formatting preference.
//
// The load-bearing case is `1e400`. It is a well-formed §6 number that no
// float64 can hold, newNumber deliberately accepts it (a grammatical document
// is not malformed just because IEEE-754 saturates), and it therefore decodes
// with Float == +Inf. A renderer that printed the float would emit the literal
// `Infinity` — which PROTOCOL.md §1.1(b) says is not JSON, in the very document
// this tool asks other people to parse. The rest of the table is the same rule
// with cheaper stakes: `1e2` reported as `100` tells the reader about a value
// that is not in their file.
func TestRenderJSONValue_numbersAreEchoedFromTheirLiteral(t *testing.T) {
	for _, tc := range []struct{ literal, want string }{
		{"1", "1"},
		{"1.0", "1.0"},     // NOT normalised to 1
		{"1e2", "1e2"},     // NOT expanded to 100
		{"1E2", "1E2"},     // the author's own spelling of the exponent
		{"1e400", "1e400"}, // overflows to +Inf; must NOT render as Infinity
		{"-0", "-0"},
		{"0.30000000000000004", "0.30000000000000004"},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			got := renderJSONValue(decoded(t, tc.literal), 6)
			if got != tc.want {
				t.Errorf("renderJSONValue(%s) = %s, want %s", tc.literal, got, tc.want)
			}
			// Whatever it rendered must itself be a JSON number this parser
			// accepts. That is the invariant the Infinity case would break, and
			// asserting the spelling alone would not catch a future encoder
			// that emitted something unparseable in some other way.
			if _, err := DecodeJSONString(got); err != nil {
				t.Errorf("rendered %s, which this parser refuses: %v", got, err)
			}
		})
	}
}

// Non-ASCII text survives as itself, and the three characters encoding/json
// escapes for HTML do not get mangled.
//
// This is the rule that would silently regress if someone swapped
// EncodeJSONString for encoding/json: `<`, `>` and `&` would come back as
// `\u003c`, `\u003e` and `\u0026`, which still parses to the same string and so
// would pass any test that compared parsed values rather than bytes.
func TestRenderJSONValue_stringsAreNotEscapedBeyondRFC8259(t *testing.T) {
	for _, tc := range []struct{ name, literal, want string }{
		{"non-ascii stays literal", `"café"`, `"café"`},
		{"the same text written as an escape decodes to it", `"caf\u00e9"`, `"café"`},
		{"astral character stays one character", `"\ud83d\ude00"`, `"😀"`},
		{"html chars are not escaped", `"<a>&b"`, `"<a>&b"`},
		{"quote and backslash are escaped", `"a\"b\\c"`, `"a\"b\\c"`},
		{"control characters use the short escapes", `"a\nb\tc"`, `"a\nb\tc"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderJSONValue(decoded(t, tc.literal), 6); got != tc.want {
				t.Errorf("renderJSONValue(%s) = %s, want %s", tc.literal, got, tc.want)
			}
		})
	}
}

// §1.1(a) refuses an unpaired surrogate escape, so the renderer is never asked
// to spell one. This is the ticket's original motivating payload, kept as an
// assertion that the refusal happens EARLIER than rendering rather than deleted
// — the guarantee is "no such value reaches the document", and the way to state
// that is to show the value cannot be built.
func TestRenderJSONValue_unpairedSurrogateNeverReachesTheRenderer(t *testing.T) {
	for _, literal := range []string{`"\ud800"`, `"\udc00"`, `"\ud800Or"`, `"\ud800\ud800"`} {
		if _, err := DecodeJSONString(literal); err == nil {
			t.Errorf("%s parsed; §1.1(a) requires it to be refused before anything renders it", literal)
		}
	}
}

// TestRenderJSONValue_emptyContainersStayOnOneLine is its own test because it is
// the rule a hand-written renderer drops first. A renderer that always expands
// emits "{\n\n      }", which is not merely ugly — it is a parse error.
func TestRenderJSONValue_emptyContainersStayOnOneLine(t *testing.T) {
	if got := renderJSONValue(NewObject(), 6); got != "{}" {
		t.Errorf("empty object rendered %q, want %q", got, "{}")
	}
	if got := renderJSONValue([]Value{}, 6); got != "[]" {
		t.Errorf("empty array rendered %q, want %q", got, "[]")
	}
}

// TestRenderJSONValue_nesting pins the indentation arithmetic.
//
// The value starts on a line already indented to `indent`, so its members are at
// indent+2 and its closing delimiter back at indent — which is why the first
// line carries no leading spaces (the caller has written `"intent": ` there) and
// the last one carries exactly `indent`. Getting that off by one produces a
// document that still parses and is still readable, which is precisely why it
// needs an assertion rather than an eyeball.
func TestRenderJSONValue_nesting(t *testing.T) {
	value := obj(
		"entity", "Order",
		"tags", []Value{"a", "b"},
		"meta", obj("depth", intNum("2"), "empty", NewObject()),
	)

	want := strings.Join([]string{
		`{`,
		`        "entity": "Order",`,
		`        "tags": [`,
		`          "a",`,
		`          "b"`,
		`        ],`,
		`        "meta": {`,
		`          "depth": 2,`,
		`          "empty": {}`,
		`        }`,
		`      }`,
	}, "\n")

	if got := renderJSONValue(value, 6); got != want {
		t.Errorf("renderJSONValue nesting:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Key order is the document's, not Go's. A map[string]any port would randomize
// this per run — passing sometimes and failing other times — which is the whole
// reason Object exists (see jsonvalue.go). Asserted on a key set whose sorted
// order differs from its insertion order, so an implementation that sorted would
// fail rather than agree by luck.
func TestRenderJSONValue_preservesDocumentKeyOrder(t *testing.T) {
	value := obj("zeta", intNum("1"), "alpha", intNum("2"), "mu", intNum("3"))

	want := strings.Join([]string{
		`{`,
		`        "zeta": 1,`,
		`        "alpha": 2,`,
		`        "mu": 3`,
		`      }`,
	}, "\n")

	if got := renderJSONValue(value, 6); got != want {
		t.Errorf("key order:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// The intent of a finding with no payload is nil, and nil renders `null` — not
// `{}`, and not an omitted key. An omitted key is the failure the all-modes
// constraint exists to prevent; `{}` would be a claim that the annotation said
// nothing, which is a different and worse lie than saying nothing was parsed.
func TestJSONFinding_nilIntentRendersNull(t *testing.T) {
	out := renderFindings([]JSONFinding{{
		File: "-", OK: false, Kind: KindParse, Errors: []string{"boom"},
	}})

	if !strings.Contains(out, `"intent": null`) {
		t.Errorf("a payload-less finding should carry `\"intent\": null`; got:\n%s", out)
	}
}

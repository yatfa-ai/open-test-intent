package main

// Conformance to PROTOCOL.md §1.1 — the accepted JSON language.
//
// §1.1 resolves the three points RFC 8259 leaves to the implementation. This
// file is where the binary is held to that resolution, and it is the only place
// that can hold it: the fixture corpus grades ACCEPT-vs-REJECT, and two of the
// three classes cannot be graded that way.
//
// WHY THE CORPUS IS NOT ENOUGH, stated plainly because it is the trap here.
// schemas/open-test-intent.v1.json sets `additionalProperties: false` and admits
// only strings and arrays of strings, so a non-finite number and a
// hundred-deep nest have no schema-legal slot to occupy: remove §1.1(b) or
// §1.1(c) entirely and examples/invalid/non-finite-number.json and
// examples/invalid/nesting-too-deep.json are still rejected — by the SCHEMA,
// one step later. A test asserting only "these fixtures fail" would stay green
// with the rules deleted, which is a check that verifies nothing.
//
// What separates the two is the KIND of failure and the DIAGNOSTIC. A §1.1
// refusal is a `parse` failure naming its clause; a schema refusal is a
// `schema` failure naming a property. So that is what is asserted.
//
// §1.1(a) is the exception: an unpaired surrogate escape lives inside a string,
// which IS a schema-legal slot, so that fixture moves a real verdict and the
// corpus grades it on its own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func conformanceSchema(t *testing.T) *Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "open-test-intent.v1.json"))
	if err != nil {
		t.Fatalf("reading the canonical schema: %v", err)
	}
	return mustSchema(t, string(data))
}

// The three fixtures under examples/invalid/ that exist for §1.1 are refused at
// PARSE time, each naming the clause it violates.
//
// Reading them off disk rather than restating their bytes here is deliberate:
// the fixture is the artifact a reader inspects and an adopter copies, so the
// assertion has to be about the fixture and not about a copy of it that can
// drift. A missing file is a failure, not a skip — a corpus that lost its §1.1
// fixtures must not read as green.
func TestProtocolSection11FixturesAreRefusedAtParseTime(t *testing.T) {
	schema := conformanceSchema(t)

	cases := []struct {
		fixture string
		clause  string
	}{
		{"unpaired-surrogate-escape.json", "PROTOCOL.md §1.1(a)"},
		{"non-finite-number.json", "PROTOCOL.md §1.1(b)"},
		{"nesting-too-deep.json", "PROTOCOL.md §1.1(c)"},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", "invalid", tc.fixture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("the §1.1 fixture is missing: %v", err)
			}

			valid, _, parseError, kind := CheckJSONBytes(data, schema)
			if valid {
				t.Fatalf("%s validated; §1.1 requires it to be refused", tc.fixture)
			}
			// THE LOAD-BEARING ASSERTION. `kind` is what distinguishes "refused
			// by §1.1" from "parsed fine, then refused by the schema", and the
			// latter is what happens to two of these three if the rule is
			// removed. Asserting only rejection would pass either way.
			if kind != KindParse {
				t.Fatalf("%s failed with kind %q, want %q — it was refused by "+
					"something other than the parser, so §1.1 may not be in force",
					tc.fixture, kind, KindParse)
			}
			if !strings.Contains(parseError, tc.clause) {
				t.Errorf("%s: diagnostic %q does not cite %s",
					tc.fixture, parseError, tc.clause)
			}
		})
	}
}

// §1.1(a) — surrogate escapes must be paired.
func TestSection11a_SurrogateEscapesMustBePaired(t *testing.T) {
	accepted := []struct {
		doc  string
		want string
	}{
		{`"\ud83d\ude80"`, "\U0001F680"},       // a well-formed pair: one astral character
		{`"\udbff\udfff"`, "\U0010FFFF"},       // the top of the range
		{`"\ud83d\ude80 ok"`, "\U0001F680 ok"}, // a pair with text after it
		{`"A"`, "A"},                           // an ordinary escape is untouched
	}
	for _, tc := range accepted {
		got, err := DecodeJSONString(tc.doc)
		if err != nil {
			t.Errorf("DecodeJSONString(%q): unexpected error %v", tc.doc, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DecodeJSONString(%q) = %q, want %q", tc.doc, got, tc.want)
		}
	}

	refused := []string{
		`"\ud800"`,        // lone high surrogate
		`"\udc00"`,        // lone low surrogate
		`"\ud800\ud800"`,  // a high surrogate followed by another high one
		`"\ud800\u0041"`,  // a high surrogate followed by a non-surrogate escape
		`"\ud800A"`,       // a high surrogate followed by a literal character
		`"\ud800 \udc00"`, // the halves of a pair, separated
		`{"\ud800": 1}`,   // in a KEY, not only in a value
		`["\udfff"]`,      // inside an array
	}
	for _, doc := range refused {
		value, err := DecodeJSONString(doc)
		if err == nil {
			t.Errorf("DecodeJSONString(%q) = %#v, want a §1.1(a) refusal", doc, value)
			continue
		}
		if !strings.Contains(err.Error(), "PROTOCOL.md §1.1(a)") {
			t.Errorf("DecodeJSONString(%q) failed with %q, want it to cite §1.1(a)", doc, err)
		}
	}

	// EVERY accepted string is representable in UTF-8. This is the property the
	// whole rest of the port relies on — CharCount, EncodeJSONString and the
	// report all assume it — and it is the property a lone surrogate breaks, so
	// it is asserted over the whole escape space rather than at the samples
	// above. A `\uXXXX` escape either produces valid UTF-8 or is refused;
	// nothing in between.
	for code := 0; code <= 0xffff; code++ {
		doc := "\"\\u" + string([]byte{
			hexDigit(code >> 12), hexDigit(code >> 8), hexDigit(code >> 4), hexDigit(code),
		}) + "\""
		got, err := DecodeJSONString(doc)
		if err != nil {
			if code < 0xd800 || code > 0xdfff {
				t.Fatalf("DecodeJSONString(%q) refused a non-surrogate escape: %v", doc, err)
			}
			continue
		}
		if code >= 0xd800 && code <= 0xdfff {
			t.Fatalf("DecodeJSONString(%q) accepted a lone surrogate", doc)
		}
		decoded, ok := got.(string)
		if !ok {
			t.Fatalf("DecodeJSONString(%q) = %#v, want a string", doc, got)
		}
		if !utf8.ValidString(decoded) {
			t.Fatalf("DecodeJSONString(%q) produced a string that is not valid UTF-8", doc)
		}
	}
}

// §1.1(b) — non-finite literals are not JSON.
func TestSection11b_NonFiniteLiteralsAreRefused(t *testing.T) {
	for _, doc := range []string{
		`NaN`, `Infinity`, `-Infinity`,
		`{"a": NaN}`, `{"a": Infinity}`, `{"a": -Infinity}`, `[NaN]`,
	} {
		value, err := DecodeJSONString(doc)
		if err == nil {
			t.Errorf("DecodeJSONString(%q) = %#v, want a §1.1(b) refusal", doc, value)
			continue
		}
		if !strings.Contains(err.Error(), "PROTOCOL.md §1.1(b)") {
			t.Errorf("DecodeJSONString(%q) failed with %q, want it to cite §1.1(b)", doc, err)
		}
	}

	// A number LARGER than float64 can hold is a different thing and stays
	// accepted: RFC 8259 §6's grammar contains it, and refusing it here would
	// narrow the language rather than state it. The distinction is easy to lose
	// in a "reject infinities" change, because the decoded value is ±Inf either
	// way.
	if _, err := DecodeJSONString(`{"a": 1e400}`); err != nil {
		t.Errorf("1e400 is a well-formed JSON number and must be accepted: %v", err)
	}

	// Identifiers that merely START like one of the three must not be reported
	// as non-finite — the diagnostic would send the reader looking for a number
	// they did not write.
	if _, err := DecodeJSONString(`Nan`); err == nil {
		t.Error(`"Nan" parsed`)
	} else if strings.Contains(err.Error(), "§1.1(b)") {
		t.Errorf(`"Nan" was blamed on §1.1(b): %v`, err)
	}
}

// §1.1(c) — the nesting limit is 100, and the boundary is where a limit is
// wrong if it is wrong at all.
func TestSection11c_NestingLimit(t *testing.T) {
	nest := func(depth int) string {
		return strings.Repeat("[", depth) + strings.Repeat("]", depth)
	}

	if _, err := DecodeJSONString(nest(MaxNestingDepth)); err != nil {
		t.Errorf("%d levels is at the limit and must be accepted: %v", MaxNestingDepth, err)
	}
	value, err := DecodeJSONString(nest(MaxNestingDepth + 1))
	if err == nil {
		t.Fatalf("%d levels parsed to %#v, want a §1.1(c) refusal", MaxNestingDepth+1, value)
	}
	if !strings.Contains(err.Error(), "PROTOCOL.md §1.1(c)") {
		t.Errorf("over-deep document failed with %q, want it to cite §1.1(c)", err)
	}

	// Objects count toward the same limit as arrays, and a mixture of the two
	// counts as the sum. A limit applied to only one container type is a limit
	// any document can walk around.
	var b strings.Builder
	for i := 0; i < MaxNestingDepth+1; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString("1")
	b.WriteString(strings.Repeat("}", MaxNestingDepth+1))
	if _, err := DecodeJSONString(b.String()); err == nil {
		t.Errorf("%d nested OBJECTS parsed, want a §1.1(c) refusal", MaxNestingDepth+1)
	}

	// The constant and the document must agree. PROTOCOL.md publishes the
	// number, so a change here that does not change the document makes the
	// specification wrong — which is the one direction this repository does not
	// allow (PROTOCOL.md's own opening).
	spec, err := os.ReadFile(filepath.Join("..", "..", "PROTOCOL.md"))
	if err != nil {
		t.Fatalf("reading PROTOCOL.md: %v", err)
	}
	if !strings.Contains(string(spec), "more than **100** levels deep") {
		t.Errorf("PROTOCOL.md §1.1(c) no longer states a limit of %d", MaxNestingDepth)
	}
	if MaxNestingDepth != 100 {
		t.Errorf("MaxNestingDepth = %d but PROTOCOL.md §1.1(c) publishes 100", MaxNestingDepth)
	}
}

// UTF-8 is part of what a JSON text IS (§1.1, "Encoding"), so ill-formed input
// is refused rather than repaired. Repairing it is the silent failure: every
// undecodable byte becomes U+FFFD and the validator grades text nobody wrote.
func TestSection11_EncodingIsNotRepaired(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("\xff\xfe{}"),
		[]byte(`{"a": "` + "\xc3" + `"}`),         // a truncated two-byte sequence
		[]byte(`{"a": "` + "\xed\xa0\x80" + `"}`), // a surrogate encoded directly
	} {
		if _, err := DecodeJSON(data); err == nil {
			t.Errorf("DecodeJSON(%q) accepted ill-formed UTF-8", data)
		}
	}
	// A BOM is not whitespace and not a value.
	if _, err := DecodeJSON([]byte("\ufeff{}")); err == nil {
		t.Error("a leading byte-order mark was accepted")
	}
}

func hexDigit(v int) byte { return "0123456789abcdef"[v&0xf] }

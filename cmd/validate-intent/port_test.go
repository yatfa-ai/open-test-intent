package main

// Unit coverage for the port's Python-emulation layer.
//
// tests/parity/run_parity.sh is the acceptance test — it proves the whole
// binary against the Python reference. These tests cover the pieces that
// harness cannot reach with the shipped corpus: number typing (the schema
// declares no numeric types yet), the glob and path primitives at their edges,
// and the invariants that keep error *order* deterministic.

import (
	"strings"
	"testing"
)

func mustSchema(t *testing.T, raw string) *Schema {
	t.Helper()
	schema, err := CompileSchema(mustDecode(t, raw))
	if err != nil {
		t.Fatalf("CompileSchema(%s): %v", raw, err)
	}
	return schema
}

func mustDecode(t *testing.T, raw string) Value {
	t.Helper()
	value, err := DecodeOrdered([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeOrdered(%s): %v", raw, err)
	}
	return value
}

// The load-bearing invariant of the whole port: a decoded object iterates in
// document order. Everything about error ordering rests on this, and a
// map-backed implementation would pass the fixture suite only intermittently.
func TestObjectPreservesDocumentOrder(t *testing.T) {
	object, ok := mustDecode(t, `{"zzz": 1, "aaa": 2, "mmm": 3, "bbb": 4}`).(*Object)
	if !ok {
		t.Fatal("expected an *Object")
	}
	want := []string{"zzz", "aaa", "mmm", "bbb"}
	if got := strings.Join(object.Keys(), ","); got != strings.Join(want, ",") {
		t.Errorf("Keys() = %s, want %s", got, strings.Join(want, ","))
	}
}

// Decoding the same keys twice, in opposite orders, must give opposite key
// orders — not merely "some stable order". A sorted implementation would pass
// the previous test and fail this one.
func TestObjectOrderTracksTheDocument(t *testing.T) {
	forward := mustDecode(t, `{"b": 1, "a": 2}`).(*Object)
	reversed := mustDecode(t, `{"a": 2, "b": 1}`).(*Object)
	if strings.Join(forward.Keys(), ",") != "b,a" {
		t.Errorf("forward Keys() = %v, want [b a]", forward.Keys())
	}
	if strings.Join(reversed.Keys(), ",") != "a,b" {
		t.Errorf("reversed Keys() = %v, want [a b]", reversed.Keys())
	}
}

// A repeated key takes the later value but keeps the earlier position, which is
// what CPython's dict does.
func TestObjectDuplicateKeyKeepsFirstPosition(t *testing.T) {
	object := mustDecode(t, `{"a": 1, "b": 2, "a": 3}`).(*Object)
	if got := strings.Join(object.Keys(), ","); got != "a,b" {
		t.Errorf("Keys() = %s, want a,b", got)
	}
	value, _ := object.Get("a")
	if got := PyRepr(value); got != "3" {
		t.Errorf(`Get("a") = %s, want 3`, got)
	}
}

// Go's encoding/json decodes every number to float64 by default, which would
// collapse the integer/number distinction `_type_matches` draws. UseNumber plus
// the literal's own grammar preserves it.
func TestNumberTyping(t *testing.T) {
	cases := []struct {
		raw   string
		isInt bool
	}{
		{"1", true},
		{"-7", true},
		{"0", true},
		{"12345678901234567890123456789", true},
		{"1.0", false},
		{"1e2", false},
		{"1E2", false},
		{"-2.5", false},
	}
	for _, tc := range cases {
		number, ok := mustDecode(t, tc.raw).(Number)
		if !ok {
			t.Fatalf("%s did not decode to a Number", tc.raw)
		}
		if number.IsInt != tc.isInt {
			t.Errorf("%s: IsInt = %v, want %v", tc.raw, number.IsInt, tc.isInt)
		}
	}
	integerSchema := mustSchema(t, `{"type": "integer"}`)
	if errs := integerSchema.Validate(mustDecode(t, "1")); len(errs) != 0 {
		t.Errorf("1 against type integer: %v", errs)
	}
	if errs := integerSchema.Validate(mustDecode(t, "1.5")); len(errs) != 1 {
		t.Errorf("1.5 against type integer: %v, want one error", errs)
	}
	// bool is a distinct Go type, so it is excluded from integer/number without
	// the special case the Python reference needs.
	if errs := integerSchema.Validate(mustDecode(t, "true")); len(errs) != 1 {
		t.Errorf("true against type integer: %v, want one error", errs)
	}
}

func TestDecodeRejectsTrailingData(t *testing.T) {
	for _, raw := range []string{`{} {}`, `1 2`, `{"a": 1} trailing`} {
		if _, err := DecodeOrdered([]byte(raw)); err == nil {
			t.Errorf("DecodeOrdered(%s) succeeded, want an error", raw)
		}
	}
}

// Python's `==` spans the numeric tower: True == 1 and 1 == 1.0.
func TestPyEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`1`, `1.0`, true},
		{`true`, `1`, true},
		{`false`, `0`, true},
		{`true`, `2`, false},
		{`"1"`, `1`, false},
		{`null`, `null`, true},
		{`null`, `0`, false},
		{`["a", 1]`, `["a", 1.0]`, true},
		{`{"a": 1, "b": 2}`, `{"b": 2, "a": 1}`, true},
		{`{"a": 1}`, `{"a": 2}`, false},
	}
	for _, tc := range cases {
		if got := pyEqual(mustDecode(t, tc.a), mustDecode(t, tc.b)); got != tc.want {
			t.Errorf("pyEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// fnmatch, at the points Go's own matcher disagrees with Python's.
func TestFnmatch(t *testing.T) {
	backslash := string(rune(92))
	cases := []struct {
		name, pattern string
		want          bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "a?c", true},
		{"ac", "a?c", false},
		{"abc", "a*", true},
		{"abc", "*c", true},
		{"abc", "*", true},
		{"abc", "**", true},
		{"abc", "a**c", true},
		{"abc", "*b*", true},
		{"", "*", true},
		{"", "?", false},
		// fnmatch itself has no opinion about dotfiles — the hidden-file rule
		// lives in the directory walk, exactly as in Python's glob.
		{".hidden", "*", true},
		// Character classes: Python spells negation "!", not "^".
		{"ab", "a[bc]", true},
		{"ad", "a[bc]", false},
		{"ad", "a[!bc]", true},
		{"ab", "a[!bc]", false},
		{"a^", "a[!bc]", true},
		{"am", "a[a-z]", true},
		{"aM", "a[a-z]", false},
		{"a5", "a[a-z0-9]", true},
		// A "]" in first position is a member, not the terminator.
		{"a]", "a[]]", true},
		// A leading or trailing "-" is a literal.
		{"a-", "a[x-]", true},
		{"a-", "a[-x]", true},
		// An unterminated "[" is a literal "["; Go's filepath.Match would
		// reject the pattern outright.
		{"a[b", "a[b", true},
		{"ab", "a[b", false},
		{"x[unclosed", "x[unclosed", true},
		// A backslash is a literal, not an escape.
		{"a" + backslash + "b", "a" + backslash + "b", true},
		{"ab", "a" + backslash + "b", false},
	}
	for _, tc := range cases {
		if got := fnmatch(tc.name, tc.pattern); got != tc.want {
			t.Errorf("fnmatch(%q, %q) = %v, want %v", tc.name, tc.pattern, got, tc.want)
		}
	}
}

// Golden values taken from Python's os.path.split — the head keeps its trailing
// separators only when it is nothing but separators.
func TestPySplit(t *testing.T) {
	cases := []struct{ path, head, tail string }{
		{"examples/x.json", "examples", "x.json"},
		{"examples//x.json", "examples", "x.json"},
		{"x.json", "", "x.json"},
		{"/a/b", "/a", "b"},
		{"/b", "/", "b"},
		{"/", "/", ""},
		{"examples/", "examples", ""},
		{"//", "//", ""},
		{"", "", ""},
		{"a//b//c", "a//b", "c"},
	}
	for _, tc := range cases {
		head, tail := pySplit(tc.path)
		if head != tc.head || tail != tc.tail {
			t.Errorf("pySplit(%q) = (%q, %q), want (%q, %q)",
				tc.path, head, tail, tc.head, tc.tail)
		}
	}
}

func TestPyJoin(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "x", "x"},
		{"a", "b", "a/b"},
		{"a/", "b", "a/b"},
		{"a", "/b", "/b"},
		{"", "/b", "/b"},
		{"//", "x", "//x"},
	}
	for _, tc := range cases {
		if got := pyJoin(tc.a, tc.b); got != tc.want {
			t.Errorf("pyJoin(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// Only a whole component "**" is recursive in Python; "a**b" is an ordinary
// wildcard the port handles, so refusing it would be over-broad.
func TestHasRecursiveComponent(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"examples/**/*.json", true},
		{"**", true},
		{"**/x.json", true},
		{"examples/**", true},
		{"examples/*.json", false},
		{"examples/a**b/*.json", false},
		{"a**b", false},
	}
	for _, tc := range cases {
		if got := hasRecursiveComponent(tc.pattern); got != tc.want {
			t.Errorf("hasRecursiveComponent(%q) = %v, want %v", tc.pattern, got, tc.want)
		}
	}
}

// The message contract, pinned at the unit level as well as differentially.
func TestValidateMessages(t *testing.T) {
	schema := mustSchema(t, `{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"layer": {"type": "string", "enum": ["unit", "integration"]},
			"count": {"type": "number", "minimum": 1, "maximum": 10},
			"tags":  {"type": "array", "minItems": 1, "maxItems": 2, "items": {"type": "string"}},
			"name":  {"type": "string", "minLength": 3, "maxLength": 5}
		},
		"required": ["layer"]
	}`)
	cases := []struct {
		instance string
		want     []string
	}{
		{`{"layer": "e2e"}`,
			[]string{`layer: value 'e2e' is not one of ['unit', 'integration']`}},
		{`{}`,
			[]string{`<root>: missing required property 'layer'`}},
		{`{"layer": "unit", "oops": 1}`,
			[]string{`<root>: additional property 'oops' is not allowed`}},
		{`{"layer": 1}`,
			[]string{`layer: expected type string, got number`}},
		{`{"layer": "unit", "count": 0.5}`,
			[]string{`count: value 0.5 is below minimum 1`}},
		{`{"layer": "unit", "count": 11}`,
			[]string{`count: value 11 is above maximum 10`}},
		{`{"layer": "unit", "tags": []}`,
			[]string{`tags: array has 0 item(s), minimum is 1`}},
		{`{"layer": "unit", "tags": ["a", "b", "c"]}`,
			[]string{`tags: array has 3 item(s), maximum is 2`}},
		{`{"layer": "unit", "tags": ["a", 2]}`,
			[]string{`tags[1]: expected type string, got number`}},
		{`{"layer": "unit", "name": "ab"}`,
			[]string{`name: string is 2 char(s), minLength is 3`}},
		{`{"layer": "unit", "name": "abcdef"}`,
			[]string{`name: string is 6 char(s), maxLength is 5`}},
		// Required properties are reported before the per-key walk, and the walk
		// itself follows document order.
		{`{"zzz": 1, "aaa": 2}`,
			[]string{
				`<root>: missing required property 'layer'`,
				`<root>: additional property 'zzz' is not allowed`,
				`<root>: additional property 'aaa' is not allowed`,
			}},
		// A type mismatch short-circuits the remaining keywords.
		{`{"layer": "unit", "name": 1}`,
			[]string{`name: expected type string, got number`}},
	}
	for _, tc := range cases {
		got := schema.Validate(mustDecode(t, tc.instance))
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("Validate(%s):\n got  %q\n want %q", tc.instance, got, tc.want)
		}
	}
}

// A non-object schema enforces nothing, matching the reference's early return
// for boolean schemas.
func TestValidateIgnoresNonObjectSchema(t *testing.T) {
	if errs := mustSchema(t, `true`).Validate(mustDecode(t, `{"a": 1}`)); len(errs) != 0 {
		t.Errorf("boolean schema produced %v, want no errors", errs)
	}
}

// TestUEscapeNeedsATrailingCharacter pins CPython's bound on a \uXXXX escape at
// end-of-document.
//
// The C scanner will not decode the escape unless a character FOLLOWS the four
// hex digits, so an escape that ends the document is `Invalid \uXXXX escape`
// reported at the 'u' — not `Unterminated string` at the opening quote. The
// port's bound was one character short and produced the wrong message at the
// wrong offset, which is a different errors[0] in the --json document.
//
// The parity harness sweeps this class against python3 in section 17b
// ("truncation sweep — every prefix of a \uXXXX-bearing document"); this test
// states the rule directly so it also fails in `go test`, without an oracle.
// Every expectation below was measured against CPython 3.13.5.
func TestUEscapeNeedsATrailingCharacter(t *testing.T) {
	cases := []struct {
		doc string
		msg string
		pos int
	}{
		// the escape ends the document: invalid, at the 'u'
		{`"\u0041`, `Invalid \uXXXX escape`, 2},
		// one more character present: it decodes, then the string runs out
		{`"\u0041x`, "Unterminated string starting at", 0},
		// the same boundary nested, so the offset is not an artefact of 0
		{`{"a":"\u0041`, `Invalid \uXXXX escape`, 7},
		{`[1,"\u0041`, `Invalid \uXXXX escape`, 5},
		// the LOW half of a surrogate pair carries the bound independently:
		// the high half decoded fine, the second one has no trailing character
		{`"\ud800\udc00`, `Invalid \uXXXX escape`, 8},
		// a high surrogate that does not combine still reaches unterminated
		{`"\ud800x`, "Unterminated string starting at", 0},
		// short escapes were already correct; the corrected bound keeps them so
		{`"\u004`, `Invalid \uXXXX escape`, 2},
		{`"\u`, `Invalid \uXXXX escape`, 2},
		// non-hex digits are the other route to the same message and offset
		{`"\uZZZZ`, `Invalid \uXXXX escape`, 2},
		// and a complete document must still parse
		{`"\u0041"`, "", 0},
	}

	for _, tc := range cases {
		value, err := DecodeOrderedString(tc.doc)
		if tc.msg == "" {
			if err != nil {
				t.Errorf("DecodeOrderedString(%q): unexpected error %v", tc.doc, err)
			} else if value != "A" {
				t.Errorf("DecodeOrderedString(%q) = %#v, want %q", tc.doc, value, "A")
			}
			continue
		}
		if err == nil {
			t.Errorf("DecodeOrderedString(%q): want error %q, got value %#v", tc.doc, tc.msg, value)
			continue
		}
		perr, ok := err.(*PyJSONError)
		if !ok {
			t.Errorf("DecodeOrderedString(%q): want *PyJSONError, got %T", tc.doc, err)
			continue
		}
		if perr.Msg != tc.msg || perr.Pos != tc.pos {
			t.Errorf("DecodeOrderedString(%q) = %q @%d, want %q @%d",
				tc.doc, perr.Msg, perr.Pos, tc.msg, tc.pos)
		}
	}
}

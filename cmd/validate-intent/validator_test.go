package main

// Unit coverage for the pieces the fixture corpus cannot reach.
//
// The self-test over examples/ is the acceptance test: it grades the binary
// against PROTOCOL.md, the schema and the shipped fixtures. What it CANNOT
// reach is anything the shipped schema does not declare (number typing, the
// numeric and array bounds), the path primitives at their edges, and the
// invariants that keep error ORDER deterministic — because the shipped corpus
// of valid and invalid fixtures exercises none of them. Those are here.

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
	value, err := DecodeJSON([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeJSON(%s): %v", raw, err)
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

// A repeated key takes the later value but keeps the earlier position —
// PROTOCOL.md §1.1, "Duplicate names".
func TestObjectDuplicateKeyKeepsFirstPosition(t *testing.T) {
	object := mustDecode(t, `{"a": 1, "b": 2, "a": 3}`).(*Object)
	if got := strings.Join(object.Keys(), ","); got != "a,b" {
		t.Errorf("Keys() = %s, want a,b", got)
	}
	value, _ := object.Get("a")
	if got := RenderValue(value); got != "3" {
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
	// bool is a distinct type here, so it is excluded from integer/number for
	// free — no "and not a boolean" clause to forget.
	if errs := integerSchema.Validate(mustDecode(t, "true")); len(errs) != 1 {
		t.Errorf("true against type integer: %v, want one error", errs)
	}
}

func TestDecodeRejectsTrailingData(t *testing.T) {
	for _, raw := range []string{`{} {}`, `1 2`, `{"a": 1} trailing`} {
		if _, err := DecodeJSON([]byte(raw)); err == nil {
			t.Errorf("DecodeJSON(%s) succeeded, want an error", raw)
		}
	}
}

// Enum membership: numbers compare by VALUE, so 1 and 1.0 are the same member,
// and a boolean is not a number.
func TestValuesEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`1`, `1.0`, true},
		{`true`, `true`, true},
		{`true`, `false`, false},
		{`true`, `1`, false},
		{`false`, `0`, false},
		{`"1"`, `1`, false},
		{`null`, `null`, true},
		{`null`, `0`, false},
		{`["a", 1]`, `["a", 1.0]`, true},
		{`{"a": 1, "b": 2}`, `{"b": 2, "a": 1}`, true},
		{`{"a": 1}`, `{"a": 2}`, false},
	}
	for _, tc := range cases {
		if got := valuesEqual(mustDecode(t, tc.a), mustDecode(t, tc.b)); got != tc.want {
			t.Errorf("valuesEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// The matcher, at the points the standard library's own would disagree.
func TestMatchName(t *testing.T) {
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
		// lives in the directory walk, not in the matcher.
		{".hidden", "*", true},
		// Character classes spell negation "!", not "^".
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
		if got := matchName(tc.name, tc.pattern); got != tc.want {
			t.Errorf("matchName(%q, %q) = %v, want %v", tc.name, tc.pattern, got, tc.want)
		}
	}
}

// splitPath: the head keeps its trailing separators only when it is nothing but
// separators.
func TestSplitPath(t *testing.T) {
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
		head, tail := splitPath(tc.path)
		if head != tc.head || tail != tc.tail {
			t.Errorf("splitPath(%q) = (%q, %q), want (%q, %q)",
				tc.path, head, tail, tc.head, tc.tail)
		}
	}
}

func TestJoinPath(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "x", "x"},
		{"a", "b", "a/b"},
		{"a/", "b", "a/b"},
		{"a", "/b", "/b"},
		{"", "/b", "/b"},
		{"//", "x", "//x"},
	}
	for _, tc := range cases {
		if got := joinPath(tc.a, tc.b); got != tc.want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// Only a WHOLE component "**" is recursive; "a**b" is an ordinary wildcard, so
// treating it as a descent would silently widen what a pattern matches.
func TestIsRecursiveComponent(t *testing.T) {
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
		if got := isRecursiveComponent(tc.pattern); got != tc.want {
			t.Errorf("isRecursiveComponent(%q) = %v, want %v", tc.pattern, got, tc.want)
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

// A boolean schema (draft-07 allows `true`/`false` in place of an object)
// enforces nothing.
func TestValidateIgnoresNonObjectSchema(t *testing.T) {
	if errs := mustSchema(t, `true`).Validate(mustDecode(t, `{"a": 1}`)); len(errs) != 0 {
		t.Errorf("boolean schema produced %v, want no errors", errs)
	}
}

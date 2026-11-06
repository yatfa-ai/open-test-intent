package main

// Unit tests for the schema `pattern` keyword: the mismatch message produced by
// Schema.Validate (validate.go) and the load-time refusal of CompileSchema
// (schemadoc.go).
//
// The shipped schema declares no `pattern`, so the self-test corpus cannot reach
// this keyword; it exists for an adopter-supplied schema, where a pattern that
// is silently skipped is the failure this repository is built to prevent. The
// mismatch message used to be asserted only by a differential test against the
// Python reference, which was deleted with that reference.

import (
	"strings"
	"testing"
)

// uncompilablePattern is a lookahead: valid in ECMA-262 but not in RE2, so regexp.Compile refuses it.
const uncompilablePattern = `(?=x)`

func TestPatternMismatchMessage(t *testing.T) {
	schema := mustSchema(t, `{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z]+$"}}}`)

	if got := schema.Validate(mustDecode(t, `{"id":"ok"}`)); len(got) != 0 {
		t.Errorf("matching value: got errors %q, want none", got)
	}

	got := schema.Validate(mustDecode(t, `{"id":"A1"}`))
	want := []string{`id: string does not match pattern '^[a-z]+$'`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("mismatching value:\n got  %q\n want %q", got, want)
	}
}

// A non-string instance is rejected by `type` alone; the pattern is not applied.
func TestPatternSkippedForNonStringInstance(t *testing.T) {
	schema := mustSchema(t, `{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z]+$"}}}`)
	got := schema.Validate(mustDecode(t, `{"id":1}`))
	want := []string{`id: expected type string, got number`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("non-string value:\n got  %q\n want %q", got, want)
	}
}

// Every position a pattern can occupy must be compiled at load time: top level,
// nested under properties, and inside an array of subschemas.
func TestCompileSchemaRefusesUncompilablePattern(t *testing.T) {
	cases := map[string]string{
		"top level":  `{"type":"string","pattern":"(?=x)"}`,
		"properties": `{"type":"object","properties":{"a":{"type":"string","pattern":"(?=x)"}}}`,
		"array":      `{"anyOf":[{"type":"integer"},{"type":"string","pattern":"(?=x)"}]}`,
		"deep":       `{"properties":{"a":{"items":{"allOf":[{"pattern":"(?=x)"}]}}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			schema, err := CompileSchema(mustDecode(t, raw))
			if err == nil {
				t.Fatalf("CompileSchema(%s) = %v, want an error", raw, schema)
			}
			if schema != nil {
				t.Errorf("CompileSchema returned a schema alongside an error")
			}
			wantPrefix := "schema pattern " + Quote(uncompilablePattern) + " cannot be compiled"
			if !strings.Contains(err.Error(), wantPrefix) {
				t.Errorf("error %q does not contain %q", err.Error(), wantPrefix)
			}
		})
	}
}

// The same valid pattern used in several places compiles once and is shared.
func TestCompileSchemaDeduplicatesPatterns(t *testing.T) {
	schema := mustSchema(t, `{"properties":{
		"a":{"type":"string","pattern":"^x$"},
		"b":{"anyOf":[{"type":"string","pattern":"^x$"}]},
		"c":{"type":"string","pattern":"^y$"}}}`)
	if len(schema.Patterns) != 2 {
		t.Errorf("Patterns has %d entries, want 2", len(schema.Patterns))
	}
	for _, p := range []string{"^x$", "^y$"} {
		if schema.Patterns[p] == nil {
			t.Errorf("pattern %q was not compiled", p)
		}
	}
}

// A present schema file whose pattern will not compile is an error, not a cue
// to fall back to the embedded copy.
func TestLoadSchemaFromUncompilablePatternDoesNotFallBack(t *testing.T) {
	path := writeSchema(t, t.TempDir(), `{"type":"string","pattern":"(?=x)"}`)
	schema, source, err := loadSchemaFrom(path)
	if err == nil {
		t.Fatalf("loadSchemaFrom: want an error, got schema %v", schema)
	}
	if schema != nil {
		t.Errorf("loadSchemaFrom returned a schema alongside an error")
	}
	if !strings.Contains(err.Error(), "cannot be compiled") {
		t.Errorf("error %q does not mention the compile failure", err.Error())
	}
	if source.Origin != path {
		t.Errorf("Origin = %q, want %q (must not be the embedded label)", source.Origin, path)
	}
}

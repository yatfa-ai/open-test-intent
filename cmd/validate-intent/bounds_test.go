package main

// Unit tests for the INCLUSIVE side of the schema bounds: minLength, maxLength,
// minimum, maximum, minItems, and additionalProperties given as a schema.
//
// Draft-07 bounds are inclusive, so a value exactly at the limit is valid.
// validator_test.go (TestValidateMessages) feeds only strictly-outside values,
// which pins the violating side of each keyword but not the boundary itself: a
// `<` → `<=` slip in Schema.validate (validate.go) would reject legitimate
// annotations in every adopter's CI and leave that test green.
//
// Mutant → example mapping (each single-line change to validate.go turns the
// named example red):
//   - minLength   `length < min`      → `<=`  : "minLength lower bound" (name "abc"), "multibyte at minLength" (é—ü), TestShippedSchemaLengthBoundaries
//   - maxLength   `length > max`      → `>=`  : "maxLength upper bound" (name "abcde")
//   - minimum     `num < min`         → `<=`  : "minimum lower bound" (count 1)
//   - maximum     `num > max`         → `>=`  : "maximum upper bound" (count 10)
//   - minItems    `len(arr) < min`    → `<=`  : "minItems lower bound" (tags ["a"])
//   - additionalProperties (schema) branch disabled : TestAdditionalPropertiesAsSchema

import (
	"strings"
	"testing"
)

const boundsSchema = `{"type":"object","properties":{
	"count":{"type":"number","minimum":1,"maximum":10},
	"tags":{"type":"array","minItems":1},
	"name":{"type":"string","minLength":3,"maxLength":5},
	"extra":{"type":"object","additionalProperties":{"type":"string"}}}}`

func TestBoundsAreInclusive(t *testing.T) {
	schema := mustSchema(t, boundsSchema)

	valid := map[string]string{
		"minimum lower bound":   `{"count":1}`,
		"maximum upper bound":   `{"count":10}`,
		"minItems lower bound":  `{"tags":["a"]}`,
		"minLength lower bound": `{"name":"abc"}`,
		"maxLength upper bound": `{"name":"abcde"}`,
		// 3 characters but 7 bytes: a byte-count regression fails here too.
		"multibyte at minLength":                                 `{"name":"é—ü"}`,
		"additionalProperties schema accepts a conforming value": `{"extra":{"k":"v"}}`,
	}
	for name, raw := range valid {
		t.Run(name, func(t *testing.T) {
			if got := schema.Validate(mustDecode(t, raw)); len(got) != 0 {
				t.Errorf("Validate(%s) = %q, want valid", raw, got)
			}
		})
	}
}

// additionalProperties given as a schema (not a boolean) applies to every
// property the schema does not declare, and its violations carry the path.
func TestAdditionalPropertiesAsSchema(t *testing.T) {
	schema := mustSchema(t, boundsSchema)
	got := schema.Validate(mustDecode(t, `{"extra":{"k":1}}`))
	want := []string{`extra.k: expected type string, got number`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("additionalProperties as schema:\n got  %q\n want %q", got, want)
	}
}

// The shipped schema's own limits (entity/action minLength 2, behavior
// minLength 15) are accepted exactly at the boundary, loaded through the same
// path the CLI uses.
func TestShippedSchemaLengthBoundaries(t *testing.T) {
	schema, _, err := LoadSchema()
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	raw := `{"entity":"Ab","action":"Ab","behavior":"returns 402 pay","layer":"unit"}`
	if got := schema.Validate(mustDecode(t, raw)); len(got) != 0 {
		t.Errorf("Validate(%s) = %q, want valid", raw, got)
	}
	// One character short of each limit is still refused.
	short := `{"entity":"A","action":"A","behavior":"returns 402 pa","layer":"unit"}`
	got := strings.Join(schema.Validate(mustDecode(t, short)), "\n")
	for _, field := range []string{"entity", "action", "behavior"} {
		if !strings.Contains(got, field+": string is") {
			t.Errorf("Validate(%s) errors %q do not mention %s", short, got, field)
		}
	}
}

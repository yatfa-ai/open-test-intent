package main

// The draft-07 subset the OpenTestIntent schema uses.
//
// The keywords implemented here are the ones schemas/open-test-intent.v1.json
// declares, plus the near neighbours an adopter's own schema is likely to reach
// for (`pattern`, `items`, numeric and array bounds). A keyword this file does
// not know is IGNORED, which is what draft-07 requires — but note that ignoring
// a keyword means not enforcing it, so anything added to the shipped schema
// needs a branch here on the same commit.
//
// The error strings and their ORDER are part of the report a human reads, and
// the order is the document's: see Object.

import (
	"fmt"
	"strings"
)

// Machine-readable failure taxonomy — the `kind` a --json finding carries.
//
// It is carried through the check functions rather than reconstructed by the
// renderer because a parse failure and a read failure render as identical prose:
// once the result is flattened to text the distinction is unrecoverable.
const (
	KindSchema     = "schema"     // parsed fine, violated the JSON Schema
	KindExtraction = "extraction" // an @intent: token whose payload could not be captured
	KindParse      = "parse"      // a payload/document that is not parseable JSON
	KindRead       = "read"       // the input could not be read
	KindNoMatch    = "no-match"   // a path/glob argument that matched no file
)

// typeMatches reports whether value satisfies any of the draft-07 type strings.
func typeMatches(value Value, allowed []Value) bool {
	for _, entry := range allowed {
		kind, ok := entry.(string)
		if !ok {
			continue
		}
		switch kind {
		case "object":
			if _, ok := value.(*Object); ok {
				return true
			}
		case "array":
			if _, ok := value.([]Value); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "integer":
			if n, ok := value.(Number); ok && n.IsInt {
				return true
			}
		case "number":
			if _, ok := value.(Number); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return false
}

func typeOf(value Value) string {
	switch value.(type) {
	case bool:
		return "boolean"
	case string:
		return "string"
	case *Object:
		return "object"
	case []Value:
		return "array"
	case Number:
		return "number"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", value)
}

// Validate checks instance against the schema and returns the human-readable
// violations — an empty slice means valid.
func (s *Schema) Validate(instance Value) []string {
	return s.validate(instance, s.Root, "")
}

// validate is the recursive body, carrying the sub-schema and the path to the
// value being checked.
func (s *Schema) validate(instance Value, schema Value, path string) []string {
	errors := []string{}

	schemaObj, ok := schema.(*Object)
	if !ok {
		// A boolean schema (true/false) or unknown form — nothing to enforce.
		return errors
	}

	where := path
	if where == "" {
		where = "<root>"
	}

	// type ---------------------------------------------------------------- //
	if raw, present := schemaObj.Get("type"); present {
		var allowed []Value
		if single, isString := raw.(string); isString {
			allowed = []Value{single}
		} else if list, isList := raw.([]Value); isList {
			allowed = list
		}
		if !typeMatches(instance, allowed) {
			names := make([]string, 0, len(allowed))
			for _, entry := range allowed {
				names = append(names, RenderBare(entry))
			}
			errors = append(errors, fmt.Sprintf("%s: expected type %s, got %s",
				where, strings.Join(names, "|"), typeOf(instance)))
			return errors // a type mismatch makes the remaining keywords moot
		}
	}

	// enum ---------------------------------------------------------------- //
	if raw, present := schemaObj.Get("enum"); present && !containsValue(raw, instance) {
		errors = append(errors, fmt.Sprintf("%s: value %s is not one of %s",
			where, RenderValue(instance), RenderBare(raw)))
	}

	// object keywords ------------------------------------------------------ //
	if obj, isObject := instance.(*Object); isObject {
		if raw, present := schemaObj.Get("required"); present {
			if required, isList := raw.([]Value); isList {
				for _, entry := range required {
					key, isString := entry.(string)
					if !isString {
						continue
					}
					if !obj.Has(key) {
						errors = append(errors, fmt.Sprintf(
							"%s: missing required property '%s'", where, key))
					}
				}
			}
		}

		var props *Object
		if raw, present := schemaObj.Get("properties"); present {
			props, _ = raw.(*Object)
		}
		additional := Value(true)
		if raw, present := schemaObj.Get("additionalProperties"); present {
			additional = raw
		}

		// Document order, not sorted order, so the errors track the order the
		// keys appear in the file. See Object's doc comment.
		for _, name := range obj.Keys() {
			value, _ := obj.Get(name)
			child := name
			if path != "" {
				child = path + "." + name
			}
			if sub, present := props.Get(name); present {
				errors = append(errors, s.validate(value, sub, child)...)
				continue
			}
			if allow, isBool := additional.(bool); isBool && !allow {
				errors = append(errors, fmt.Sprintf(
					"%s: additional property '%s' is not allowed", where, name))
			} else if subSchema, isObject := additional.(*Object); isObject {
				errors = append(errors, s.validate(value, subSchema, child)...)
			}
		}
	}

	// array keywords ------------------------------------------------------- //
	if arr, isArray := instance.([]Value); isArray {
		if raw, present := schemaObj.Get("minItems"); present {
			if min, isNumber := raw.(Number); isNumber && float64(len(arr)) < min.Float {
				errors = append(errors, fmt.Sprintf("%s: array has %d item(s), minimum is %d",
					where, len(arr), min.AsInt()))
			}
		}
		if raw, present := schemaObj.Get("maxItems"); present {
			if max, isNumber := raw.(Number); isNumber && float64(len(arr)) > max.Float {
				errors = append(errors, fmt.Sprintf("%s: array has %d item(s), maximum is %d",
					where, len(arr), max.AsInt()))
			}
		}
		if raw, present := schemaObj.Get("items"); present {
			if items, isObject := raw.(*Object); isObject {
				for index, item := range arr {
					errors = append(errors, s.validate(item, items,
						fmt.Sprintf("%s[%d]", path, index))...)
				}
			}
		}
	}

	// string keywords ------------------------------------------------------ //
	if str, isString := instance.(string); isString {
		// Characters, not bytes: see CharCount in render.go for why the
		// difference is reachable from the shipped corpus.
		length := CharCount(str)
		if raw, present := schemaObj.Get("minLength"); present {
			if min, isNumber := raw.(Number); isNumber && float64(length) < min.Float {
				errors = append(errors, fmt.Sprintf("%s: string is %d char(s), minLength is %d",
					where, length, min.AsInt()))
			}
		}
		if raw, present := schemaObj.Get("maxLength"); present {
			if max, isNumber := raw.(Number); isNumber && float64(length) > max.Float {
				errors = append(errors, fmt.Sprintf("%s: string is %d char(s), maxLength is %d",
					where, length, max.AsInt()))
			}
		}
		if raw, present := schemaObj.Get("pattern"); present {
			if pattern, isString := raw.(string); isString {
				// Compiled once at schema-load time by CompileSchema, which
				// refuses the whole schema if a pattern will not compile. There
				// is therefore no "compile failed, skip the check" branch here
				// for a silently unenforced rule to hide in. See schemadoc.go.
				compiled, known := s.Patterns[pattern]
				if !known {
					// Unreachable: collectPatterns walks the entire schema
					// document. A miss means the tree was mutated after loading,
					// and guessing (match? non-match?) would be a silent,
					// verdict-changing answer — the failure this port refuses to
					// produce anywhere else.
					panic(fmt.Sprintf(
						"pattern %s reached validation without being compiled by CompileSchema",
						Quote(pattern)))
				}
				if !compiled.MatchString(str) {
					errors = append(errors, fmt.Sprintf("%s: string does not match pattern %s",
						where, Quote(pattern)))
				}
			}
		}
	}

	// number keywords ------------------------------------------------------ //
	// bool is a distinct type here, so `true` is excluded from these for free.
	if num, isNumber := instance.(Number); isNumber {
		if raw, present := schemaObj.Get("minimum"); present {
			if min, ok := raw.(Number); ok && num.Float < min.Float {
				errors = append(errors, fmt.Sprintf("%s: value %s is below minimum %s",
					where, RenderValue(num), RenderValue(min)))
			}
		}
		if raw, present := schemaObj.Get("maximum"); present {
			if max, ok := raw.(Number); ok && num.Float > max.Float {
				errors = append(errors, fmt.Sprintf("%s: value %s is above maximum %s",
					where, RenderValue(num), RenderValue(max)))
			}
		}
	}

	return errors
}

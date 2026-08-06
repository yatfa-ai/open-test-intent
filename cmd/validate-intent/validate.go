package main

// Port of the draft-07 subset validator at bin/validate-intent:98-247.
//
// The error *strings* and their *order* are the contract: this port is proven
// against the Python reference byte for byte by tests/parity/run_parity.sh, so
// every message here is a transcription of the reference's format string rather
// than an idiomatic Go rephrasing.

import (
	"fmt"
	"strings"
)

// Machine-readable failure taxonomy — the `kind` a --json finding will carry.
//
// --json itself is a later slice, but CheckFile already returns the kind for
// the same reason the Python reference does (bin/validate-intent:88-92): a
// parse failure and a read failure render as identical prose, so once the
// result is flattened to text the distinction is unrecoverable. Keeping it in
// the signature now means the later slice adds a renderer, not a second
// implementation that can drift.
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
				names = append(names, PyStr(entry))
			}
			errors = append(errors, fmt.Sprintf("%s: expected type %s, got %s",
				where, strings.Join(names, "|"), typeOf(instance)))
			return errors // a type mismatch makes the remaining keywords moot
		}
	}

	// enum ---------------------------------------------------------------- //
	if raw, present := schemaObj.Get("enum"); present && !pyContains(raw, instance) {
		errors = append(errors, fmt.Sprintf("%s: value %s is not one of %s",
			where, PyRepr(instance), PyStr(raw)))
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

		// Document order, not sorted order: the reference iterates
		// `instance.items()`, so the order of these errors tracks the order the
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
		// Python's len() over a str counts code points, not bytes — and pyLen,
		// unlike utf8.RuneCountInString, also counts a WTF-8 lone surrogate as
		// the single character Python holds for it. See pystr.go.
		length := pyLen(str)
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
				// Translated and compiled once at schema-load time by
				// CompileSchema, which refuses the whole schema if the pattern
				// cannot be given Python's exact meaning under RE2. There is
				// therefore no "compile failed, skip the check" branch here for
				// a divergence to disappear into. See pypattern.go.
				compiled, known := s.Patterns[pattern]
				if !known {
					// Unreachable: collectPatterns walks the entire schema
					// document. A miss means the tree was mutated after loading,
					// and guessing (match? non-match?) would be a silent,
					// verdict-changing answer — the failure this port refuses to
					// produce anywhere else.
					panic(fmt.Sprintf(
						"pattern %s reached validation without being compiled by CompileSchema",
						PyReprString(pattern)))
				}
				if !compiled.MatchString(str) {
					errors = append(errors, fmt.Sprintf("%s: string does not match pattern %s",
						where, PyReprString(pattern)))
				}
			}
		}
	}

	// number keywords ------------------------------------------------------ //
	// bool is a distinct type in Go, so it is excluded for free — the Python
	// reference has to say `and not isinstance(instance, bool)` here.
	if num, isNumber := instance.(Number); isNumber {
		if raw, present := schemaObj.Get("minimum"); present {
			if min, ok := raw.(Number); ok && num.Float < min.Float {
				errors = append(errors, fmt.Sprintf("%s: value %s is below minimum %s",
					where, PyRepr(num), PyRepr(min)))
			}
		}
		if raw, present := schemaObj.Get("maximum"); present {
			if max, ok := raw.(Number); ok && num.Float > max.Float {
				errors = append(errors, fmt.Sprintf("%s: value %s is above maximum %s",
					where, PyRepr(num), PyRepr(max)))
			}
		}
	}

	return errors
}

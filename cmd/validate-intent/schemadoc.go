package main

// The compiled form of a schema document.
//
// Two things are precomputed at load time rather than per value checked, and
// both are about failing in the right place: a schema that cannot be honoured
// must be refused when it is LOADED, not silently skipped in the middle of a
// verdict.

import (
	"fmt"
	"regexp"
)

// PatternSet maps each `pattern` string in a schema to its compiled regexp.
type PatternSet map[string]*regexp.Regexp

// Schema is a decoded schema plus everything precomputed from it.
type Schema struct {
	Root     Value
	Patterns PatternSet
}

// CompileSchema prepares a decoded schema document for validation.
//
// It compiles every `pattern` up front and refuses the whole schema if one of
// them will not compile. The alternative — compiling lazily inside Validate and
// skipping the check when it fails — is the failure this repository is built to
// prevent: the run reports a clean pass having never applied the constraint.
//
// The dialect is Go's regexp, which is RE2: the ECMA-262 syntax draft-07 names,
// minus backreferences and lookaround. A pattern using either of those does not
// compile and is refused here with the engine's own message, so the author is
// told at load time rather than being given a verdict that ignored their rule.
// The shipped schema (PROTOCOL.md §3) declares no `pattern`, so this governs
// only a schema an adopter supplies.
func CompileSchema(root Value) (*Schema, error) {
	patterns := PatternSet{}
	if err := collectPatterns(root, patterns); err != nil {
		return nil, err
	}
	return &Schema{Root: root, Patterns: patterns}, nil
}

// collectPatterns walks the whole schema document and compiles every string
// under a `pattern` key.
//
// It deliberately does not work out which of those keys are really draft-07
// `pattern` keywords and which are, say, a property named "pattern" inside a
// `default` value. Over-approximating costs at most a spurious refusal on an
// exotic schema; under-approximating would let a real pattern reach Validate
// uncompiled, which is the failure this file exists to prevent.
func collectPatterns(node Value, into PatternSet) error {
	switch typed := node.(type) {
	case *Object:
		for _, key := range typed.Keys() {
			child, _ := typed.Get(key)
			if pattern, isString := child.(string); isString && key == "pattern" {
				if _, done := into[pattern]; !done {
					compiled, err := regexp.Compile(pattern)
					if err != nil {
						return fmt.Errorf(
							"schema pattern %s cannot be compiled: %w", Quote(pattern), err)
					}
					into[pattern] = compiled
				}
			}
			if err := collectPatterns(child, into); err != nil {
				return err
			}
		}
	case []Value:
		for _, item := range typed {
			if err := collectPatterns(item, into); err != nil {
				return err
			}
		}
	}
	return nil
}

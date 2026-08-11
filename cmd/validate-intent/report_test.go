package main

// Tests for renderJSONValue — the `intent` key's encoder.
//
// WHY THIS NEEDS ITS OWN FILE, when tests/parity/run_parity.sh already compares
// whole documents against python3 byte for byte: the harness can only exercise
// what a run can produce, and the SHIPPED SCHEMA constrains that to almost
// nothing. Every `--source` payload that survives validation is a flat object of
// four strings, so a run over the repo's fixtures never renders an array, a
// nested object, a number, a boolean, an empty container or a non-finite — and a
// renderer that got all six wrong would still be byte-identical on the corpus.
//
// The harness's section 20 ("the carried intent — payload shapes the shipped
// schema cannot produce") closes that by feeding the shapes through stdin mode,
// where the schema rejects them but the payload is carried anyway. This file is
// the other half: the harness says "the two agree", these say WHICH RULE was
// meant, so a failure names the layout rule rather than reporting that two
// documents differ somewhere.
//
// The expectations are hand-written CPython output. They were produced by
// running `json.dumps(value, indent=2)` and pasting the result, not by
// describing what it ought to do — the point of a golden expectation is that it
// is transcribed rather than derived from the same understanding that wrote the
// code.

import (
	"math"
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

func floatNum(f float64) Number {
	return Number{Raw: "", IsInt: false, Float: f}
}

// TestRenderJSONValue_scalars pins the leaf spellings, including the three
// json.dumps produces that repr() does NOT. PyReprFloat is right there and
// answers `inf`, `-inf` and `nan` — correct for its own callers, and three
// documents CPython's json module would never emit.
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
		// Python ints are arbitrary precision; float64 would round this.
		{"big int", intNum("123456789012345678901234567890"), "123456789012345678901234567890"},
		{"float keeps its .0", floatNum(1), "1.0"},
		{"float", floatNum(0.1), "0.1"},
		{"exponential", floatNum(1e17), "1e+17"},
		{"infinity", floatNum(math.Inf(1)), "Infinity"},
		{"-infinity", floatNum(math.Inf(-1)), "-Infinity"},
		{"nan", floatNum(math.NaN()), "NaN"},
		// ensure_ascii=True, and the three characters encoding/json escapes and
		// json.dumps does not.
		{"non-ascii is escaped", "café", `"caf\u00e9"`},
		{"astral becomes a surrogate pair", "😀", `"\ud83d\ude00"`},
		{"html chars are not escaped", "<a>&b", `"<a>&b"`},
		// The payload that motivated the whole key: CPython decodes a lone
		// surrogate and re-emits it rather than raising.
		{"lone surrogate", "\xed\xa0\x80Or", `"\ud800Or"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderJSONValue(tc.value, 6); got != tc.want {
				t.Errorf("renderJSONValue(%v) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

// TestRenderJSONValue_emptyContainersStayOnOneLine is its own test because it is
// the rule a Go marshaller gets wrong by default and the rule a hand-written
// renderer drops first: Python indents a NON-empty container and collapses an
// empty one, so a renderer that always expands emits "{\n\n      }" where the
// oracle emits "{}".
func TestRenderJSONValue_emptyContainersStayOnOneLine(t *testing.T) {
	if got := renderJSONValue(NewObject(), 6); got != "{}" {
		t.Errorf("empty object rendered %q, want %q", got, "{}")
	}
	if got := renderJSONValue([]Value{}, 6); got != "[]" {
		t.Errorf("empty array rendered %q, want %q", got, "[]")
	}
}

// TestRenderJSONValue_nesting pins the indentation arithmetic against
// transcribed CPython output.
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

	// python3 -c 'import json; print(json.dumps({...}, indent=2))', with each
	// line after the first shifted right by the 6 columns a finding's keys sit
	// at — exactly what the enclosing document does to it.
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

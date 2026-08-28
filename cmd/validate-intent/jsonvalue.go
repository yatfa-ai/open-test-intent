package main

// The decoded value model.
//
// The validator reports one error per offending key, in the order the keys
// appear in the DOCUMENT — so a reader's eye moves down their file in step with
// the report. Go's map iteration order is deliberately randomized, so a
// `map[string]any` would emit the same set of errors in a different order on
// every run: green on some runs and red on others, over identical input.
// Everything in this file exists to make that impossible.

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Value is a decoded JSON value. The concrete types are deliberately narrow, so
// the validator's `type` keyword is a Go type switch and nothing more:
//
//	nil        JSON null
//	bool       JSON true/false
//	string     JSON string
//	Number     JSON number (int/float distinction preserved — see below)
//	[]Value    JSON array
//	*Object    JSON object (key order preserved)
//
// bool being a distinct type is load-bearing: `true` must not satisfy the
// `number` or `integer` type, and here it cannot, with nothing to remember.
type Value = interface{}

// Object is a JSON object that remembers the order its keys appeared in.
type Object struct {
	keys []string
	vals map[string]Value
}

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{vals: map[string]Value{}}
}

// Set records a key. A repeated key keeps its original POSITION and takes the
// LATER value — PROTOCOL.md §1.1, "Duplicate names".
func (o *Object) Set(key string, value Value) {
	if _, seen := o.vals[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

// Get returns the value for key and whether it was present.
func (o *Object) Get(key string) (Value, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.vals[key]
	return ok
}

// Keys returns the keys in document order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// Number is a JSON number that remembers whether it was written as an integer.
//
// A token with neither a fraction nor an exponent is an integer, so `1` is one
// while `1.0` and `1e2` are not. Collapsing all three into float64 — which is
// what a float-only decoder does — would erase the distinction the `integer`
// and `number` schema types draw. The shipped schema (PROTOCOL.md §3) declares
// no numeric types, so this is unexercised today; it is written correctly now
// so it does not become a bug on the commit that adds one.
type Number struct {
	Raw   string   // the literal exactly as it appeared in the document
	IsInt bool     // written with neither a fraction nor an exponent
	Int   *big.Int // set when IsInt; arbitrary precision, as RFC 8259 §6 allows
	Float float64  // always set; the float64 view of the literal
}

func newNumber(raw string) (Number, error) {
	n := Number{Raw: raw, IsInt: !strings.ContainsAny(raw, ".eE")}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		// A literal outside float64's range is NOT a syntax error. RFC 8259 §6
		// permits any number the grammar accepts and leaves the range to the
		// implementation; 1e400 is a well-formed JSON number that no float64
		// can hold. ParseFloat saturates to ±Inf and flags ErrRange, and
		// treating that flag as a failure would report a grammatical document
		// as malformed.
		var numErr *strconv.NumError
		if !errors.As(err, &numErr) || numErr.Err != strconv.ErrRange {
			return n, fmt.Errorf("invalid number literal %q", raw)
		}
	}
	n.Float = f
	if n.IsInt {
		i, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return n, fmt.Errorf("invalid integer literal %q", raw)
		}
		n.Int = i
	}
	return n, nil
}

// AsInt returns the value truncated toward zero, for the keyword messages that
// interpolate a schema bound as a whole number.
func (n Number) AsInt() int64 {
	if n.IsInt && n.Int != nil && n.Int.IsInt64() {
		return n.Int.Int64()
	}
	return int64(n.Float)
}

// valuesEqual reports whether two decoded JSON values are the same value.
//
// It backs the `enum` membership test. It is more general than the shipped
// schema needs — that enum holds strings and is guarded by a preceding type
// check — so that a schema growing a numeric or boolean enum does not quietly
// start answering the wrong question.
//
// Numbers compare by VALUE, so `1` and `1.0` are the same member of an enum:
// JSON has one number type and the integer/real split this port preserves is
// about the `integer` type keyword, not about identity. A boolean is NOT a
// number here, so `true` does not match an enum member of `1`.
func valuesEqual(a, b Value) bool {
	if an, aok := numericValue(a); aok {
		if bn, bok := numericValue(b); bok {
			return an == bn
		}
		return false
	}
	if _, bok := numericValue(b); bok {
		return false
	}

	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case []Value:
		bv, ok := b.([]Value)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case *Object:
		bv, ok := b.(*Object)
		if !ok || len(av.Keys()) != len(bv.Keys()) {
			return false
		}
		for _, k := range av.Keys() {
			other, present := bv.Get(k)
			if !present {
				return false
			}
			mine, _ := av.Get(k)
			if !valuesEqual(mine, other) {
				return false
			}
		}
		return true
	}
	return false
}

// numericValue exposes the float64 view of a JSON number. A bool is not one.
func numericValue(v Value) (float64, bool) {
	if n, ok := v.(Number); ok {
		return n.Float, true
	}
	return 0, false
}

// containsValue reports whether container — an `enum` array — holds instance.
func containsValue(container Value, instance Value) bool {
	items, ok := container.([]Value)
	if !ok {
		return false
	}
	for _, item := range items {
		if valuesEqual(instance, item) {
			return true
		}
	}
	return false
}

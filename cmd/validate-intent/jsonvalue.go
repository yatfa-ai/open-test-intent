package main

// Ordered JSON decoding.
//
// The Python reference walks `instance.items()`, and CPython dicts iterate in
// insertion order — so the order of the errors it reports tracks the order the
// keys appear in the *document*, not sorted order. Go's map iteration order is
// deliberately randomized, so a `map[string]any` port would emit the same set
// of errors in a different order on every run: it would pass the fixture suite
// sometimes and fail it other times. Everything in this file exists to make
// that impossible.

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Value is a decoded JSON value. The concrete types are deliberately narrow so
// a type switch can reproduce Python's isinstance() ladder exactly:
//
//	nil        JSON null
//	bool       JSON true/false
//	string     JSON string
//	Number     JSON number (int/float distinction preserved — see below)
//	[]Value    JSON array
//	*Object    JSON object (key order preserved)
//
// Note that bool is a distinct Go type, which removes the Python trap the
// reference comments call out at bin/validate-intent:107 — there, bool is a
// subclass of int and has to be excluded from number/integer by hand.
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

// Set records a key. A repeated key keeps its original *position* but takes the
// later value, which is what CPython's dict does for a duplicated JSON key.
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

// Number is a JSON number that remembers whether Python's json module would
// have decoded it as an int or a float.
//
// Python's scanner picks parse_int when the token has neither a fraction nor an
// exponent and parse_float otherwise, so `1` is an int while `1.0` and `1e2`
// are floats. Go's encoding/json collapses all three into float64 by default,
// which would erase the `integer` vs `number` distinction `_type_matches`
// draws (bin/validate-intent:108-111) and change how the value reprs. The
// shipped schema declares no numeric types, so this is unexercised today — it
// is preserved here so it does not become a bug the moment the schema grows.
type Number struct {
	Raw   string   // the literal exactly as it appeared in the document
	IsInt bool     // Python would have produced an int
	Int   *big.Int // set when IsInt (Python ints are arbitrary precision)
	Float float64  // always set; the float64 view of the literal
}

func newNumber(raw string) (Number, error) {
	n := Number{Raw: raw, IsInt: !strings.ContainsAny(raw, ".eE")}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		// A literal outside float64's range is NOT a parse failure. Python's
		// float() saturates: float("1e400") is inf and float("1e-400") is 0.0.
		// Go's ParseFloat returns exactly those same values but also flags
		// ErrRange, and treating that flag as an error made the port answer
		// "Expecting value" where python3 answered `inf` — caught by
		// TestPyJSON_handWrittenCases, not by inspection.
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

// AsInt returns the value truncated toward zero, the way Python's "%d" renders
// it.
func (n Number) AsInt() int64 {
	if n.IsInt && n.Int != nil && n.Int.IsInt64() {
		return n.Int.Int64()
	}
	return int64(n.Float)
}

// DecodeOrdered lives in pyjson.go, which parses the way json.loads does —
// error prose, offsets and all. Slice 1 backed it with encoding/json and paid
// for that with two parity exclusions (json.JSONDecodeError's wording, and the
// NaN/Infinity literals encoding/json rejects); slice 2 needed both of them
// closed for `--source`, so the decoder moved rather than being duplicated.

// pyEqual reports whether Python's `==` would consider a and b equal.
//
// This backs the `instance not in schema["enum"]` membership test. It is more
// general than the shipped schema needs (whose only enum holds strings, and is
// guarded by a preceding type check) so that a schema growing a numeric or
// boolean enum does not quietly start answering the wrong question.
func pyEqual(a, b Value) bool {
	// Python's numeric tower makes True == 1 and 1 == 1.0 true.
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
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case []Value:
		bv, ok := b.([]Value)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !pyEqual(av[i], bv[i]) {
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
			if !pyEqual(mine, other) {
				return false
			}
		}
		return true
	}
	return false
}

// numericValue exposes the float64 view of anything Python treats as a number,
// bool included.
func numericValue(v Value) (float64, bool) {
	switch t := v.(type) {
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case Number:
		return t.Float, true
	}
	return 0, false
}

// pyContains reports whether Python's `instance in container` would hold.
func pyContains(container Value, instance Value) bool {
	items, ok := container.([]Value)
	if !ok {
		return false
	}
	for _, item := range items {
		if pyEqual(instance, item) {
			return true
		}
	}
	return false
}

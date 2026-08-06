package main

// Python `repr` emulation.
//
// Several of the reference's messages interpolate `%r`, not `%s`, so Python's
// repr is part of the message contract this port has to reproduce byte for
// byte:
//
//	bin/validate-intent:162  "%s: value %r is not one of %s" % (where, instance, schema["enum"])
//	bin/validate-intent:205  "string does not match pattern %r"
//	bin/validate-intent:207  "value %r is below minimum %r"
//	bin/validate-intent:209  "value %r is above maximum %r"
//	bin/validate-intent:493  "error: no file(s) match %r" % pattern
//
// Concretely, the enum message has to come out as
//
//	layer: value 'e2e' is not one of ['unit', 'integration', 'request', 'system']
//
// — single-quoted scalars and a Python *list literal*. Go's `%v` would give
// `[unit integration request system]` and `%q` would give `["unit" ...]`;
// neither matches, so the formatting is done here by hand.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// PyRepr renders a decoded JSON value the way Python's repr() would.
func PyRepr(v Value) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		return PyReprString(t)
	case Number:
		return pyReprNumber(t)
	case []Value:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, PyRepr(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Object:
		parts := make([]string, 0, len(t.Keys()))
		for _, key := range t.Keys() {
			value, _ := t.Get(key)
			parts = append(parts, PyReprString(key)+": "+PyRepr(value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprintf("%v", v)
}

// PyStr renders a value the way Python's str() would. It differs from PyRepr
// for strings only: `"%s" % "x"` is `x` while `"%r" % "x"` is `'x'`. Containers
// still repr their elements, which is why the enum message's `%s` of a list
// comes out as a Python list literal.
func PyStr(v Value) string {
	if s, ok := v.(string); ok {
		return s
	}
	return PyRepr(v)
}

// PyReprString renders a Go string the way Python's repr() renders a str.
//
// Python prefers single quotes and switches to double quotes only when the
// string contains a single quote but no double quote.
//
// It iterates pyRunes rather than `range s`: a decoded JSON string may hold a
// lone surrogate (from a `"\ud800"` literal, which Python decodes and keeps),
// carried through this port as WTF-8. `range` over a Go string decodes those
// three bytes as three U+FFFD replacements, which repr'd as three literal
// replacement characters instead of Python's `\ud800`.
func PyReprString(s string) string {
	runes := pyRunes(s)

	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}

	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range runes {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			// Python 3 keeps printable non-ASCII characters literal, and Go's
			// unicode.IsPrint tracks str.isprintable() closely (both exclude the
			// C* and Z* categories, ASCII space excepted).
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(fmt.Sprintf(`\x%02x`, r))
		case r < 0x10000:
			b.WriteString(fmt.Sprintf(`\u%04x`, r))
		default:
			b.WriteString(fmt.Sprintf(`\U%08x`, r))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func pyReprNumber(n Number) string {
	if n.IsInt && n.Int != nil {
		// Python ints are arbitrary precision and repr as plain decimal, so
		// `-0` reprs as `0`.
		return n.Int.String()
	}
	return PyReprFloat(n.Float)
}

// PyReprFloat renders a float64 the way Python's repr() renders a float.
//
// Python emits the shortest decimal string that round-trips, then picks fixed
// or exponential notation by the position of the decimal point: exponential
// when it lands below -3 or above 16. Fixed notation always keeps a `.0` so the
// value still reads as a float; exponential notation does not (repr(1e16) is
// '1e+16', not '1.0e+16'), and the exponent is at least two digits.
//
// Go's own formatting verbs pick their notation by different rules, so the
// digits are taken from Go (which produces the same shortest round-trip digits)
// and the layout is reassembled here.
func PyReprFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}

	sign := ""
	if math.Signbit(f) {
		sign = "-"
		f = -f
	}

	// 'e' with precision -1 gives the shortest round-tripping digit string.
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	exp, err := strconv.Atoi(exponent)
	if err != nil {
		return sign + strconv.FormatFloat(f, 'g', -1, 64)
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	decpt := exp + 1 // digits before the decimal point

	if decpt < -3 || decpt > 16 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		e := decpt - 1
		esign := "+"
		if e < 0 {
			esign, e = "-", -e
		}
		return fmt.Sprintf("%s%se%s%02d", sign, out, esign, e)
	}
	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}

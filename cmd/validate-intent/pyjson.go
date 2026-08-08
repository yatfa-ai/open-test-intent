package main

// A port of CPython's `json.loads`, error prose included.
//
// WHY THIS EXISTS
// ---------------
// Slice 1 decoded with encoding/json and listed the consequence as an excluded
// parity case: check_file embeds the raw exception text
// ("could not read/parse JSON: %s" % exc, bin/validate-intent:243) and Go's
// decoder cannot reproduce json.JSONDecodeError's wording. That exclusion was
// affordable there because no shipped `examples/invalid/*.json` fixture is
// malformed — every one parses cleanly and fails on schema grounds.
//
// It is not affordable in `--source` mode. check_source_file parses the
// *normalized annotation payload* (bin/validate-intent:446) and renders any
// failure as "could not parse annotation: %s". A payload that does not parse is
// the single most likely thing a real adopter hits — it is what a typo'd
// annotation produces — so leaving it as a documented divergence would mean the
// port is byte-for-byte on everything except the case users actually meet.
// Divergence 3's own regression fixtures land here too: `{café: "x"}` and
// `{a<b>&c: "y"}` both normalize to something json.loads rejects, so SPGD-102's
// success criterion 4 cannot be met without this file.
//
// So: one decoder, written against CPython's semantics, used by every caller.
// It replaces slice 1's encoding/json-backed DecodeOrdered rather than sitting
// beside it — two decoders in one binary is two things to keep in agreement,
// and the second one always loses.
//
// WHAT IT BUYS BEYOND THE PROSE
// -----------------------------
//   - `NaN` / `Infinity` / `-Infinity` are accepted, as Python accepts them.
//     Slice 1 excluded them (harness exclusion 5) because encoding/json rejects
//     them, which made Go classify such a document as a parse failure where
//     Python reported a schema violation. Both exclusions are now retired.
//   - Object key order is preserved by construction rather than by re-walking
//     tokens.
//   - Python's int/float split falls out of the grammar (a token with neither a
//     fraction nor an exponent is an int) instead of being re-derived.
//
// HOW IT IS KNOWN TO BE RIGHT
// ---------------------------
// Not by transcription from memory: cmd/validate-intent/pyjson_fuzz_test.go
// generates tens of thousands of JSON-ish strings, feeds each to python3's
// json.loads and to this decoder, and requires the *value or the exact error
// string* to agree. Every message and offset below was first observed from
// CPython 3.13.5, then pinned by that fuzzer.
//
// Reach it with `go test ./cmd/validate-intent` — and note that nothing else
// will. tests/parity/run_parity.sh builds the binary and diffs CLI invocations;
// it does not run `go test`, and CI runs neither (the workflow is still an
// unpromoted draft in .agents/ci.yml). So that one command is the sole path to
// the oracle behind this file. The fuzzer skips itself, loudly, when python3 is
// absent rather than passing vacuously.

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// PyJSONError is the port of json.JSONDecodeError.
//
// Its rendering is the contract: `str(exc)` is what the reference interpolates
// into "could not read/parse JSON: %s" and "could not parse annotation: %s", so
// the msg, the 1-based line, the 1-based column and the 0-based character
// offset all have to match.
type PyJSONError struct {
	Msg string
	Doc []rune // the document, as code points — offsets below index into this
	Pos int
}

// Error renders the exception exactly as `str(JSONDecodeError)` does:
//
//	Expecting property name enclosed in double quotes: line 1 column 2 (char 1)
//
// lineno and colno are derived from Pos the way JSONDecodeError.__init__ does
// (json/decoder.py): count the newlines before Pos, and measure back to the last
// one. Note that only "\n" counts here — this is *not* pySplitlines' terminator
// set. json.JSONDecodeError says `doc.count('\n', 0, pos)`, and a \v or \x85 in
// a document therefore does not advance the reported line number even though
// str.splitlines() would have broken on it. The two really do disagree, and this
// port reproduces both.
func (e *PyJSONError) Error() string {
	lineno := 1
	lastNL := -1
	for i := 0; i < e.Pos && i < len(e.Doc); i++ {
		if e.Doc[i] == '\n' {
			lineno++
			lastNL = i
		}
	}
	colno := e.Pos - lastNL
	return fmt.Sprintf("%s: line %d column %d (char %d)", e.Msg, lineno, colno, e.Pos)
}

func jsonErr(doc []rune, msg string, pos int) error {
	return &PyJSONError{Msg: msg, Doc: doc, Pos: pos}
}

// Error messages, spelled exactly as CPython spells them. `Invalid \escape` and
// `Invalid \uXXXX escape` carry a literal backslash.
const (
	msgExpectingValue    = "Expecting value"
	msgExpectingProperty = "Expecting property name enclosed in double quotes"
	msgExpectingColon    = "Expecting ':' delimiter"
	msgExpectingComma    = "Expecting ',' delimiter"
	msgTrailingObject    = "Illegal trailing comma before end of object"
	msgTrailingArray     = "Illegal trailing comma before end of array"
	msgUnterminated      = "Unterminated string starting at"
	msgControlChar       = "Invalid control character at"
	msgInvalidEscape     = `Invalid \escape`
	msgInvalidUEscape    = `Invalid \uXXXX escape`
	msgExtraData         = "Extra data"
	msgBOM               = "Unexpected UTF-8 BOM (decode using utf-8-sig)"
)

// DecodeOrdered parses one JSON document the way json.loads does, preserving
// object key order.
//
// The input is decoded to code points up front because every offset CPython
// reports is an index into the decoded `str`, not into the UTF-8 bytes. A
// byte-indexed port reports the right message at the wrong column the first time
// a document contains a non-ASCII character before the error — which is exactly
// the class of "specific, confident, wrong" answer this port exists to avoid.
func DecodeOrdered(data []byte) (Value, error) {
	return DecodeOrderedString(string(data))
}

// DecodeOrderedString is DecodeOrdered over an already-decoded string. The
// annotation-payload path (check_source_file) has a str in hand, not bytes.
func DecodeOrderedString(s string) (Value, error) {
	doc := pyRunes(s)

	// json.loads checks for a BOM before anything else and refuses it, because
	// the caller was supposed to decode with utf-8-sig.
	if len(doc) > 0 && doc[0] == 0xfeff {
		return nil, jsonErr(doc, msgBOM, 0)
	}

	idx := skipJSONWhitespace(doc, 0)
	value, end, err := scanOnce(doc, idx)
	if err != nil {
		return nil, err
	}
	end = skipJSONWhitespace(doc, end)
	if end != len(doc) {
		return nil, jsonErr(doc, msgExtraData, end)
	}
	return value, nil
}

// skipJSONWhitespace advances past the whitespace json's scanner recognises.
//
// That set is exactly {space, \t, \n, \r} — json/decoder.py's
// WHITESPACE = re.compile(r'[ \t\n\r]*'). It is a third, narrower set than
// either pyIsSpace or pyLineTerminator: a \v or \f between two JSON tokens is a
// syntax error, not whitespace. Enumerated over range(0x110000): only those four
// characters let `chr(c) + "1"` parse.
func skipJSONWhitespace(doc []rune, i int) int {
	for i < len(doc) {
		switch doc[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// scanOnce parses one value starting at idx, in the same order CPython's
// scanner tries them.
func scanOnce(doc []rune, idx int) (Value, int, error) {
	if idx >= len(doc) {
		return nil, 0, jsonErr(doc, msgExpectingValue, idx)
	}
	switch doc[idx] {
	case '"':
		return scanStringLiteral(doc, idx)
	case '{':
		return parseJSONObject(doc, idx+1)
	case '[':
		return parseJSONArray(doc, idx+1)
	}
	if hasPrefixAt(doc, idx, "null") {
		return nil, idx + 4, nil
	}
	if hasPrefixAt(doc, idx, "true") {
		return true, idx + 4, nil
	}
	if hasPrefixAt(doc, idx, "false") {
		return false, idx + 5, nil
	}
	if raw, end, ok := matchJSONNumber(doc, idx); ok {
		number, err := newNumber(raw)
		if err != nil {
			// Unreachable for anything matchJSONNumber accepts; a literal too
			// large for a float64 still parses (to ±Inf), as it does in Python.
			return nil, 0, jsonErr(doc, msgExpectingValue, idx)
		}
		return number, end, nil
	}
	// The three non-standard literals json accepts by default. They sit after
	// the number match, as they do in CPython, so `-Infinity` is only reached
	// once NUMBER_RE has failed on the leading '-'.
	if hasPrefixAt(doc, idx, "NaN") {
		return nonFinite(math.NaN(), "NaN"), idx + 3, nil
	}
	if hasPrefixAt(doc, idx, "Infinity") {
		return nonFinite(math.Inf(1), "Infinity"), idx + 8, nil
	}
	if hasPrefixAt(doc, idx, "-Infinity") {
		return nonFinite(math.Inf(-1), "-Infinity"), idx + 9, nil
	}
	return nil, 0, jsonErr(doc, msgExpectingValue, idx)
}

// nonFinite builds the Number for one of json's three non-standard literals.
//
// It bypasses newNumber deliberately: that helper decides int-vs-float by
// looking for '.', 'e' or 'E' in the literal, and none of "NaN", "Infinity" or
// "-Infinity" contains one — so they would be classified as ints and then fail
// to parse as one. Python produces a float for all three (parse_constant), which
// is what makes `_type_matches` treat them as "number" and not "integer".
func nonFinite(f float64, raw string) Number {
	return Number{Raw: raw, IsInt: false, Float: f}
}

func hasPrefixAt(doc []rune, idx int, literal string) bool {
	if idx+len(literal) > len(doc) {
		return false
	}
	for i := 0; i < len(literal); i++ {
		if doc[idx+i] != rune(literal[i]) {
			return false
		}
	}
	return true
}

// parseJSONObject parses from just past the '{'.
//
// Every offset here was checked against CPython 3.13.5 rather than inferred; the
// probes are reproduced in TestPyJSON_objectErrorPositions. The shape worth
// noting is that whitespace is skipped *before* each decision, so
// `{"a":1,  }` blames the comma at its own offset ("Illegal trailing comma")
// while `{"a":1, ` blames the position past the whitespace ("Expecting property
// name").
func parseJSONObject(doc []rune, idx int) (Value, int, error) {
	obj := NewObject()

	idx = skipJSONWhitespace(doc, idx)
	if idx < len(doc) && doc[idx] == '}' {
		return obj, idx + 1, nil // trivial empty object
	}

	for {
		idx = skipJSONWhitespace(doc, idx)
		if idx >= len(doc) || doc[idx] != '"' {
			return nil, 0, jsonErr(doc, msgExpectingProperty, idx)
		}
		key, end, err := scanStringLiteral(doc, idx)
		if err != nil {
			return nil, 0, err
		}
		idx = skipJSONWhitespace(doc, end)
		if idx >= len(doc) || doc[idx] != ':' {
			return nil, 0, jsonErr(doc, msgExpectingColon, idx)
		}
		idx = skipJSONWhitespace(doc, idx+1)

		value, end, err := scanOnce(doc, idx)
		if err != nil {
			return nil, 0, err
		}
		// A repeated key takes the later value and keeps the earlier position,
		// which is what CPython's dict does. Object.Set owns that rule.
		obj.Set(key.(string), value)

		idx = skipJSONWhitespace(doc, end)
		if idx < len(doc) && doc[idx] == '}' {
			return obj, idx + 1, nil
		}
		if idx >= len(doc) || doc[idx] != ',' {
			return nil, 0, jsonErr(doc, msgExpectingComma, idx)
		}
		commaIdx := idx
		idx = skipJSONWhitespace(doc, idx+1)
		if idx < len(doc) && doc[idx] == '}' {
			return nil, 0, jsonErr(doc, msgTrailingObject, commaIdx)
		}
	}
}

// parseJSONArray parses from just past the '['.
func parseJSONArray(doc []rune, idx int) (Value, int, error) {
	values := []Value{}

	idx = skipJSONWhitespace(doc, idx)
	if idx < len(doc) && doc[idx] == ']' {
		return values, idx + 1, nil // trivial empty array
	}

	for {
		value, end, err := scanOnce(doc, idx)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, value)

		idx = skipJSONWhitespace(doc, end)
		if idx < len(doc) && doc[idx] == ']' {
			return values, idx + 1, nil
		}
		if idx >= len(doc) || doc[idx] != ',' {
			return nil, 0, jsonErr(doc, msgExpectingComma, idx)
		}
		commaIdx := idx
		idx = skipJSONWhitespace(doc, idx+1)
		if idx < len(doc) && doc[idx] == ']' {
			return nil, 0, jsonErr(doc, msgTrailingArray, commaIdx)
		}
	}
}

// scanStringLiteral is the port of json.decoder.scanstring with strict=True
// (the default). idx must be the index of the opening quote; the returned index
// is just past the closing one.
func scanStringLiteral(doc []rune, idx int) (Value, int, error) {
	begin := idx
	var b strings.Builder
	i := idx + 1
	for {
		if i >= len(doc) {
			// Note the offset: the *opening* quote, not where the scan ran out.
			// A trailing backslash lands here too ("a\ has no terminator).
			return nil, 0, jsonErr(doc, msgUnterminated, begin)
		}
		c := doc[i]
		if c == '"' {
			return b.String(), i + 1, nil
		}
		if c != '\\' {
			if c < 0x20 {
				// strict=True: a raw control character inside a string literal
				// is an error rather than a literal character.
				return nil, 0, jsonErr(doc, msgControlChar, i)
			}
			writeCodePoint(&b, c)
			i++
			continue
		}
		if i+1 >= len(doc) {
			return nil, 0, jsonErr(doc, msgUnterminated, begin)
		}
		switch doc[i+1] {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			code, err := decodeUXXXX(doc, i+1)
			if err != nil {
				return nil, 0, err
			}
			i += 6
			// A high surrogate immediately followed by another \u escape is
			// combined *only* when the second one is a low surrogate. Note that
			// the second escape is decoded before that test, so a malformed one
			// raises even though the pair would not have combined —
			// `"\ud800\uZZZZ"` is an error, `"\ud800é"` is two characters.
			if code >= 0xd800 && code <= 0xdbff && i+1 < len(doc) && doc[i] == '\\' && doc[i+1] == 'u' {
				low, err := decodeUXXXX(doc, i+1)
				if err != nil {
					return nil, 0, err
				}
				if low >= 0xdc00 && low <= 0xdfff {
					code = 0x10000 + (((code - 0xd800) << 10) | (low - 0xdc00))
					i += 6
				}
			}
			writeCodePoint(&b, code)
			continue
		default:
			// The offset is the backslash, not the offending character.
			return nil, 0, jsonErr(doc, msgInvalidEscape, i)
		}
		i += 2
	}
}

// decodeUXXXX reads the four hex digits of a \uXXXX escape. uIdx is the index of
// the 'u'; CPython reports a malformed escape at that offset, not at the
// backslash — the one place where the two differ.
//
// DECISION — the bound needs a character AFTER the four hex digits, not merely
// the digits themselves. CPython's C scanner will not decode the escape unless
// one more character follows it, so an escape whose last hex digit is the final
// character of the document raises `Invalid \uXXXX escape` at the 'u' and never
// reaches the "unterminated" path. Both the message and the reported offset
// differ, so this changes errors[0] in the --json document consumers read:
//
//	`"\u0041`   (len 7, escape ends the document)
//	                 -> Invalid \uXXXX escape         @2  (the 'u')
//	`"\u0041x`  (one more character present)
//	                 -> Unterminated string starting at @0  (the opening quote)
//
// The same bound governs the low half of a surrogate pair: `"\ud800\udc00`
// raises at the *second* 'u' (@8), not at the first. That comes for free here
// because both halves are decoded through this function.
//
// Note the pure-Python fallback scanner in json/decoder.py *accepts* this input
// — it slices (`s[pos+1:pos+5]`), which is happy to stop at the end of the
// string. The C scanner is what CPython actually runs, and it is what we
// reproduce. Swept over every prefix of the \u-bearing corpus by
// tests/parity/run_parity.sh, section 17b ("truncation sweep — every prefix of
// a \uXXXX-bearing document").
func decodeUXXXX(doc []rune, uIdx int) (rune, error) {
	if uIdx+5 >= len(doc) {
		return 0, jsonErr(doc, msgInvalidUEscape, uIdx)
	}
	var code rune
	for k := 1; k <= 4; k++ {
		d := doc[uIdx+k]
		switch {
		case d >= '0' && d <= '9':
			code = code<<4 | (d - '0')
		case d >= 'a' && d <= 'f':
			code = code<<4 | (d - 'a' + 10)
		case d >= 'A' && d <= 'F':
			code = code<<4 | (d - 'A' + 10)
		default:
			return 0, jsonErr(doc, msgInvalidUEscape, uIdx)
		}
	}
	return code, nil
}

// writeCodePoint appends one code point, including the ones Go refuses to
// encode.
//
// DECISION — unpaired surrogates. `json.loads(r'"\ud800"')` gives Python a str
// holding one lone surrogate (len 1). Go's utf8.EncodeRune substitutes U+FFFD
// for any surrogate, which would silently turn that into a *different* string:
// still length 1, but no longer equal to the Python value, so a minLength check
// or an enum comparison could come out differently with nothing on stderr to say
// why. Rather than accept a silent verdict change, the surrogate is written in
// WTF-8 (its literal 3-byte UTF-8-style encoding) and pyRunes decodes it back to
// the same code point. Every length count and every repr in the port goes
// through pyRunes/pyLen, so the round trip is closed. See pystr.go.
func writeCodePoint(b *strings.Builder, r rune) {
	if r >= 0xd800 && r <= 0xdfff {
		b.WriteByte(byte(0xe0 | (r >> 12)))
		b.WriteByte(byte(0x80 | ((r >> 6) & 0x3f)))
		b.WriteByte(byte(0x80 | (r & 0x3f)))
		return
	}
	var buf [utf8.UTFMax]byte
	b.Write(buf[:utf8.EncodeRune(buf[:], r)])
}

// matchJSONNumber is json/decoder.py's NUMBER_RE, anchored at idx:
//
//	(-?(?:0|[1-9]\d*))(\.\d+)?([eE][-+]?\d+)?
//
// It returns the matched literal verbatim, so newNumber can apply Python's
// int-vs-float rule to it: a token with neither a fraction nor an exponent is an
// int (arbitrary precision), anything else is a float.
func matchJSONNumber(doc []rune, idx int) (raw string, end int, ok bool) {
	i := idx
	if i < len(doc) && doc[i] == '-' {
		i++
	}
	switch {
	case i < len(doc) && doc[i] == '0':
		i++
	case i < len(doc) && doc[i] >= '1' && doc[i] <= '9':
		for i < len(doc) && isASCIIDigit(doc[i]) {
			i++
		}
	default:
		return "", 0, false // no integer part: not a number (a bare '-', say)
	}

	if i+1 < len(doc) && doc[i] == '.' && isASCIIDigit(doc[i+1]) {
		i += 2
		for i < len(doc) && isASCIIDigit(doc[i]) {
			i++
		}
	}

	if i < len(doc) && (doc[i] == 'e' || doc[i] == 'E') {
		j := i + 1
		if j < len(doc) && (doc[j] == '+' || doc[j] == '-') {
			j++
		}
		if j < len(doc) && isASCIIDigit(doc[j]) {
			for j < len(doc) && isASCIIDigit(doc[j]) {
				j++
			}
			i = j
		}
		// An 'e' with no digits after it is not part of the number; it is left
		// for the caller, which reports it as Extra data or a missing delimiter.
	}

	return string(doc[idx:i]), i, true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

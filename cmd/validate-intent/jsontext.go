package main

// A JSON parser for exactly the language PROTOCOL.md §1.1 accepts.
//
// The specification, not this file, decides what is valid. §1.1 says a
// normalized annotation payload is a JSON text per RFC 8259, and it resolves
// the three points RFC 8259 leaves to the implementation. This parser
// implements that and nothing else:
//
//	§1.1(a)  an unpaired surrogate escape is refused   (RFC 8259 §8.2)
//	§1.1(b)  NaN / Infinity / -Infinity are refused    (RFC 8259 §6)
//	§1.1(c)  nesting deeper than MaxNestingDepth is refused (RFC 8259 §9)
//
// Each refusal names the rule it enforces, because a diagnostic that only says
// "invalid" leaves the reader to guess whether the tool or the payload is
// wrong. §1.1(a) in particular refuses input that most parsers accept, so it
// owes the reader a reason they can look up.
//
// WHY NOT encoding/json. Two reasons, both of which change verdicts rather
// than only prose. It substitutes U+FFFD for an unpaired surrogate escape
// instead of refusing it, which is precisely the silent repair §1.1 forbids;
// and it decodes an object into a map, losing the order the keys appeared in,
// while the validator reports one error per key in document order (see
// Object). A parser that answered the right verdict in a nondeterministic
// order would pass the fixture corpus on some runs and fail it on others.

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxNestingDepth is PROTOCOL.md §1.1(c). The outermost container is depth 1.
//
// It is a named constant with the section number on it because the number is a
// published part of the protocol: changing it here without changing the
// document makes the document wrong, which is the one direction this repository
// does not allow.
const MaxNestingDepth = 100

// JSONSyntaxError is a payload that is not a JSON text per PROTOCOL.md §1.1.
//
// Position is carried as line and column rather than as a byte offset because
// the reader is looking at an annotation on one line of a test file, and
// "column 34" is something they can count to.
type JSONSyntaxError struct {
	Msg  string
	Line int // 1-based
	Col  int // 1-based, counted in characters
}

func (e *JSONSyntaxError) Error() string {
	return fmt.Sprintf("%s (line %d, column %d)", e.Msg, e.Line, e.Col)
}

// jsonParser holds the document as code points, so a column is a character
// count and not a byte count. An annotation payload is one line of a source
// file; measuring its columns in bytes would put the caret in the wrong place
// on any line containing a non-ASCII character, which the shipped corpus has
// (examples/sources/order_spec.rb).
type jsonParser struct {
	doc []rune
}

// DecodeJSON parses one JSON text from bytes.
//
// Bytes that are not well-formed UTF-8 are refused here rather than being
// decoded lossily: PROTOCOL.md §1.1 makes UTF-8 part of the definition of a
// JSON text, and Go's []rune conversion would otherwise turn every undecodable
// byte into U+FFFD and hand the validator a string the author never wrote.
func DecodeJSON(data []byte) (Value, error) {
	if !utf8.Valid(data) {
		return nil, &JSONSyntaxError{Msg: errNotUTF8, Line: 1, Col: 1}
	}
	return DecodeJSONString(string(data))
}

// DecodeJSONString is DecodeJSON over text that has already been decoded — the
// annotation path, where normalization produced a Go string rather than bytes.
func DecodeJSONString(s string) (Value, error) {
	p := &jsonParser{doc: []rune(s)}

	// A byte-order mark is not whitespace and not a value. RFC 8259 §8.1: "the
	// JSON text SHALL be encoded in UTF-8" and implementations MUST NOT add a
	// BOM. Skipping one silently would accept a document the specification does
	// not describe.
	if len(p.doc) > 0 && p.doc[0] == 0xfeff {
		return nil, p.errorAt(0, errByteOrderMark)
	}

	i := p.skipWhitespace(0)
	value, end, err := p.parseValue(i, 1)
	if err != nil {
		return nil, err
	}
	end = p.skipWhitespace(end)
	if end != len(p.doc) {
		return nil, p.errorAt(end, errTrailingContent)
	}
	return value, nil
}

// The diagnostics. They are the port's own prose, in the specification's
// vocabulary, and the three that enforce §1.1 cite the rule they enforce.
const (
	errExpectedValue   = "expected a JSON value"
	errExpectedName    = "expected a double-quoted property name"
	errExpectedColon   = "expected ':' after a property name"
	errExpectedComma   = "expected ',' or a closing bracket"
	errTrailingComma   = "trailing comma before a closing bracket"
	errUnterminated    = "unterminated string"
	errControlChar     = "unescaped control character in a string (RFC 8259 §7 requires it to be escaped)"
	errBadEscape       = `unrecognised escape sequence`
	errBadUnicode      = `malformed \uXXXX escape`
	errTrailingContent = "unexpected content after the end of the JSON value"
	errByteOrderMark   = "byte-order mark at the start of the document (RFC 8259 §8.1 forbids it)"
	errNotUTF8         = "input is not well-formed UTF-8 (PROTOCOL.md §1.1 requires it)"
	errBadNumber       = "malformed number"
)

// errNonFinite and errTooDeep are formatted rather than constant because naming
// the offending literal, and the limit that was exceeded, is what makes them
// actionable — see PROTOCOL.md §1.1(b) and §1.1(c).
func errNonFinite(literal string) string {
	return fmt.Sprintf(
		"%q is not a JSON number (PROTOCOL.md §1.1(b): RFC 8259 §6 has no non-finite literals)",
		literal)
}

func errTooDeep() string {
	return fmt.Sprintf(
		"nesting is deeper than %d levels (PROTOCOL.md §1.1(c))", MaxNestingDepth)
}

// errUnpairedSurrogate is PROTOCOL.md §1.1(a).
//
// It names the escape that failed and says which half of the pair was missing,
// because the two cases are fixed differently: a lone high surrogate is usually
// a truncated pair, and a lone low surrogate is usually a pair whose halves got
// swapped or split.
func errUnpairedSurrogate(code rune, high bool) string {
	half := "low"
	if !high {
		half = "high"
	}
	return fmt.Sprintf(
		`unpaired surrogate escape \u%04X (PROTOCOL.md §1.1(a): RFC 8259 §8.2 requires a %s surrogate escape to complete the pair)`,
		code, half)
}

// errorAt builds the error, deriving line and column from a code-point offset.
//
// Lines are counted on "\n" alone. A payload is one line of a test file by
// construction, so this is only ever exercised by a whole-document parse of a
// standalone `.json` fixture, where "\n" is what a line is.
func (p *jsonParser) errorAt(pos int, msg string) error {
	line, lastNL := 1, -1
	for i := 0; i < pos && i < len(p.doc); i++ {
		if p.doc[i] == '\n' {
			line++
			lastNL = i
		}
	}
	return &JSONSyntaxError{Msg: msg, Line: line, Col: pos - lastNL}
}

// skipWhitespace advances past RFC 8259 §2's whitespace, which is exactly
// {space, tab, LF, CR}. Nothing else is insignificant between JSON tokens — a
// vertical tab between two values is a syntax error, not a separator.
func (p *jsonParser) skipWhitespace(i int) int {
	for i < len(p.doc) {
		switch p.doc[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// parseValue parses one value at idx. depth is the nesting level this value
// would occupy if it turns out to be a container: the outermost value is 1.
func (p *jsonParser) parseValue(idx, depth int) (Value, int, error) {
	if idx >= len(p.doc) {
		return nil, 0, p.errorAt(idx, errExpectedValue)
	}
	switch p.doc[idx] {
	case '"':
		return p.parseString(idx)
	case '{':
		if depth > MaxNestingDepth {
			return nil, 0, p.errorAt(idx, errTooDeep())
		}
		return p.parseObject(idx+1, depth)
	case '[':
		if depth > MaxNestingDepth {
			return nil, 0, p.errorAt(idx, errTooDeep())
		}
		return p.parseArray(idx+1, depth)
	}
	if p.hasPrefixAt(idx, "null") {
		return nil, idx + 4, nil
	}
	if p.hasPrefixAt(idx, "true") {
		return true, idx + 4, nil
	}
	if p.hasPrefixAt(idx, "false") {
		return false, idx + 5, nil
	}
	if raw, end, ok := p.matchNumber(idx); ok {
		number, err := newNumber(raw)
		if err != nil {
			// Unreachable for anything matchNumber accepts: a literal outside
			// float64's range still parses, saturating to ±Inf as any IEEE-754
			// conversion does. Reported rather than ignored, because a number
			// that matched the grammar and then failed to convert would
			// otherwise be silently dropped.
			return nil, 0, p.errorAt(idx, errBadNumber)
		}
		return number, end, nil
	}
	// PROTOCOL.md §1.1(b). These three are matched only to REFUSE them, and only
	// after the number grammar has failed — so `-Infinity` is reached once
	// matchNumber has rejected the bare '-'. Recognising them by name buys the
	// author a diagnostic that says what is wrong with their payload instead of
	// the bare "expected a JSON value" a stricter parser would give.
	for _, literal := range []string{"NaN", "Infinity", "-Infinity"} {
		if p.hasPrefixAt(idx, literal) {
			return nil, 0, p.errorAt(idx, errNonFinite(literal))
		}
	}
	return nil, 0, p.errorAt(idx, errExpectedValue)
}

func (p *jsonParser) hasPrefixAt(idx int, literal string) bool {
	if idx+len(literal) > len(p.doc) {
		return false
	}
	for i := 0; i < len(literal); i++ {
		if p.doc[idx+i] != rune(literal[i]) {
			return false
		}
	}
	return true
}

// parseObject parses from just past the '{'.
func (p *jsonParser) parseObject(idx, depth int) (Value, int, error) {
	obj := NewObject()

	idx = p.skipWhitespace(idx)
	if idx < len(p.doc) && p.doc[idx] == '}' {
		return obj, idx + 1, nil
	}

	for {
		idx = p.skipWhitespace(idx)
		if idx >= len(p.doc) || p.doc[idx] != '"' {
			return nil, 0, p.errorAt(idx, errExpectedName)
		}
		key, end, err := p.parseString(idx)
		if err != nil {
			return nil, 0, err
		}
		idx = p.skipWhitespace(end)
		if idx >= len(p.doc) || p.doc[idx] != ':' {
			return nil, 0, p.errorAt(idx, errExpectedColon)
		}
		idx = p.skipWhitespace(idx + 1)

		value, end, err := p.parseValue(idx, depth+1)
		if err != nil {
			return nil, 0, err
		}
		// PROTOCOL.md §1.1 "Duplicate names": last value wins, first position
		// kept. Object.Set owns that rule so the parser and the validator cannot
		// disagree about it.
		obj.Set(key.(string), value)

		idx = p.skipWhitespace(end)
		if idx < len(p.doc) && p.doc[idx] == '}' {
			return obj, idx + 1, nil
		}
		if idx >= len(p.doc) || p.doc[idx] != ',' {
			return nil, 0, p.errorAt(idx, errExpectedComma)
		}
		comma := idx
		idx = p.skipWhitespace(idx + 1)
		if idx < len(p.doc) && p.doc[idx] == '}' {
			return nil, 0, p.errorAt(comma, errTrailingComma)
		}
	}
}

// parseArray parses from just past the '['.
func (p *jsonParser) parseArray(idx, depth int) (Value, int, error) {
	values := []Value{}

	idx = p.skipWhitespace(idx)
	if idx < len(p.doc) && p.doc[idx] == ']' {
		return values, idx + 1, nil
	}

	for {
		value, end, err := p.parseValue(idx, depth+1)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, value)

		idx = p.skipWhitespace(end)
		if idx < len(p.doc) && p.doc[idx] == ']' {
			return values, idx + 1, nil
		}
		if idx >= len(p.doc) || p.doc[idx] != ',' {
			return nil, 0, p.errorAt(idx, errExpectedComma)
		}
		comma := idx
		idx = p.skipWhitespace(idx + 1)
		if idx < len(p.doc) && p.doc[idx] == ']' {
			return nil, 0, p.errorAt(comma, errTrailingComma)
		}
	}
}

// parseString parses a string literal. idx is the opening quote; the returned
// index is just past the closing one.
//
// Every code point this produces is a real Unicode scalar value, which is what
// lets the rest of the port hold a decoded payload in an ordinary Go string and
// count its length with utf8.RuneCountInString. That property comes from
// PROTOCOL.md §1.1(a) and from nothing else: a parser accepting an unpaired
// surrogate escape has to invent an encoding for it, and every length, every
// comparison and every re-serialisation downstream then has to know about that
// invention.
func (p *jsonParser) parseString(idx int) (Value, int, error) {
	begin := idx
	var b strings.Builder
	i := idx + 1
	for {
		if i >= len(p.doc) {
			// Blamed on the opening quote, not on the end of input: that is the
			// character the author has to look at.
			return nil, 0, p.errorAt(begin, errUnterminated)
		}
		c := p.doc[i]
		if c == '"' {
			return b.String(), i + 1, nil
		}
		if c != '\\' {
			if c < 0x20 {
				return nil, 0, p.errorAt(i, errControlChar)
			}
			b.WriteRune(c)
			i++
			continue
		}
		if i+1 >= len(p.doc) {
			return nil, 0, p.errorAt(begin, errUnterminated)
		}
		switch p.doc[i+1] {
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
			code, err := p.parseHex4(i + 1)
			if err != nil {
				return nil, 0, err
			}
			escapeAt := i
			i += 6

			switch {
			case code >= 0xd800 && code <= 0xdbff:
				// PROTOCOL.md §1.1(a): a high surrogate MUST be completed by a
				// low one. Anything else — a different escape, a literal
				// character, the end of the string — is unpaired.
				if i+1 >= len(p.doc) || p.doc[i] != '\\' || p.doc[i+1] != 'u' {
					return nil, 0, p.errorAt(escapeAt, errUnpairedSurrogate(code, true))
				}
				low, err := p.parseHex4(i + 1)
				if err != nil {
					return nil, 0, err
				}
				if low < 0xdc00 || low > 0xdfff {
					return nil, 0, p.errorAt(escapeAt, errUnpairedSurrogate(code, true))
				}
				code = 0x10000 + (((code - 0xd800) << 10) | (low - 0xdc00))
				i += 6
			case code >= 0xdc00 && code <= 0xdfff:
				// A low surrogate reaching here was not consumed by the branch
				// above, so it has no high half in front of it.
				return nil, 0, p.errorAt(escapeAt, errUnpairedSurrogate(code, false))
			}
			b.WriteRune(code)
			continue
		default:
			// The backslash is the offending character, not what follows it.
			return nil, 0, p.errorAt(i, errBadEscape)
		}
		i += 2
	}
}

// parseHex4 reads the four hex digits of a \uXXXX escape. uIdx is the index of
// the 'u', which is where a malformed escape is blamed.
func (p *jsonParser) parseHex4(uIdx int) (rune, error) {
	if uIdx+4 >= len(p.doc) {
		return 0, p.errorAt(uIdx, errBadUnicode)
	}
	var code rune
	for k := 1; k <= 4; k++ {
		d := p.doc[uIdx+k]
		switch {
		case d >= '0' && d <= '9':
			code = code<<4 | (d - '0')
		case d >= 'a' && d <= 'f':
			code = code<<4 | (d - 'a' + 10)
		case d >= 'A' && d <= 'F':
			code = code<<4 | (d - 'A' + 10)
		default:
			return 0, p.errorAt(uIdx, errBadUnicode)
		}
	}
	return code, nil
}

// matchNumber matches RFC 8259 §6's number grammar, anchored at idx:
//
//	-? (0 | [1-9][0-9]*) (. [0-9]+)? ([eE] [-+]? [0-9]+)?
//
// It returns the literal verbatim so newNumber can preserve the integer/real
// distinction the `integer` and `number` schema types draw.
func (p *jsonParser) matchNumber(idx int) (raw string, end int, ok bool) {
	doc := p.doc
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
		return "", 0, false // no integer part — a bare '-', say
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
		// An 'e' with no digits after it is not part of the number. It is left
		// for the caller, which reports it as trailing content or a missing
		// delimiter.
	}

	return string(doc[idx:i]), i, true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

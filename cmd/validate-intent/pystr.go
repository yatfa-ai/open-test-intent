package main

// Python `str` semantics that Go's standard library gets *almost* right.
//
// The three helpers here back the four divergences slice 2 had to settle
// (SPGD-102). Each is small, and each is the difference between a port that
// looks correct and one that is. They live together because they share a single
// root cause: `str` in Python is a sequence of code points with its own
// definitions of "whitespace" and "line", and none of those three ideas survives
// a naive translation to Go.
//
//	DIVERGENCE 1  str.splitlines()  -> pySplitlines   (see below)
//	DIVERGENCE 2  str.isspace()     -> pyIsSpace      (see below)
//	DIVERGENCE 3  json.dumps(str)   -> pyJSONDumpsString (see below)
//	DIVERGENCE 4  code-point indexing -> []rune, settled at the call sites in
//	              source.go rather than here; see the DIVERGENCE 4 note there.
//
// Every set below was enumerated by running CPython 3.13.5 over the whole
// range(0x110000) rather than transcribed from documentation — see the
// TestPy*_matchesCPython tests, which pin the enumerations.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// --------------------------------------------------------------------------- //
// DIVERGENCE 1 — str.splitlines()
// --------------------------------------------------------------------------- //

// pyLineTerminators is the exact set of characters CPython's str.splitlines()
// breaks on, enumerated from CPython 3.13.5:
//
//	['0xa', '0xb', '0xc', '0xd', '0x1c', '0x1d', '0x1e', '0x85', '0x2028', '0x2029']
//
// Go's strings.Split(s, "\n") knows only the first of them, and even
// bufio.ScanLines knows only "\n" and "\r\n".
//
// DECISION: reproduce Python's set exactly rather than approximate it.
//
// This is load-bearing, not pedantry. extract_intents numbers its findings by
// enumerating splitlines() (bin/validate-intent:320) and `--source` reports
// every finding as `file:line`, so a port that splits on "\n" alone reports a
// *shifted line number* for every annotation after the first \v, \f, \x1c,
// \x1d, \x1e, \x85, U+2028 or U+2029 anywhere in the file. Measured on the
// ticket's probe: "a\x0bb\x0cc\x1dd\x85e".splitlines() is 5 lines in Python and
// strings.Split(s, "\n") is 1 in Go. The wrong line number is the most
// dangerous shape of failure this mode has — it is specific, confident, and
// points the reader at innocent code.
//
// Note the deliberate asymmetry with pyIsSpace below: 0x1f is whitespace to
// Python but is *not* a line terminator, and 0x1c-0x1e are both. The two sets
// are genuinely different and are therefore listed separately rather than
// derived from one another.
func pyLineTerminator(r rune) bool {
	switch r {
	case '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// pySplitlines is the port of str.splitlines() (used at bin/validate-intent:320).
//
// It splits on every character in pyLineTerminator, treats "\r\n" as a single
// terminator, drops the terminators themselves, and — like Python — yields no
// trailing empty element for a string that ends in one. "" splits to zero lines,
// not one empty line.
func pySplitlines(s string) []string {
	lines := []string{}
	runes := pyRunes(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !pyLineTerminator(runes[i]) {
			continue
		}
		lines = append(lines, string(runes[start:i]))
		if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
			i++ // "\r\n" is one terminator, not two
		}
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// --------------------------------------------------------------------------- //
// DIVERGENCE 2 — str.isspace()
// --------------------------------------------------------------------------- //

// pyIsSpace is the port of str.isspace() for a single character.
//
// normalize_payload uses it twice as a *lookahead*: once to find the ':' after a
// bare word (bin/validate-intent:398) and once to find the '}'/']' after a comma
// (bin/validate-intent:409). Both decide whether a rewrite happens at all, so
// getting the set wrong silently changes the normalized payload — a bare word
// stops being recognised as a key and is left unquoted, and the payload then
// fails to parse.
//
// DECISION: Python's set is Go's unicode.IsSpace plus U+001C-U+001F.
//
// Enumerated over range(0x110000) on CPython 3.13.5: Python answers True for 29
// characters, Go's unicode.IsSpace (which tracks the Unicode White_Space
// property) for 25. The four Python adds are the C0 information separators
// U+001C FILE SEPARATOR, U+001D GROUP SEPARATOR, U+001E RECORD SEPARATOR and
// U+001F UNIT SEPARATOR, which CPython treats as space because their
// bidirectional class is B or S. Nothing goes the other way — Go's set is a
// strict subset — so the union is written as exactly that, and
// TestPyIsSpace_matchesCPython pins the whole enumeration so a Go stdlib
// Unicode-table update cannot silently widen it.
func pyIsSpace(r rune) bool {
	if r >= 0x1c && r <= 0x1f {
		return true
	}
	return unicode.IsSpace(r)
}

// --------------------------------------------------------------------------- //
// DIVERGENCE 3 — json.dumps(str)
// --------------------------------------------------------------------------- //

// pyJSONDumpsString is the port of json.dumps() over a str, used by
// normalize_payload to quote a bare-word key (bin/validate-intent:403).
//
// DECISION: reproduce json.dumps' defaults — ensure_ascii=True and no HTML
// escaping — rather than reach for encoding/json, which disagrees in *both*
// directions:
//
//	json.dumps("café")   -> "café"    Go's encoding/json emits raw UTF-8
//	json.dumps("a<b>&c") -> "a<b>&c"       Go escapes to </>/&
//	                                        unless the encoder sets
//	                                        SetEscapeHTML(false)
//
// so neither Go default matches: one over-escapes non-ASCII, the other
// over-escapes HTML metacharacters, and the two cannot be fixed by flipping a
// single flag.
//
// HONEST SCOPE NOTE — this is currently unreachable, and is written exactly
// anyway. Its only caller passes a match of _BARE_WORD_RE
// (bin/validate-intent:260), which is `[A-Za-z_$][A-Za-z0-9_$]*` — ASCII
// letters, digits, underscore and dollar. No such string contains a character
// json.dumps would escape, so today this function provably always returns
// '"' + word + '"'.
//
// That was MEASURED, not assumed: deleting the non-ASCII branch below leaves
// tests/parity/run_parity.sh entirely green and fails only
// TestPyJSONDumpsString_matchesCPython. The unit test is therefore the only
// thing holding this correct, which is precisely why it compares against
// python3's own json.dumps over astral-plane characters, control characters and
// the <>& trio rather than over the handful of words the bare-word class can
// currently produce.
//
// It is written out in full because the *reachable* half of divergence 3 is
// what happens to the input around such a word: `{café: "x"}` matches only
// `caf`, leaving `é` outside the match, and `{a<b>&c: "y"}` normalizes to
// `{a<b>&"c": "y"}` — only `c` is in key position. Both are covered by parity
// fixtures. Should the bare-word pattern ever widen, this function is already
// correct instead of becoming a bug on the same commit.
func pyJSONDumpsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range pyRunes(s) {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || r > 0x7e:
				// ensure_ascii=True: everything outside printable ASCII becomes
				// a \uXXXX escape, and anything above the BMP becomes a
				// surrogate pair — which is what CPython's py_encode_basestring_ascii
				// does. '<', '>' and '&' fall through untouched.
				if r > 0xffff {
					r -= 0x10000
					b.WriteString(hexEscape(0xd800 + (r >> 10)))
					b.WriteString(hexEscape(0xdc00 + (r & 0x3ff)))
					continue
				}
				b.WriteString(hexEscape(r))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

const hexDigits = "0123456789abcdef"

func hexEscape(r rune) string {
	return `\u` + string([]byte{
		hexDigits[(r>>12)&0xf], hexDigits[(r>>8)&0xf],
		hexDigits[(r>>4)&0xf], hexDigits[r&0xf],
	})
}

// --------------------------------------------------------------------------- //
// code points, including the ones Go's utf8 package refuses to decode
// --------------------------------------------------------------------------- //

// pyRunes decodes a Go string into the code points a Python str would hold.
//
// It is `[]rune(s)` plus one thing: a WTF-8-encoded lone surrogate (the three
// bytes ED A0..BF 80..BF) decodes back to the single surrogate code point rather
// than to three U+FFFD replacements. Nothing in Go's standard library will do
// that — utf8.DecodeRune rejects surrogates by design — but Python's json
// decoder happily produces a str holding a lone surrogate from a `"\ud800"`
// literal, so the port has to be able to carry one. See pyjson.go's
// scanStringLiteral for the encoding half and the decision behind it.
//
// Every place the port counts or iterates "characters" the way Python does goes
// through here, so the lone-surrogate case cannot leak into a length or a repr.
func pyRunes(s string) []rune {
	// Fast path. Valid UTF-8 cannot encode a surrogate at all, so []rune is
	// already exact and there is nothing for the loop below to rescue. (Guarding
	// on utf8.ValidString rather than on a 0xED lead byte is deliberate: 0xED
	// also leads U+D000-U+D7FF, which are ordinary characters, so a byte test
	// would send perfectly normal text down the slow path — and a `ContainsRune`
	// test would look for U+00ED, an entirely different character.)
	if utf8.ValidString(s) {
		return []rune(s)
	}
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xed && i+2 < len(s) && s[i+1] >= 0xa0 && s[i+1] <= 0xbf && s[i+2] >= 0x80 && s[i+2] <= 0xbf {
			runes = append(runes, rune(s[i]&0x0f)<<12|rune(s[i+1]&0x3f)<<6|rune(s[i+2]&0x3f))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		runes = append(runes, r)
		i += size
	}
	return runes
}

// pyLen is Python's len() over a str: a count of code points, not bytes and not
// UTF-8 sequences. Used wherever the reference's messages interpolate len().
func pyLen(s string) int {
	return len(pyRunes(s))
}

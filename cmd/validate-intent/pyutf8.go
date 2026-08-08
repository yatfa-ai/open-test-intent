package main

// CPython's UTF-8 *decoder*, both of its answers.
//
// Python decodes bytes to `str` at the boundary — `open(..., encoding="utf-8")`
// and `sys.stdin` — and what it does with bytes that do not decode depends on
// the error handler in force:
//
//	strict           raise UnicodeDecodeError  -> a READ failure (KIND_READ)
//	surrogateescape  map each bad byte to U+DC00+byte and carry on
//
// Both are reachable from the same binary (see pyStdinErrors), and they produce
// *completely different* verdicts for the same bytes: strict never reaches the
// parser, while surrogateescape hands the parser a string that may well parse
// and then fail the schema instead. Getting the handler right is therefore not
// cosmetic — it decides `kind`, `summary.annotations` and often the exit code.
//
// Go has neither behaviour. utf8.DecodeRune reports "bad byte" but not CPython's
// error *span*, and Go's implicit []byte->string conversion silently substitutes
// U+FFFD, which is a third answer matching neither.

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// --------------------------------------------------------------------------- //
// the strict handler — str(UnicodeDecodeError)
// --------------------------------------------------------------------------- //

// utf8DecodeFailure finds the first sequence CPython's UTF-8 decoder rejects and
// returns the byte span it blames plus its reason, or ok=false when the input
// decodes cleanly.
//
// The span is the load-bearing part and it is NOT one byte per failure. CPython
// blames the whole *partial* sequence it had accepted so far, so a truncated
// three-byte character is "bytes in position 10-11" while a bad lead byte is
// "byte 0xff in position 0" — a different message template, not just different
// numbers. Transcribed from CPython's unicode_decode_utf8 (Objects/unicodeobject.c)
// and pinned against python3 itself by TestUTF8DecodeFailure_matchesCPython.
func utf8DecodeFailure(data []byte) (start, end int, reason string, ok bool) {
	const (
		endOfData   = "unexpected end of data"
		badStart    = "invalid start byte"
		badContinue = "invalid continuation byte"
	)

	inRange := func(b byte, lo, hi byte) bool { return b >= lo && b <= hi }

	for i := 0; i < len(data); {
		lead := data[i]
		switch {
		case lead < 0x80:
			i++
			continue

		case lead < 0xc2:
			// 0x80-0xc1: a continuation byte with no lead, or an overlong
			// two-byte form. Neither can start a character.
			return i, i + 1, badStart, true

		case lead < 0xe0: // two-byte sequence
			if len(data)-i < 2 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+1], 0x80, 0xbf) {
				return i, i + 1, badContinue, true
			}
			i += 2

		case lead < 0xf0: // three-byte sequence
			// The second byte's legal range narrows for the two lead bytes that
			// would otherwise admit an overlong form (0xe0) or a surrogate
			// (0xed). Widening either here is how a decoder ends up accepting
			// bytes CPython rejects.
			lo, hi := byte(0x80), byte(0xbf)
			if lead == 0xe0 {
				lo = 0xa0
			} else if lead == 0xed {
				hi = 0x9f
			}
			if len(data)-i < 2 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+1], lo, hi) {
				return i, i + 1, badContinue, true
			}
			if len(data)-i < 3 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+2], 0x80, 0xbf) {
				return i, i + 2, badContinue, true
			}
			i += 3

		case lead < 0xf5: // four-byte sequence
			lo, hi := byte(0x80), byte(0xbf)
			if lead == 0xf0 {
				lo = 0x90
			} else if lead == 0xf4 {
				hi = 0x8f
			}
			if len(data)-i < 2 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+1], lo, hi) {
				return i, i + 1, badContinue, true
			}
			if len(data)-i < 3 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+2], 0x80, 0xbf) {
				return i, i + 2, badContinue, true
			}
			if len(data)-i < 4 {
				return i, len(data), endOfData, true
			}
			if !inRange(data[i+3], 0x80, 0xbf) {
				return i, i + 3, badContinue, true
			}
			i += 4

		default: // 0xf5-0xff: beyond U+10FFFF, or not a lead byte at all
			return i, i + 1, badStart, true
		}
	}
	return 0, 0, "", false
}

// pyUnicodeDecodeError renders str(UnicodeDecodeError) for bytes that do not
// decode as UTF-8.
//
// CPython uses two different message templates depending on the width of the
// blamed span (unicodeobject.c, make_decode_exception -> its "%zd-%zd" branch),
// and both appear in this project's own corpus: a stray 0xff is the singular
// form, a truncated multi-byte character the plural one. Emitting only the
// singular form — which is what a naive port does, since utf8.DecodeRune hands
// back one byte at a time — produces a message that is *almost* right, which is
// the shape of divergence a reader trusts.
//
// KNOWN LIMIT, stated rather than discovered later: the position is an offset
// into the buffer CPython was decoding. For sys.stdin.read() and for the files
// this tool reads that is the whole input, but TextIOWrapper decodes a large
// file in chunks, so for a decode failure past the first chunk CPython's offset
// is chunk-relative and this one is not. Every fixture here is far below that
// threshold; a file large enough to reach it would be a different bug report.
func pyUnicodeDecodeError(data []byte) string {
	start, end, reason, ok := utf8DecodeFailure(data)
	if !ok {
		// Defensive: callers only reach this after !utf8.Valid(data), so the
		// two classifiers disagreeing would be a bug in one of them. Say so
		// rather than returning a confident-looking message about byte 0.
		return "'utf-8' codec can't decode the input"
	}
	if end-start == 1 {
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s",
			data[start], start, reason)
	}
	return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s",
		start, end-1, reason)
}

// --------------------------------------------------------------------------- //
// the surrogateescape handler
// --------------------------------------------------------------------------- //

// pySurrogateEscape decodes bytes the way `errors="surrogateescape"` does: every
// byte that cannot be part of a valid UTF-8 sequence becomes the lone surrogate
// U+DC00+byte, and everything else decodes normally.
//
// The result is carried as WTF-8, the same representation pyRunes/PyReprString/
// pyJSONDumpsString already use for the lone surrogates json.loads can produce
// from a "\udc80" literal — so a surrogate that arrives through stdin and one
// that arrives through a JSON escape are the same value downstream, and both
// repr as \udcXX and dump as \udcXX without any special case.
//
// Byte-at-a-time is equivalent to CPython's span-at-a-time replacement even
// though utf8DecodeFailure above shows the spans are not the same: the handler
// maps every byte in the blamed span individually, and CPython resumes decoding
// at the end of the span, which is exactly where a byte-wise walk arrives.
func pySurrogateEscape(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data))
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			// utf8.DecodeRune reports size 1 for both "not a lead byte" and
			// "truncated sequence"; either way this single byte did not decode,
			// so it is escaped and the walk resumes at the next one.
			writeCodePoint(&b, rune(0xdc00+int(data[i])))
			i++
			continue
		}
		b.Write(data[i : i+size])
		i += size
	}
	return b.String()
}

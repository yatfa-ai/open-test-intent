package main

// Rendering: how a value, a path or a diagnostic string is written out.
//
// Two audiences, two encoders, and they are deliberately different:
//
//   - Quote / RenderValue produce the HUMAN report. A quoted string there is
//     for a person reading a terminal, so it stays as close to the author's own
//     text as it can while still showing where the value begins and ends.
//   - EncodeJSONString / EncodeJSONPath produce the `--json` document. They are
//     bound by RFC 8259 §7 and by nothing else: exactly the escapes the grammar
//     requires, and no others.
//
// The second is why this file does not reach for encoding/json. That encoder
// escapes `<`, `>` and `&` — a legacy of embedding JSON in HTML — which would
// mangle every diagnostic quoting an author's payload, and it reorders or
// reflects object keys, which the report's fixed key order cannot survive.
//
// Both audiences are handed operating-system bytes — a path, a glob pattern, an
// OS error quoting one — and POSIX pathnames are byte strings with no encoding
// guarantee. Neither renderer may answer that by substituting U+FFFD, which
// PROTOCOL.md §1.1(a) forbids for a payload byte and which merges two files
// into one report entry. Both write such a byte `\xHH`; only EncodeJSONPath is
// additionally injective, because only it names something.

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// --------------------------------------------------------------------------- //
// the human report
// --------------------------------------------------------------------------- //

// Quote renders a string for a diagnostic.
//
// The style — single quotes, switching to double only when the string contains
// a `\'` and no `"` — is THE ECOSYSTEM'S MESSAGE VOCABULARY, not a stylistic
// preference and not this file's to change. `specguard-rspec` renders the same
// violations from its own structured fields, and the two tools spell them the
// same way BY CONVENTION — the client-gem spec's criterion 7 — not because
// either suite pins the other. NO SPEC IN EITHER REPO COMPARES THE TWO
// RENDERERS: the gem's spec/specguard/rspec/message_parity_spec.rb runs four
// payloads through its own `Schema#violations` and asserts the result against
// strings hand-copied from this binary, and its header is explicit that
// reading four hand-copied strings as a proof about two programs is a
// misreading; the gem's spec/specguard/rspec/validator_backend_spec.rb, the
// nearest thing to a cross-tool comparison, replays reports RECORDED from this
// binary through a shell stub and never executes it either. The agreement is
// therefore held by this comment and its counterpart in the gem, which is
// exactly why changing the quoting here breaks it silently, in a place only a
// CI log diff would show.
//
// Escaping: a printable character stays literal, including a non-ASCII one, so
// a `behavior` sentence keeps its em dash; C0 controls and DEL become visible
// escapes, so a path containing a newline or an ANSI sequence prints as
// something a reader can copy rather than as something that reformats their
// terminal.
//
// A BYTE THAT IS NOT PART OF A WELL-FORMED UTF-8 SEQUENCE prints as `\xHH`, the
// same notation the C1 range gets. It reaches here from one direction only —
// the operating system, in a path or a glob pattern, which on POSIX is a byte
// string and carries no encoding guarantee. Ranging over the string instead
// would decode every such byte to U+FFFD and print a pattern the user never
// typed; PROTOCOL.md §1.1(a) forbids exactly that substitution for payload
// bytes, and there is no reason to do to a filename what the specification
// refuses to do to a value.
//
// This is a rendering for reading and is not a decodable channel: a real
// U+0085 and a raw 0x85 both print `\x85`. The `--json` document is where a
// consumer must be able to tell two paths apart, and EncodeJSONPath below is
// what makes that true there.
func Quote(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}

	var b strings.Builder
	b.WriteByte(quote)
	for _, c := range walkUTF8(s) {
		if c.Raw {
			fmt.Fprintf(&b, `\x%02x`, c.Byte)
			continue
		}
		r := c.Rune
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
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// RenderValue renders a decoded JSON value for a diagnostic: strings quoted by
// Quote, containers bracketed, scalars spelled out.
//
// Like Quote, the spelling is the ecosystem's shared message vocabulary rather
// than a free choice — `layer: value 'e2e' is not one of ['unit', ...]` is a
// line CI logs get diffed on, and specguard-rspec builds the same line from its
// own fields. It is a rendering for reading and never has to round-trip.
func RenderValue(v Value) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		return Quote(t)
	case Number:
		// The literal exactly as the document wrote it. Re-deriving it from the
		// parsed float would print 1e2 as 100 and tell the reader about a value
		// that is not in their file.
		return t.Raw
	case []Value:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, RenderValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Object:
		parts := make([]string, 0, len(t.Keys()))
		for _, key := range t.Keys() {
			value, _ := t.Get(key)
			parts = append(parts, Quote(key)+": "+RenderValue(value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprintf("%v", v)
}

// RenderBare is RenderValue with the quotes left off a top-level string.
//
// It is for the places where the surrounding sentence already establishes that
// the value is a name rather than data — "expected type string|array" reads
// worse as `expected type 'string'|'array'`. Containers still render their
// elements, so an enum list still comes out with its members quoted.
func RenderBare(v Value) string {
	if s, ok := v.(string); ok {
		return s
	}
	return RenderValue(v)
}

// CharCount is the length of a string in characters, which is what the
// `minLength` and `maxLength` keywords count.
//
// Bytes would be the wrong unit and the difference is reachable from the
// shipped corpus: `behavior` carries prose with an em dash, so a byte count
// would report a 15-character sentence as 17 and admit a shorter one than the
// schema allows.
func CharCount(s string) int {
	return utf8.RuneCountInString(s)
}

// --------------------------------------------------------------------------- //
// walking a byte string that may not be UTF-8
// --------------------------------------------------------------------------- //

// utf8Chunk is one step of a walk over a byte string: either a decoded rune, or
// a single byte that no well-formed UTF-8 sequence could account for.
type utf8Chunk struct {
	Rune rune
	Byte byte
	Raw  bool
}

// walkUTF8 splits a string into runes, keeping undecodable bytes distinct.
//
// `for _, r := range s` cannot do this: it yields U+FFFD for an undecodable
// byte, which is the same rune it yields for a literal U+FFFD, so the two
// become one and the original byte is gone. Every renderer here that can be
// handed operating-system bytes walks with this instead.
func walkUTF8(s string) []utf8Chunk {
	out := make([]utf8Chunk, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			out = append(out, utf8Chunk{Byte: s[i], Raw: true})
			i++
			continue
		}
		out = append(out, utf8Chunk{Rune: r})
		i += size
	}
	return out
}

// --------------------------------------------------------------------------- //
// the --json document
// --------------------------------------------------------------------------- //

// EncodeJSONString writes a Go string as a JSON string literal per RFC 8259 §7:
// the two mandatory escapes (`"` and `\`), the shorthand escapes for the
// control characters that have one, `\u00XX` for the rest, and every other
// character verbatim as UTF-8.
//
// Non-ASCII characters are NOT escaped. RFC 8259 §8.1 makes UTF-8 the encoding,
// so an em dash in an error message is one em dash in the output — legible in a
// terminal and identical in meaning to `—`. Every JSON parser reads both.
//
// MOST of what reaches here is guaranteed well-formed UTF-8: text the port
// composed from its own source literals, and payload strings, which PROTOCOL.md
// §1.1(a) admits only when they hold real Unicode scalar values. Two kinds of
// string are not, and they are not exotic — a path or glob pattern the OS gave
// us, and an OS error message quoting one. POSIX pathnames are byte strings and
// carry no encoding guarantee.
//
// Such a byte is written `\xHH`, which is four characters of the string's
// VALUE, not a JSON escape — RFC 8259 has none for a byte outside the character
// model, and the document must stay valid UTF-8 for any parser to read it at
// all. What it must not do is REPAIR: substituting U+FFFD is precisely what
// §1.1(a) forbids for a payload byte, and it silently merges two different
// files into one report entry.
//
// The escape makes an error message faithful; it does not make it decodable,
// because prose does not double the backslashes around it. Use EncodeJSONPath
// for a value a consumer has to be able to tell apart from another one.
func EncodeJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range walkUTF8(s) {
		if c.Raw {
			fmt.Fprintf(&b, `\\x%02x`, c.Byte)
			continue
		}
		switch c.Rune {
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
			if c.Rune < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c.Rune)
				continue
			}
			b.WriteRune(c.Rune)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// EncodeJSONPath is EncodeJSONString for a value that IDENTIFIES something —
// the `file` key, which carries a path or the glob pattern that matched
// nothing. It is the one string in the document a consumer may have to compare
// against another, so the encoding it uses is injective: two paths that differ
// by one byte produce two different values, always.
//
// EncodeJSONString alone is not injective, and the gap is reachable rather than
// theoretical. It writes an undecodable 0xE9 as the four characters `\xe9`, and
// it writes a file genuinely NAMED `a\xe9.json` as those same four characters,
// so `--json` reports both as one entry while `summary.files` counts two. That
// is the shape of the defect this key exists not to have: a batch over a spec
// tree whose findings silently collapse.
//
// So a literal backslash is doubled here, and only here. The inverse is the
// obvious one — read `\xHH` as a byte, `\\` as a backslash, everything else as
// itself — and it recovers the operating system's bytes exactly.
//
// The price is a path containing a literal backslash being reported with it
// doubled. That is the whole cost, it is confined to this key, and it buys the
// property the key is for; a report that cannot name two files apart is worse
// than one that names an unusual file verbosely.
func EncodeJSONPath(s string) string {
	return EncodeJSONString(strings.ReplaceAll(s, `\`, `\\`))
}

// --------------------------------------------------------------------------- //
// lines
// --------------------------------------------------------------------------- //

// SplitLines splits source text into lines on LF, CRLF and a lone CR, dropping
// the terminators. A trailing terminator does not produce a final empty line,
// and "" is zero lines rather than one empty one.
//
// Those three are what a line ending is in a text file, and `--source` reports
// every finding as `file:line` — a number a reader counts to in their editor.
// Splitting on anything more exotic (a form feed, U+2028) would report a line
// number their editor disagrees with, which is the worst shape of failure this
// mode has: specific, confident, and pointing at innocent code.
func SplitLines(s string) []string {
	lines := []string{}
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\n' && runes[i] != '\r' {
			continue
		}
		lines = append(lines, string(runes[start:i]))
		if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
			i++ // CRLF is one terminator, not two
		}
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

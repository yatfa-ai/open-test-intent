package main

// The JSON Schema `pattern` keyword: Python `re` semantics on top of Go's RE2.
//
// The reference evaluates a pattern with `re.search(schema["pattern"], instance)`
// (bin/validate-intent:205). Go's regexp is RE2, and the two engines are not the
// same language even where both compile the same source text:
//
//	pattern     value    Python  Go RE2
//	^[a-z]+$    "abc\n"  match   no match   -- Python's `$` also matches before a
//	                                           trailing newline; RE2's does not
//	^\d+$       "٣٤"     match   no match   -- \d is Unicode-aware in Python 3,
//	^\w+$       "café"   match   no match      ASCII-only in RE2
//	[[:alpha:]] "a"      no      match      -- POSIX classes are RE2-only; Python
//	                                           reads the inner [ : a l p h ] as a
//	                                           character class of literals
//	\p{L}       "a"      error   match      -- Unicode classes are RE2-only
//	a{,3}       "a"      match   no match   -- Python reads {,3} as {0,3}, RE2 as
//	                                           three literal characters
//
// Every one of those compiles cleanly in Go, so a compile check cannot see them.
// They are silent and verdict-changing: a document the reference passes, the
// port fails. That is this project's signature defect class (a check that
// reports clean having verified less than it claims), so the port takes the
// opposite default.
//
// # The rule: allow-list, not deny-list
//
// translatePattern walks the pattern and accepts only constructs whose meaning
// is *provably identical* in both engines. Anything it does not recognise is
// REFUSED at schema-load time with exit 2 and a diagnostic naming the construct
// — the same treatment recursive `**` globs get. A deny-list would inherit the
// bug it is fixing: it can only reject the divergences someone thought of, and
// the next one (`[[:alpha:]]`, `\p{L}`, `{,3}`) sails through.
//
// Refusing is safe in the direction that matters. A construct wrongly refused
// is a loud error a human fixes in a minute; a construct wrongly accepted is a
// wrong verdict nobody notices.
//
// # The one translation
//
// A trailing `$` is rewritten to `(?:\n?\z)`. Python's `$` (no re.MULTILINE)
// matches "at the end of the string or just before the newline at the end of
// the string", which is exactly that expression. It is applied ONLY when `$` is
// the final character of the pattern, because the rewrite consumes the newline
// where the anchor is zero-width — indistinguishable for a match/no-match test
// at the end of a pattern, but not if anything follows it (`a$\n` matches "a\n"
// in Python; `a(?:\n?\z)\n` cannot). A `$` anywhere else is refused.
//
// Anchoring at the end is the single most common shape a `pattern` takes, and
// there is no portable spelling for it: Python rejects `\z` and Go rejects `\Z`.
// Refusing `$` outright would have left the keyword with no usable end anchor,
// which is why this one construct is translated rather than refused.
//
// # Compiled once
//
// CompileSchema compiles every pattern in the schema at load time and hands the
// *regexp.Regexp forward on the Schema. Validate looks the pattern up rather
// than recompiling per string checked, so there is exactly one place a pattern
// can be rejected and no "compile failed, skip the check" branch for a failure
// to hide in.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PatternSet maps each `pattern` string in a schema to the compiled Go regexp
// that reproduces Python's meaning for it.
type PatternSet map[string]*regexp.Regexp

// Schema is a decoded schema plus everything precomputed from it.
type Schema struct {
	Root     Value
	Patterns PatternSet
}

// CompileSchema prepares a decoded schema for validation, refusing it outright
// if it carries a `pattern` the port cannot reproduce faithfully.
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
// It deliberately does not try to work out which of those keys are really
// draft-07 `pattern` keywords and which are, say, a property named "pattern"
// inside a `default` value. Over-approximating costs at most a spurious refusal
// on an exotic schema; under-approximating would let a real pattern reach
// Validate uncompiled, which is the failure this whole file exists to prevent.
func collectPatterns(node Value, into PatternSet) error {
	switch typed := node.(type) {
	case *Object:
		for _, key := range typed.Keys() {
			child, _ := typed.Get(key)
			if pattern, isString := child.(string); isString && key == "pattern" {
				if _, done := into[pattern]; !done {
					compiled, err := CompilePythonPattern(pattern)
					if err != nil {
						return err
					}
					into[pattern] = compiled
				}
			}
			if err := collectPatterns(child, into); err != nil {
				return err
			}
		}
	case []Value:
		for _, child := range typed {
			if err := collectPatterns(child, into); err != nil {
				return err
			}
		}
	}
	return nil
}

// CompilePythonPattern translates a Python `re` pattern to its exact RE2
// equivalent and compiles it, or explains why the port will not run it.
func CompilePythonPattern(pattern string) (*regexp.Regexp, error) {
	translated, err := translatePattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("the Go port cannot faithfully reproduce the pattern %s: %s",
			PyReprString(pattern), err)
	}
	compiled, err := regexp.Compile(translated)
	if err != nil {
		// Reachable for constructs the allow-list permits but RE2 still refuses
		// — a repetition count over 1000, a `{2}` with nothing to repeat.
		return nil, fmt.Errorf("the Go port cannot compile the pattern %s (RE2): %s",
			PyReprString(pattern), err)
	}
	return compiled, nil
}

// translatePattern returns the RE2 source that means, exactly, what pattern
// means to Python's `re.search`, or an error naming the construct that stopped
// it. See the file comment for the reasoning behind the allow-list.
func translatePattern(pattern string) (string, error) {
	runes := []rune(pattern)
	var out strings.Builder

	for i := 0; i < len(runes); {
		switch r := runes[i]; r {

		// `^` — start of text in both engines without MULTILINE/(?m).
		// `.` — any character except newline in both.
		// `*`, `+`, `?`, `|`, `)` — identical, including the lazy `*?` forms,
		// which arrive here as two separate tokens.
		case '^', '.', '*', '+', '?', '|', ')':
			out.WriteRune(r)
			i++

		case '$':
			if i != len(runes)-1 {
				return "", errors.New(
					"`$` is only supported as the very last character of a pattern, because " +
						"Python's `$` also matches just before a trailing newline and the port " +
						"rewrites it to `(?:\\n?\\z)`, which consumes that newline instead of " +
						"anchoring at it — indistinguishable at the end of a pattern, not " +
						"before anything else. Write `\\$` for a literal dollar sign")
			}
			out.WriteString(`(?:\n?\z)`)
			i++

		case '(':
			// `(?:` is the only extended group both engines spell the same way
			// and mean the same by. `(?i)`, `(?P<n>...)`, `(?=...)`, `(?#...)`
			// are each either RE2-only, Python-only, or subtly different.
			if i+1 < len(runes) && runes[i+1] == '?' {
				if i+2 < len(runes) && runes[i+2] == ':' {
					out.WriteString("(?:")
					i += 3
					continue
				}
				return "", errors.New(
					"`(?` groups other than the non-capturing `(?:` are not supported: " +
						"inline flags, named groups, lookaround and comments are spelled " +
						"differently — or mean different things — in Python and Go's RE2")
			}
			out.WriteRune('(')
			i++

		case '{':
			// `{m}`, `{m,}` and `{m,n}` agree. `{,n}` does not: Python reads it
			// as `{0,n}`, RE2 as three literal characters.
			length, ok := repetitionLength(runes, i)
			if !ok {
				return "", errors.New(
					"`{` is only supported as a `{m}`, `{m,}` or `{m,n}` repetition: Python " +
						"reads `{,n}` as `{0,n}` while Go's RE2 reads it as literal text. " +
						"Write `\\{` for a literal brace")
			}
			out.WriteString(string(runes[i : i+length]))
			i += length

		case '[':
			length, text, err := translateClass(runes, i)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
			i += length

		case '\\':
			length, text, err := translateEscape(runes, i, false)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
			i += length

		case ']', '}':
			// Unmatched closers are literals in Python and in RE2 alike, but
			// only because nothing opened them — which the cases above already
			// guarantee. Emitting them escaped keeps that explicit.
			out.WriteRune('\\')
			out.WriteRune(r)
			i++

		default:
			// Everything else is an ordinary literal character. The two engines
			// have the same metacharacter set, all of which is handled above,
			// so no rune reaching here can be special in one and not the other.
			out.WriteRune(r)
			i++
		}
	}
	return out.String(), nil
}

// translateClass handles a `[...]` character class, returning the runes
// consumed and the RE2 text to emit.
func translateClass(runes []rune, start int) (int, string, error) {
	var out strings.Builder
	out.WriteRune('[')

	i := start + 1
	if i < len(runes) && runes[i] == '^' {
		out.WriteRune('^')
		i++
	}

	for first := true; i < len(runes); first = false {
		switch r := runes[i]; {

		case r == ']' && first:
			// Python treats a `]` in first position as a member; RE2 treats it
			// as the terminator and then reports the class as unclosed.
			return 0, "", errors.New(
				"a `]` as the first member of a character class is a literal in Python but " +
					"closes the class in Go's RE2 — write it as `\\]`")

		case r == ']':
			out.WriteRune(']')
			return i + 1 - start, out.String(), nil

		case r == '[':
			// Literal in Python; in RE2 a `[:...:]` POSIX class.
			return 0, "", errors.New(
				"a `[` inside a character class is a literal in Python but can open a POSIX " +
					"class such as `[:alpha:]` in Go's RE2 — write it as `\\[`")

		case r == '\\':
			length, text, err := translateEscape(runes, i, true)
			if err != nil {
				return 0, "", err
			}
			out.WriteString(text)
			i += length

		default:
			// Literal members and `a-z` ranges. Both engines compare by code
			// point, so these agree.
			out.WriteRune(r)
			i++
		}
	}
	return 0, "", errors.New("the character class is never closed with a `]`")
}

// translateEscape handles a backslash escape, returning the runes consumed and
// the RE2 text to emit. inClass narrows what is legal to a class member.
func translateEscape(runes []rune, i int, inClass bool) (int, string, error) {
	if i+1 >= len(runes) {
		return 0, "", errors.New("the pattern ends with a lone `\\`")
	}
	c := runes[i+1]

	switch {
	case c >= '0' && c <= '9':
		return 0, "", fmt.Errorf(
			"`\\%c` is a backreference or an octal escape in Python and neither in Go's RE2",
			c)

	case c == 'x':
		// `\xHH` agrees. Go's `\x{...}` form is not Python syntax, and Python's
		// `\uXXXX`/`\UXXXXXXXX` are not RE2 syntax.
		if i+3 >= len(runes) || !isHexDigit(runes[i+2]) || !isHexDigit(runes[i+3]) {
			return 0, "", errors.New(
				"`\\x` is only supported as `\\xHH` with exactly two hex digits; Go's " +
					"`\\x{...}` form is not Python syntax")
		}
		return 4, string(runes[i : i+4]), nil

	case c == 'a' || c == 'f' || c == 'n' || c == 'r' || c == 't' || c == 'v':
		// Identical control-character escapes, in and out of a class.
		return 2, string(runes[i : i+2]), nil

	case c == 'A':
		if inClass {
			return 0, "", errors.New("`\\A` is not a valid character-class member in either engine")
		}
		return 2, `\A`, nil

	case isASCIILetter(c):
		return 0, "", fmt.Errorf("`\\%c` is not supported: %s", c, escapeReason(c))

	case c < 0x80:
		// Escaped ASCII punctuation is the literal character in both engines
		// (Python: anything not an ASCII letter or digit; RE2: anything not
		// alphanumeric).
		return 2, string(runes[i : i+2]), nil

	default:
		// Python takes `\é` as a literal `é`; RE2 rejects the escape outright.
		// Drop the backslash and it means the same thing in both.
		return 0, "", fmt.Errorf(
			"`\\%c` escapes a non-ASCII character, which Go's RE2 rejects — drop the backslash",
			c)
	}
}

// escapeReason explains why a given `\<letter>` escape is refused. Each one is
// a real divergence, not a conservatism.
func escapeReason(c rune) string {
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		return "Python 3 makes it Unicode-aware while Go's RE2 makes it ASCII-only " +
			"(`\\d` is `[0-9]`, `\\w` is `[0-9A-Za-z_]`), so the two engines disagree on " +
			"any non-ASCII input. Spell the intended set out as an explicit character class"
	case 'b', 'B':
		return "Python's word boundary is Unicode-aware and Go's RE2 is ASCII-only, and " +
			"inside a character class Python reads `\\b` as a backspace instead"
	case 'Z':
		return "Python's `\\Z` (end of string) is spelled `\\z` in Go's RE2, which Python " +
			"in turn rejects; end the pattern with `$` instead"
	case 'z':
		return "`\\z` is Go's RE2 spelling; Python rejects it and spells end-of-string " +
			"`\\Z`. End the pattern with `$` instead"
	case 'p', 'P':
		return "`\\p{...}` Unicode classes are Go's RE2 only; Python rejects the escape"
	case 'Q', 'E', 'C', 'G', 'K', 'R', 'X':
		return "it is not valid in Python's `re`"
	case 'N', 'u', 'U':
		return "it is Python-only syntax that Go's RE2 does not accept"
	default:
		return "the two engines do not agree on what it means"
	}
}

// repetitionLength reports the length of a `{m}` / `{m,}` / `{m,n}` repetition
// starting at runes[start], and whether one is there at all.
func repetitionLength(runes []rune, start int) (int, bool) {
	i := start + 1
	digits := 0
	for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
		i++
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(runes) && runes[i] == ',' {
		i++
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
	}
	if i < len(runes) && runes[i] == '}' {
		return i + 1 - start, true
	}
	return 0, false
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

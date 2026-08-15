package main

// In-source annotation extraction and permissive-syntax normalization.
//
// This is the half of the validator that reads *test source* (.rb/.py/.js/...)
// rather than standalone JSON: it finds each `@intent:` token, captures the
// object literal after it brace-balanced, relaxes PROTOCOL.md §1's permissive
// surface syntax into the strict JSON §1.1 requires, and validates the result —
// reporting every finding at its `file:line`.
//
// CHARACTERS, NOT BYTES.
// ----------------------
// Every scanner in this file operates on []rune, never on a string.
//
// The sharp edge is scanString's `i += 2`, which steps over a backslash and the
// one character it escapes. Over bytes that is one BYTE, so a backslash
// immediately before a multi-byte character lands the scanner mid-rune and
// every subsequent offset is wrong — a payload silently truncated or extended,
// and reported with total confidence.
//
// HONEST SCOPE NOTE: this is *latent* on the shipped corpus. No backslash
// directly precedes a multi-byte character in examples/sources/, so a
// byte-indexed scanner would pass the whole fixture suite today. It is settled
// anyway: examples/sources/order_spec.rb:57 proves non-ASCII does reach these
// functions, so the distance between "latent" and "live" is one commit by
// someone who has never read this comment.
// TestScanString_backslashBeforeMultibyte pins it.
//
// Working on []rune costs one conversion per line and buys the property that
// every index in this file counts the same thing a reader counts.

import (
	"fmt"
	"strings"
	"unicode"
)

// intentToken is the marker PROTOCOL.md §1 puts in front of an annotation.
const intentToken = "@intent:"

// --------------------------------------------------------------------------- //
// _scan_string / _scan_object
// --------------------------------------------------------------------------- //

// scanString consumes a quoted string.
//
// text[start] must be the opening quote; the returned index is just past the
// closing one. Backslash escapes are honoured, so a quote or brace *inside* the
// string never terminates the scan.
func scanString(text []rune, start int, quote rune) (int, error) {
	i := start + 1
	length := len(text)
	for i < length {
		char := text[i]
		if char == '\\' {
			// One CODE POINT of escape, not one byte. See the file comment.
			i += 2
			continue
		}
		if char == quote {
			return i + 1, nil
		}
		i++
	}
	return 0, fmt.Errorf("unterminated %c-quoted string", quote)
}

// openers maps each closing bracket to the opener it must match.
var openers = map[rune]rune{'}': '{', ']': '['}

// scanObject captures the object literal after an @intent: token.
//
// Bracket-balanced and string-aware, so a brace inside a quoted value does not
// end the payload early.
//
// This is the thin ice of the whole extractor, and the shipped corpus exercises it:
// examples/sources/order_spec.rb:57 carries trailing prose containing its own
// brace pair (`— see ADR-14 {§3}`). A greedy regex (`\{.*\}`) captures through
// that pair and fails; a lazy one truncates the payload mid-object. Only a
// balanced scan stops at the *matching* `}`.
func scanObject(text []rune, start int) (int, error) {
	stack := []rune{}
	i := start
	length := len(text)
	for i < length {
		char := text[i]
		if char == '"' || char == '\'' {
			end, err := scanString(text, i, char)
			if err != nil {
				return 0, err
			}
			i = end
			continue
		}
		switch char {
		case '{', '[':
			stack = append(stack, char)
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != openers[char] {
				return 0, fmt.Errorf("unbalanced brackets in the annotation payload")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unterminated object literal (an annotation must fit on one line)")
}

// --------------------------------------------------------------------------- //
// extract_intents
// --------------------------------------------------------------------------- //

// IntentSite is one `@intent:` occurrence found in a file.
//
// Problem is empty on success and Raw holds the object literal exactly as
// written; on failure Raw is empty and Problem says why the payload could not be
// captured. A token with no extractable payload is REPORTED, not skipped, so a
// typo'd annotation fails loudly rather than silently counting as "unannotated"
// — the vacuous-green rule this project names (SPGD-78) applied one level down.
type IntentSite struct {
	Line    int
	Raw     string
	Problem string
}

// ExtractIntents finds every @intent: site in a file's text.
//
// KNOWN LIMITATION, ACCEPTED DELIBERATELY: this cannot distinguish an `@intent:`
// inside a string literal from one in a real comment. A Ruby line
// `x = '# @intent: {...}'` is extracted and validated exactly as the same
// annotation in a comment would be.
//
// Telling them apart needs a parser for every language a test can be written
// in, which is the coupling PROTOCOL.md §6 rules out — the annotation is
// deliberately readable without one. Narrowing it to "only in a comment" would
// therefore mean guessing at comment syntax per file extension and being wrong
// about the next language. A phantom annotation is loud (it is validated, and a
// bad one fails) where a missed one is silent, so the scanner errs toward
// finding too many. source_test.go pins both the phantom case and a
// real-comment positive control so the choice stays deliberate rather than
// becoming an accident nobody remembers.
func ExtractIntents(text string) []IntentSite {
	sites := []IntentSite{}
	// Line numbers are the whole point of this mode. See SplitLines.
	for lineNo, line := range SplitLines(text) {
		sites = append(sites, extractFromLine([]rune(line), lineNo+1)...)
	}
	return sites
}

func extractFromLine(line []rune, lineNo int) []IntentSite {
	sites := []IntentSite{}
	pos := 0
	for {
		tokenAt := runeIndex(line, intentToken, pos)
		if tokenAt == -1 {
			return sites
		}
		// The payload search is BOUNDED BY THE NEXT @intent: TOKEN on the line.
		//
		// Unbounded, it reads to end of line, so a malformed token followed by a
		// well-formed one adopts its neighbour's object literal: the typo'd token
		// is reported as VALID carrying an intent its author never wrote, and
		// because `pos` then resumes past the captured payload the real
		// annotation is swallowed on the way. One false green, one silent skip,
		// from a single unbounded read. This tool exists to report a bad
		// annotation loudly — a false red costs a reader one glance, a false
		// green costs them the guarantee — so a line that is genuinely ambiguous
		// about which token owns the literal takes the no-payload path instead of
		// guessing.
		//
		// It also keeps the two backends in agreement: the gem's scanner bounds
		// the same search (Specguard::RSpec::AnnotationScanner#payload_brace), and
		// this binary is the canonical validator the gem can be pointed at, so an
		// unbounded read here means the same file gets two verdicts depending on
		// which backend the user opted into — with the opted-in one permissive.
		//
		// The bound can only ever SHRINK the search. It never picks a different
		// brace, only declines one, so every line whose token owns the brace that
		// follows it is unaffected.
		//
		// runeIndex, not strings.Index: this is a code-point offset like every
		// other index in this file, and intentToken is ASCII so len() is the same
		// count in both — matching what the braceAt search below already relies on.
		nextToken := runeIndex(line, intentToken, tokenAt+len(intentToken))
		braceAt := runeIndex(line, "{", tokenAt+len(intentToken))
		if braceAt == -1 || (nextToken != -1 && braceAt > nextToken) {
			return append(sites, IntentSite{Line: lineNo,
				Problem: "no '{...}' object literal follows the @intent: token"})
		}
		end, err := scanObject(line, braceAt)
		if err != nil {
			return append(sites, IntentSite{Line: lineNo, Problem: err.Error()})
		}
		sites = append(sites, IntentSite{Line: lineNo, Raw: string(line[braceAt:end])})
		pos = end
	}
}

// runeIndex is str.find(needle, start) over code points: it returns a CODE POINT
// offset, which is what every other index in this file is. strings.Index would
// return a byte offset, and mixing the two is exactly divergence 4.
func runeIndex(hay []rune, needle string, from int) int {
	pat := []rune(needle)
	if from < 0 {
		from = 0
	}
	for i := from; i+len(pat) <= len(hay); i++ {
		match := true
		for j, r := range pat {
			if hay[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// --------------------------------------------------------------------------- //
// _requote / normalize_payload
// --------------------------------------------------------------------------- //

// requote re-emits the body of a single-quoted string as a double-quoted JSON
// string — PROTOCOL.md §1's "may use single or double quotes".
func requote(body []rune) string {
	var out strings.Builder
	i := 0
	length := len(body)
	for i < length {
		char := body[i]
		if char == '\\' {
			var next rune = -1 // sentinel for "past the end"
			if i+1 < length {
				next = body[i+1]
			}
			// \' is meaningless in JSON — unescape it; keep every other escape.
			if next == '\'' {
				out.WriteRune('\'')
			} else {
				out.WriteRune(char)
				if next != -1 {
					out.WriteRune(next)
				}
			}
			i += 2
			continue
		}
		if char == '"' {
			out.WriteString(`\"`)
		} else {
			out.WriteRune(char)
		}
		i++
	}
	return `"` + out.String() + `"`
}

// NormalizePayload relaxes PROTOCOL.md §1's permissive annotation syntax into
// the strict JSON §1.1 requires.
//
//	unquoted keys          {entity:"Order"}   -> {"entity":"Order"}
//	single-quoted strings  {'entity':'Order'} -> {"entity":"Order"}
//	trailing comma         {"a":1,}           -> {"a":1}
//
// It is a character scanner, not a regex substitution, so quoted content is
// never rewritten — a behavior sentence containing `it's` or `{` passes through
// untouched.
func NormalizePayload(raw []rune) (string, error) {
	var out strings.Builder
	i := 0
	length := len(raw)
	for i < length {
		char := raw[i]

		if char == '"' {
			end, err := scanString(raw, i, '"')
			if err != nil {
				return "", err
			}
			out.WriteString(string(raw[i:end]))
			i = end
			continue
		}

		if char == '\'' {
			end, err := scanString(raw, i, '\'')
			if err != nil {
				return "", err
			}
			out.WriteString(requote(raw[i+1 : end-1]))
			i = end
			continue
		}

		if word, after := matchBareWord(raw, i); after > i {
			probe := after
			for probe < length && unicode.IsSpace(raw[probe]) {
				probe++
			}
			// Quote a bare word only in *key* position. A bare word used as a
			// value (`{layer: request}`) is left alone so it still fails — the
			// protocol relaxes keys and quote style, not value quoting.
			if probe < length && raw[probe] == ':' {
				out.WriteString(EncodeJSONString(word))
			} else {
				out.WriteString(word)
			}
			i = after
			continue
		}

		if char == ',' {
			probe := i + 1
			for probe < length && unicode.IsSpace(raw[probe]) {
				probe++
			}
			if probe < length && (raw[probe] == '}' || raw[probe] == ']') {
				i++ // drop the trailing comma
				continue
			}
		}

		out.WriteRune(char)
		i++
	}
	return out.String(), nil
}

// matchBareWord matches an unquoted key, anchored at i:
//
//	[A-Za-z_$][A-Za-z0-9_$]*
//
// ASCII, and hand-coded rather than compiled, so there is no regex dialect to
// disagree about: `café` matches only `caf`, leaving `é` outside the match, in
// this implementation and in any client that reads this comment. Widening it to
// a Unicode word class would make the accepted surface syntax depend on which
// engine a client happens to use, which is exactly the drift one specification
// exists to prevent.
func matchBareWord(raw []rune, i int) (word string, after int) {
	isStart := func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || r == '$'
	}
	if i >= len(raw) || !isStart(raw[i]) {
		return "", i
	}
	end := i + 1
	for end < len(raw) {
		r := raw[end]
		if !isStart(r) && !(r >= '0' && r <= '9') {
			break
		}
		end++
	}
	return string(raw[i:end]), end
}

// --------------------------------------------------------------------------- //
// check_source_file
// --------------------------------------------------------------------------- //

// SourceFinding is one annotation's result.
//
// Kind names *how* the annotation failed and is empty when it passed. It is
// carried here rather than reconstructed by the caller because a failed
// extraction and an unparseable payload both land in Problem, so the two become
// indistinguishable the moment the tuple is flattened to prose — which is
// precisely what the --json renderer must not do.
type SourceFinding struct {
	Line    int
	Valid   bool
	Errors  []string
	Problem string
	Kind    string
}

// CheckSourceFile extracts, normalizes and validates every annotation in a file.
//
// NOTE THE SHAPE: it returns (findings, readError). CheckFile returns a 4-tuple
// carrying its own parse/read distinction. They look similar and are not;
// conflating them is the easiest mistake available here.
//
// findings is nil when readError is set. Otherwise it holds one entry per
// annotation found, in source order — and an EMPTY list means the file carries
// none, which is not a failure (the protocol allows unannotated tests).
func CheckSourceFile(path string, schema *Schema) (findings []SourceFinding, readError string) {
	text, readErr := readSourceText(path)
	if readErr != "" {
		return nil, readErr
	}
	return CheckSourceText(text, schema), ""
}

// CheckSourceText is CheckSourceFile with the read already done: extraction,
// normalization and validation over source text from wherever it came. Split
// out for the self-test's embedded corpus — see fixtureSource in selftest.go —
// so an installed binary and a checkout run the SAME extractor over the same
// bytes rather than two that have to be kept in agreement.
func CheckSourceText(text string, schema *Schema) []SourceFinding {
	findings := []SourceFinding{}
	for _, site := range ExtractIntents(text) {
		if site.Problem != "" {
			findings = append(findings, SourceFinding{
				Line: site.Line, Valid: false, Errors: nil,
				Problem: site.Problem, Kind: KindExtraction,
			})
			continue
		}
		instance, err := normalizeAndParse(site.Raw)
		if err != nil {
			// One branch for both halves of "this payload is not JSON": the
			// normalizer's own "unterminated ...-quoted string" and the
			// parser's syntax errors. A reader fixing either does the same
			// thing — look at the payload — so they share a prefix and a kind.
			findings = append(findings, SourceFinding{
				Line: site.Line, Valid: false, Errors: nil,
				Problem: "could not parse annotation: " + err.Error(), Kind: KindParse,
			})
			continue
		}
		errs := schema.Validate(instance)
		finding := SourceFinding{Line: site.Line, Valid: len(errs) == 0, Errors: errs}
		if len(errs) > 0 {
			finding.Kind = KindSchema
		}
		findings = append(findings, finding)
	}
	return findings
}

func normalizeAndParse(raw string) (Value, error) {
	normalized, err := NormalizePayload([]rune(raw))
	if err != nil {
		return nil, err
	}
	return DecodeJSONString(normalized)
}

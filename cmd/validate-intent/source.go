package main

// In-source annotation extraction and permissive-syntax normalization —
// the port of bin/validate-intent:255-454.
//
// This is the half of the validator that reads *test source* (.rb/.py/.js/...)
// rather than standalone JSON: it finds each `@intent:` token, captures the
// object literal after it brace-balanced, relaxes the protocol's permissive
// syntax into strict JSON, and validates the result — reporting every finding at
// its `file:line`.
//
// DIVERGENCE 4 — code-point indexing vs byte indexing.
// ---------------------------------------------------
// Every offset in the reference is an index into a Python `str`, which is a
// sequence of CODE POINTS. `text[i]` in Go is a BYTE. The two agree only while
// the input is ASCII.
//
// DECISION: every scanner in this file operates on []rune, never on a string.
//
// The sharp edge is _scan_string's `i += 2` (bin/validate-intent:275), which
// steps over a backslash and the one character it escapes. In Python that is
// always exactly one escaped character; over bytes it is one byte, so a
// backslash immediately before a multi-byte character lands the scanner
// mid-rune and every subsequent offset is wrong — a payload silently truncated
// or extended, reported with total confidence.
//
// HONEST SCOPE NOTE: this is *latent* on the shipped corpus. No backslash
// directly precedes a multi-byte character in examples/sources/, so a
// byte-indexed port would pass the whole fixture suite today. It is settled
// anyway, for the same reason slice 1 settled the unexercised `pattern`
// keyword: examples/sources/order_spec.rb:57 proves non-ASCII does reach these
// functions (18 code points / 19 UTF-8 bytes on the ticket's probe), so the
// distance between "latent" and "live" is one commit by someone who has never
// read this comment. TestScanString_backslashBeforeMultibyte pins it, and the
// parity harness diffs a fixture built for it.
//
// Working on []rune costs one conversion per line and buys the property that
// every index in this file means the same thing it means in the reference.

import (
	"fmt"
	"strings"
)

// intentToken is the reference's INTENT_TOKEN (bin/validate-intent:258).
const intentToken = "@intent:"

// --------------------------------------------------------------------------- //
// _scan_string / _scan_object
// --------------------------------------------------------------------------- //

// scanString is the port of `_scan_string` (bin/validate-intent:264-280).
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
			// DIVERGENCE 4 lives here: one code point of escape, not one byte.
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

// openers is the reference's _OPENERS (bin/validate-intent:261).
var openers = map[rune]rune{'}': '{', ']': '['}

// scanObject is the port of `_scan_object` (bin/validate-intent:283-307).
//
// Bracket-balanced and string-aware, so a brace inside a quoted value does not
// end the payload early.
//
// This is the roadmap's named "thin ice", and the shipped corpus exercises it:
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
// It is the reference's 3-tuple `(line_no, raw_payload, problem)`
// (bin/validate-intent:310-336) — note the *three* elements. Slice 1's
// check_file returns a 4-tuple with an entirely different meaning; the shapes do
// not correspond and are not interchangeable.
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

// ExtractIntents is the port of `extract_intents` (bin/validate-intent:310-336).
//
// KNOWN DEFECT, REPRODUCED DELIBERATELY: this cannot distinguish an `@intent:`
// inside a string literal from one in a real comment. A Ruby line
// `x = '# @intent: {...}'` is extracted and validated exactly as the same
// annotation in a comment would be — byte-identical output, exit 0 either way.
// That identity IS the defect. Byte-for-byte parity means the port inherits it:
// "fixing" it here would fail the parity harness against the reference, and
// changing the behaviour is a protocol-level decision for bin/validate-intent
// first. tests/parity/run_parity.sh pins both the phantom case and a
// real-comment positive control so the reproduction stays deliberate rather than
// becoming an accident nobody remembers.
func ExtractIntents(text string) []IntentSite {
	sites := []IntentSite{}
	// DIVERGENCE 1: str.splitlines(), not strings.Split(text, "\n"). Line
	// numbers are the whole point of this mode. See pystr.go.
	for lineNo, line := range pySplitlines(text) {
		sites = append(sites, extractFromLine(pyRunes(line), lineNo+1)...)
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
		braceAt := runeIndex(line, "{", tokenAt+len(intentToken))
		if braceAt == -1 {
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

// requote is the port of `_requote` (bin/validate-intent:339-354): re-emit the
// body of a single-quoted string as a double-quoted JSON string.
func requote(body []rune) string {
	var out strings.Builder
	i := 0
	length := len(body)
	for i < length {
		char := body[i]
		if char == '\\' {
			var next rune = -1 // Python's "" sentinel for "past the end"
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

// NormalizePayload is the port of `normalize_payload`
// (bin/validate-intent:357-418): relax the protocol's permissive annotation
// syntax into strict JSON.
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
			// DIVERGENCE 2: str.isspace(), which also answers True for
			// U+001C-U+001F. Go's unicode.IsSpace does not. See pystr.go.
			for probe < length && pyIsSpace(raw[probe]) {
				probe++
			}
			// Quote a bare word only in *key* position. A bare word used as a
			// value (`{layer: request}`) is left alone so it still fails — the
			// protocol relaxes keys and quote style, not value quoting.
			if probe < length && raw[probe] == ':' {
				// DIVERGENCE 3: json.dumps' escaping rules, not Go's. See
				// pystr.go for why neither encoding/json default matches.
				out.WriteString(pyJSONDumpsString(word))
			} else {
				out.WriteString(word)
			}
			i = after
			continue
		}

		if char == ',' {
			probe := i + 1
			for probe < length && pyIsSpace(raw[probe]) {
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

// matchBareWord is _BARE_WORD_RE anchored at i (bin/validate-intent:260):
//
//	[A-Za-z_$][A-Za-z0-9_$]*
//
// RULED OUT, deliberately: the usual "Python's \w is Unicode-aware, Go's RE2 is
// ASCII-only" divergence does NOT apply here. This class is spelled out in
// explicit ASCII ranges in the reference, so both engines agree, and it is
// hand-coded rather than compiled so there is no regex dialect to disagree
// about at all. `café` therefore matches only `caf` in *both* implementations —
// which is what makes the divergence-3 fixture behave the way it does.
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

// SourceFinding is one entry of the reference's findings list
// (bin/validate-intent:421-454): `(line_no, valid, errors, problem, kind)`.
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

// CheckSourceFile is the port of `check_source_file` (bin/validate-intent:421-454).
//
// NOTE THE SHAPE: it returns (findings, readError) — a 2-tuple. Slice 1's
// CheckFile returns a 4-tuple carrying its own parse/read distinction. They look
// similar and are not; conflating them is the easiest mistake available here.
//
// findings is nil when readError is set. Otherwise it holds one entry per
// annotation found, in source order — and an EMPTY list means the file carries
// none, which is not a failure (the protocol allows unannotated tests).
func CheckSourceFile(path string, schema *Schema) (findings []SourceFinding, readError string) {
	text, readErr := readSourceText(path)
	if readErr != "" {
		return nil, readErr
	}

	findings = []SourceFinding{}
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
			// Python catches ValueError here, which covers BOTH the
			// normalizer's own "unterminated ...-quoted string" and
			// json.JSONDecodeError — they are one branch, reported with one
			// prefix, and both land in KIND_PARSE.
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
	return findings, ""
}

func normalizeAndParse(raw string) (Value, error) {
	normalized, err := NormalizePayload(pyRunes(raw))
	if err != nil {
		return nil, err
	}
	return DecodeOrderedString(normalized)
}

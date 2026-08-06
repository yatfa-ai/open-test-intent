package main

// Tests for pystr.go: the Python `str` semantics the port had to reproduce.
//
// The three enumerations here (line terminators, isspace, json.dumps escaping)
// are not asserted against a list somebody typed from documentation — they are
// asserted against CPython itself, over the whole of range(0x110000). That
// matters in both directions: it catches a set transcribed wrong today, and it
// catches Go's Unicode tables drifting apart from Python's tomorrow.
//
// Skipped, loudly, when python3 is unavailable: an oracle-less differential test
// has verified nothing and must not read as a pass.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func runPython(t *testing.T, script string) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	path := filepath.Join(t.TempDir(), "probe.py")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing probe: %v", err)
	}
	out, err := exec.Command(python, path).Output()
	if err != nil {
		t.Fatalf("python probe failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// parseCodePoints reads the "cp,cp,cp" the enumeration probes emit.
func parseCodePoints(t *testing.T, s string) map[rune]bool {
	t.Helper()
	set := map[rune]bool{}
	if s == "" {
		return set
	}
	for _, field := range strings.Split(s, ",") {
		n, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("bad code point %q from the probe: %v", field, err)
		}
		set[rune(n)] = true
	}
	return set
}

// pyList renders Go strings as a Python list literal, so a probe can be handed
// exactly the inputs the Go side is about to run.
//
// It uses pyJSONDumpsString, which is itself under test here. That is safe
// precisely because the assertions compare against python3's own json.dumps of
// the same data: a wrong encoder makes the comparison FAIL rather than agree
// with itself, since the oracle is external.
func pyList(items []string) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, pyJSONDumpsString(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func pyListOfLists(lists [][]string) string {
	parts := make([]string, 0, len(lists))
	for _, list := range lists {
		parts = append(parts, pyList(list))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// --------------------------------------------------------------------------- //
// DIVERGENCE 1 — str.splitlines()
// --------------------------------------------------------------------------- //

func TestPyLineTerminator_matchesCPython(t *testing.T) {
	got := parseCodePoints(t, runPython(t, `
terminators = [c for c in range(0x110000) if len(("a" + chr(c) + "b").splitlines()) > 1]
print(",".join(str(c) for c in terminators))
`))
	if len(got) == 0 {
		t.Fatal("the probe returned no terminators — it verified nothing")
	}
	for c := rune(0); c < 0x110000; c++ {
		if pyLineTerminator(c) != got[c] {
			t.Errorf("pyLineTerminator(U+%04X) = %v, CPython splitlines() says %v",
				c, pyLineTerminator(c), got[c])
		}
	}
}

// TestPySplitlines_matchesCPython checks the SPLITTING, not just the set:
// "\r\n" as one terminator, no trailing empty element, and "" -> zero lines.
func TestPySplitlines_matchesCPython(t *testing.T) {
	cases := []string{
		"", "a", "a\n", "a\n\n", "\n", "\n\n", "a\r\nb", "a\rb", "a\r\r\nb",
		"a\vb\fc\x1dde", "a b c", "a\x1cb", "a\x1eb",
		"a\x1fb", // NOT a terminator, though it IS str.isspace()
		"line1\nline2\r\nline3\vline4", "trailing\r\n", "\r\n", "\r", "",
		"café\nüber", "a\n\r\nb", "a b c",
	}
	want := runPython(t, `
import json
cases = `+pyList(cases)+`
print(json.dumps([s.splitlines() for s in cases]))
`)

	gotParts := make([][]string, 0, len(cases))
	for _, c := range cases {
		gotParts = append(gotParts, pySplitlines(c))
	}
	// Both sides are rendered with the SAME ensure_ascii rules, so this is a
	// plain string comparison rather than a structural one.
	if got := pyListOfLists(gotParts); got != want {
		t.Errorf("pySplitlines disagrees with str.splitlines()\n python: %s\n go:     %s", want, got)
	}
}

// --------------------------------------------------------------------------- //
// DIVERGENCE 2 — str.isspace()
// --------------------------------------------------------------------------- //

func TestPyIsSpace_matchesCPython(t *testing.T) {
	got := parseCodePoints(t, runPython(t, `
spaces = [c for c in range(0x110000) if chr(c).isspace()]
print(",".join(str(c) for c in spaces))
`))
	if len(got) == 0 {
		t.Fatal("the probe returned an empty isspace set — it verified nothing")
	}
	for c := rune(0); c < 0x110000; c++ {
		if pyIsSpace(c) != got[c] {
			t.Errorf("pyIsSpace(U+%04X) = %v, CPython str.isspace() says %v",
				c, pyIsSpace(c), got[c])
		}
	}
}

// TestPyIsSpace_deltaIsExactlyTheInformationSeparators checks the CLAIM pystr.go
// makes in prose — that the delta is one-directional and consists of exactly
// U+001C-U+001F — rather than leaving it as an assertion a reader has to trust.
func TestPyIsSpace_deltaIsExactlyTheInformationSeparators(t *testing.T) {
	for c := rune(0); c < 0x110000; c++ {
		goSays, pySays := unicode.IsSpace(c), pyIsSpace(c)
		if goSays && !pySays {
			t.Errorf("U+%04X is whitespace to Go but not to Python — the delta is supposed to be one-directional", c)
		}
		if pySays && !goSays && (c < 0x1c || c > 0x1f) {
			t.Errorf("U+%04X is in the Python-only delta but is not an information separator", c)
		}
	}
}

// TestPyIsSpace_onlyU001FReachesTheLookaheads records the finding that narrows
// divergence 2's REACHABLE surface to a single character.
//
// Three of the four delta characters (U+001C, U+001D, U+001E) are also
// str.splitlines() terminators, so they can never survive inside one line and
// can never reach normalize_payload's lookaheads. U+001F is the only one that
// can — which is why it, and not the ticket's U+001C/U+001E, is what the
// normalizer tests and the parity fixture actually use.
func TestPyIsSpace_onlyU001FReachesTheLookaheads(t *testing.T) {
	reachable := []rune{}
	for c := rune(0x1c); c <= 0x1f; c++ {
		if !pyLineTerminator(c) {
			reachable = append(reachable, c)
		}
	}
	if len(reachable) != 1 || reachable[0] != 0x1f {
		t.Errorf("expected U+001F to be the only reachable delta character, got %v", reachable)
	}
}

// --------------------------------------------------------------------------- //
// DIVERGENCE 3 — json.dumps()
// --------------------------------------------------------------------------- //

func TestPyJSONDumpsString_matchesCPython(t *testing.T) {
	cases := []string{
		// What _BARE_WORD_RE can actually produce today — all trivially quoted.
		"entity", "a", "_x", "$id", "a1", "Order_2",
		// The two directions Go's encoding/json gets wrong: it emits raw UTF-8
		// where json.dumps escapes, and escapes <>& where json.dumps does not.
		"café", "a<b>&c", "<script>", "&amp;",
		// Escapes and control characters.
		"a\"b\\c", "tab\there", "nl\nhere", "cr\rhere", "\b\f",
		"\x00\x01\x1f\x7f", "/", "a/b",
		// Astral plane: json.dumps emits a surrogate PAIR under ensure_ascii.
		"\U0001F600", "\U00010000", "\U0010FFFF",
		// Ordinary non-ASCII, and the empty string.
		"über", "naïve", "日本語", "", " ",
	}
	want := runPython(t, `
import json
cases = `+pyList(cases)+`
print(json.dumps([json.dumps(c) for c in cases]))
`)
	encoded := make([]string, 0, len(cases))
	for _, c := range cases {
		encoded = append(encoded, pyJSONDumpsString(c))
	}
	if got := pyList(encoded); got != want {
		t.Errorf("pyJSONDumpsString disagrees with json.dumps\n python: %s\n go:     %s", want, got)
	}
}

// --------------------------------------------------------------------------- //
// code points, including the ones Go's utf8 package refuses to decode
// --------------------------------------------------------------------------- //

// TestPyRunes_roundTripsLoneSurrogates checks the WTF-8 round trip pyjson.go's
// writeCodePoint depends on: a lone surrogate must decode back to ITSELF, not to
// three U+FFFD replacements. Without it, `"\ud800"` would silently become a
// different string of a different length, and a minLength check or an enum
// comparison could come out differently from Python with nothing to say why.
func TestPyRunes_roundTripsLoneSurrogates(t *testing.T) {
	for _, code := range []rune{0xd800, 0xdbff, 0xdc00, 0xdfff} {
		var b strings.Builder
		writeCodePoint(&b, code)
		encoded := b.String()
		runes := pyRunes(encoded)
		if len(runes) != 1 || runes[0] != code {
			t.Errorf("U+%04X round-tripped to %v, want [%d]", code, runes, code)
		}
		if pyLen(encoded) != 1 {
			t.Errorf("U+%04X has pyLen %d, want 1 (Python's len() of a lone surrogate)",
				code, pyLen(encoded))
		}
		if repr := PyReprString(encoded); !strings.Contains(repr, "\\ud") {
			t.Errorf("U+%04X reprs as %s, want a \\uXXXX escape (Python's repr)", code, repr)
		}
	}
}

// TestPyRunes_ordinaryStringsAreUnchanged guards the fast path: pyRunes must be
// []rune(s) for anything that is not a WTF-8 surrogate, including strings that
// legitimately contain a 0xED lead byte (U+D000-U+D7FF encode with one).
func TestPyRunes_ordinaryStringsAreUnchanged(t *testing.T) {
	for _, s := range []string{
		"", "abc", "café", "\U0001F600", "日本語",
		"퟿", // 0xED lead byte, but NOT a surrogate
		"퟿퟿", "a퟿b",
	} {
		want := []rune(s)
		got := pyRunes(s)
		if len(got) != len(want) {
			t.Fatalf("pyRunes(%q) has %d runes, want %d", s, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("pyRunes(%q)[%d] = U+%04X, want U+%04X", s, i, got[i], want[i])
			}
		}
	}
}

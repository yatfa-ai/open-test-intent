package main

// Coverage for the JSON Schema `pattern` keyword — the one place the port
// re-implements a Python *engine* rather than a Python function.
//
// The shipped schema declares no patterns, so the parity harness's fixture
// corpus cannot reach any of this; tests/parity/run_parity.sh grows a schema in
// a temp tree to compare it differentially, and these tests pin the pieces at
// the unit level. Every `want` in TestPatternSemanticsMatchPython is a value
// produced by CPython's `re.search`, not one reasoned out.

import (
	"strings"
	"testing"
)

// Patterns the port accepts, and the RE2 source it runs for them. Only the
// trailing `$` is rewritten; everything else passes through unchanged, which is
// what makes "accepted" mean "identical", not "close".
func TestTranslatePatternAccepts(t *testing.T) {
	cases := []struct{ pattern, want string }{
		{``, ``},
		{`abc`, `abc`},
		{`^[a-z]+$`, `^[a-z]+(?:\n?\z)`},
		{`$`, `(?:\n?\z)`},
		{`^$`, `^(?:\n?\z)`},
		{`^v[0-9]{1,3}\.[0-9]+$`, `^v[0-9]{1,3}\.[0-9]+(?:\n?\z)`},
		{`a{2}`, `a{2}`},
		{`a{2,}`, `a{2,}`},
		{`(?:foo|bar)-[0-9]+`, `(?:foo|bar)-[0-9]+`},
		{`^(a|b)+$`, `^(a|b)+(?:\n?\z)`},
		{`^[^/]+$`, `^[^/]+(?:\n?\z)`},
		{`[a-z-]+`, `[a-z-]+`},
		{`^[\]]$`, `^[\]](?:\n?\z)`},
		{`a\.b`, `a\.b`},
		{`^\xe9$`, `^\xe9(?:\n?\z)`},
		{`^café$`, `^café(?:\n?\z)`},
		{`colou?r`, `colou?r`},
		{`a+?b`, `a+?b`},
		{`^\t\n\r\f\v\a$`, `^\t\n\r\f\v\a(?:\n?\z)`},
		{`\A[a-z]+`, `\A[a-z]+`},
		// A `$` inside a character class is an ordinary member, not the anchor.
		{`[$]`, `[$]`},
		{`\$`, `\$`},
		// A quantifier on a *grouped* anchor is legal in Python as well as RE2,
		// so the refusal below must not reach these. Every one of them compiles
		// under CPython's re.
		{`(^)*`, `(^)*`},
		{`(?:^)*`, `(?:^)*`},
		{`(?:\A)*`, `(?:\A)*`},
		{`()*`, `()*`},
		{`(?:)*`, `(?:)*`},
		{`(^)+abc`, `(^)+abc`},
		// `^` next to, but not quantified by, the operators.
		{`^|a`, `^|a`},
		{`^a*`, `^a*`},
		{`[a^]*`, `[a^]*`},
		{`[^a]*`, `[^a]*`},
		{`\Aab*`, `\Aab*`},
	}
	for _, tc := range cases {
		got, err := translatePattern(tc.pattern)
		if err != nil {
			t.Errorf("translatePattern(%q) refused: %v", tc.pattern, err)
			continue
		}
		if got != tc.want {
			t.Errorf("translatePattern(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

// Patterns the port refuses, each because Python and RE2 genuinely disagree
// about it — not because the allow-list is timid. `reason` is a substring of
// the diagnostic, so the message stays actionable rather than "unsupported".
func TestTranslatePatternRefuses(t *testing.T) {
	cases := []struct{ pattern, reason string }{
		// The four divergences that motivated the guard.
		{`^\d+$`, "Unicode-aware"},
		{`^\w+$`, "Unicode-aware"},
		{`^\s+$`, "Unicode-aware"},
		{`\bword\b`, "word boundary"},
		{`\D`, "Unicode-aware"},
		{`\W`, "Unicode-aware"},
		{`\S`, "Unicode-aware"},
		{`[\d]`, "Unicode-aware"},
		// `$` is only exact as the last character.
		{`a$b`, "very last character"},
		{`^x$\n`, "very last character"},
		// End-of-string spellings the two engines do not share.
		{`abc\Z`, `\z`},
		{`abc\z`, `\Z`},
		// RE2-only syntax Python rejects outright.
		{`\p{L}+`, "RE2 only"},
		{`[[:alpha:]]+`, "POSIX"},
		// Python-only syntax RE2 rejects, or reads differently.
		{`a{,3}`, "{0,n}"},
		{`(a)\1`, "backreference"},
		{`\0`, "backreference"},
		{`(?i)abc`, "non-capturing"},
		{`(?P<name>a)`, "non-capturing"},
		{`(?=a)b`, "non-capturing"},
		{`(?<=a)b`, "non-capturing"},
		{`[]a]`, `\]`},
		{`\u0041`, "Python-only"},
		{`\N{BULLET}`, "Python-only"},
		// Malformed input is refused with a diagnostic, never passed through.
		{`abc\`, "lone"},
		{`[abc`, "never closed"},
		{`\x4`, "exactly two hex digits"},
		{`\x{263a}`, "exactly two hex digits"},
		{`\é`, "non-ASCII"},
	}
	for _, tc := range cases {
		got, err := translatePattern(tc.pattern)
		if err == nil {
			t.Errorf("translatePattern(%q) = %q, want a refusal", tc.pattern, got)
			continue
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("translatePattern(%q) refused with %q, want it to mention %q",
				tc.pattern, err, tc.reason)
		}
	}
}

// A quantifier applied to a bare `^` or `\A` compiles under RE2 and is a hard
// parse error in Python ("nothing to repeat"). Accepting one lets the port
// return a verdict — a vacuous PASS, since `^*` matches empty everywhere —
// where the reference raises. Both anchors, both positions, every quantifier
// spelling; each of the 28 was confirmed to raise re.error under CPython 3.
func TestTranslatePatternRefusesQuantifiedAnchor(t *testing.T) {
	for _, anchor := range []string{`^`, `\A`} {
		for _, quantifier := range []string{`*`, `+`, `?`, `{2}`, `{2,}`, `{1,3}`, `*?`} {
			for _, pattern := range []string{
				anchor + quantifier,             // at the start of the pattern
				"a" + anchor + quantifier + "b", // and in the middle
			} {
				got, err := translatePattern(pattern)
				if err == nil {
					t.Errorf("translatePattern(%q) = %q, want a refusal (python: nothing to repeat)",
						pattern, got)
					continue
				}
				if !strings.Contains(err.Error(), "nothing to repeat") {
					t.Errorf("translatePattern(%q) refused with %q, want it to explain the "+
						"Python parse error", pattern, err)
				}
			}
		}
	}
	// `^^*` quantifies the second `^`, so it is refused too — as Python does.
	if _, err := translatePattern(`^^*`); err == nil {
		t.Error("translatePattern(`^^*`) was accepted, want a refusal")
	}
}

// The point of the whole exercise: for every pattern the port accepts, its
// verdict is the one CPython's re.search gives. Each `want` below was produced
// by running the pair through python3.
func TestPatternSemanticsMatchPython(t *testing.T) {
	cases := []struct {
		pattern, input string
		want           bool
	}{
		// The reviewer's row 1 — Python's `$` matches before a trailing newline.
		{`^[a-z]+$`, "abc", true},
		{`^[a-z]+$`, "abc\n", true},
		{`^[a-z]+$`, "abc\n\n", false},
		{`^[a-z]+$`, "ab1", false},
		{`^[a-z]+$`, "\nabc", false},
		{`$`, "", true},
		{`$`, "\n", true},
		{`^$`, "", true},
		{`^$`, "\n", true},
		{`^$`, "a", false},
		{`^[a-z\n]+$`, "abc\n\n", true},
		// re.search is unanchored, and so is Go's MatchString.
		{`abc`, "xxabcxx", true},
		{`abc`, "ab", false},
		{`^v[0-9]{1,3}\.[0-9]+$`, "v12.4", true},
		{`^v[0-9]{1,3}\.[0-9]+$`, "v1234.4", false},
		{`a{2,}`, "aaa", true},
		{`a{2,}`, "a", false},
		{`a{2}`, "aa", true},
		{`(?:foo|bar)-[0-9]+`, "bar-7", true},
		{`(?:foo|bar)-[0-9]+`, "baz-7", false},
		{`^[^/]+$`, "abc", true},
		{`^[^/]+$`, "a/b", false},
		{`[a-z-]+`, "-", true},
		{`[-a-z]+`, "-", true},
		{`a\.b`, "a.b", true},
		{`a\.b`, "axb", false},
		// \xHH is a code point in both engines, not a byte.
		{`^\xe9$`, "é", true},
		{`^café$`, "café", true},
		// `.` excludes the newline in both.
		{`^.$`, "\n", false},
		{`^.$`, "x", true},
		{`^[\]]$`, "]", true},
		{`^[\[]$`, "[", true},
		{`colou?r`, "color", true},
		{`^ab*c$`, "ac", true},
		{`^(a|b)+$`, "abab", true},
		{`^\t$`, "\t", true},
		// The grouped anchors the quantified-anchor refusal must not swallow:
		// legal in both engines, and agreeing on the verdict.
		{`(^)*`, "abc", true},
		{`(^)*`, "", true},
		{`(?:^)*`, "abc", true},
		{`(?:\A)*`, "abc", true},
		{`()*`, "abc", true},
		{`(^)+abc`, "abc", true},
		{`(^)+abc`, "xabc", false},
		{`^|a`, "zz", true},
		{`^a*`, "b", true},
		{`[a^]*`, "^", true},
		{`[^a]*`, "b", true},
		{`\Aab*`, "abbb", true},
		{`\Aab*`, "xab", false},
	}
	for _, tc := range cases {
		compiled, err := CompilePythonPattern(tc.pattern)
		if err != nil {
			t.Errorf("CompilePythonPattern(%q) refused: %v", tc.pattern, err)
			continue
		}
		if got := compiled.MatchString(tc.input); got != tc.want {
			t.Errorf("pattern %q against %q = %v, want %v (python3 re.search says %v)",
				tc.pattern, tc.input, got, tc.want, tc.want)
		}
	}
}

// A refused pattern must take the whole schema down at load time, not surface
// later as a validation error against some document — which would blame the
// document for the port's limitation.
func TestCompileSchemaRefusesDivergentPattern(t *testing.T) {
	_, err := CompileSchema(mustDecode(t,
		`{"properties": {"slug": {"type": "string", "pattern": "^\\w+$"}}}`))
	if err == nil {
		t.Fatal("CompileSchema accepted a schema carrying `^\\w+$`, want a refusal")
	}
	for _, want := range []string{"cannot faithfully reproduce", `'^\\w+$'`, "Unicode-aware"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// Nested and array positions are walked too — a pattern hiding under `items` or
// a `definitions` list is not a way past the guard.
func TestCompileSchemaWalksTheWholeDocument(t *testing.T) {
	for _, schema := range []string{
		`{"properties": {"tags": {"items": {"pattern": "\\d"}}}}`,
		`{"anyOf": [{"type": "string"}, {"pattern": "\\d"}]}`,
		`{"definitions": {"a": {"b": {"c": {"pattern": "\\d"}}}}}`,
	} {
		if _, err := CompileSchema(mustDecode(t, schema)); err == nil {
			t.Errorf("CompileSchema(%s) accepted a nested divergent pattern", schema)
		}
	}
}

// The accepted path, wired end to end: an accepted pattern reaches Validate as
// a compiled regexp and produces the reference's message, repr'd pattern and
// all.
func TestValidatePatternMessage(t *testing.T) {
	schema := mustSchema(t, `{
		"type": "object",
		"properties": {"slug": {"type": "string", "pattern": "^[a-z]+$"}}
	}`)
	cases := []struct {
		instance string
		want     []string
	}{
		{`{"slug": "abc"}`, nil},
		// The trailing-newline case, through the real validator.
		{"{\"slug\": \"abc\\n\"}", nil},
		{`{"slug": "AB"}`, []string{`slug: string does not match pattern '^[a-z]+$'`}},
	}
	for _, tc := range cases {
		got := schema.Validate(mustDecode(t, tc.instance))
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("Validate(%s) = %q, want %q", tc.instance, got, tc.want)
		}
	}
}

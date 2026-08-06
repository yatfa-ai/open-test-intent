package main

// Differential test for pyjson.go: this decoder against python3's json.loads.
//
// pyjson.go reproduces CPython's parser *including its error prose and offsets*,
// which is a large surface to get right from reading alone. So it is not
// established by reading: every case here is decoded twice — once by
// python3 -c 'json.loads(...)' and once by DecodeOrderedString — and the two
// must agree on the decoded value (compared as Python `repr`) or on the exact
// exception string.
//
// The generated half matters more than the hand-written half. Hand-written cases
// only cover failures somebody already thought of; the token-soup generator
// produces the ones nobody did, which is where a message-and-offset port
// actually breaks.
//
// Skipped, loudly, when python3 is unavailable — a differential test with no
// oracle has verified nothing and must not read as a pass.

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pythonOracle renders, for each input, either "OK <repr(value)>" or
// "ERR <str(exc)>" — the same two shapes the Go side produces below.
const pythonOracle = `
import json, sys
for line in sys.stdin:
    doc = json.loads(line.rstrip("\n"))   # the case itself, transported as a JSON string
    try:
        sys.stdout.write("OK " + repr(json.loads(doc)) + "\n")
    except json.JSONDecodeError as exc:
        sys.stdout.write("ERR " + str(exc) + "\n")
    except RecursionError:
        sys.stdout.write("SKIP recursion\n")
`

func goResult(doc string) string {
	value, err := DecodeOrderedString(doc)
	if err != nil {
		return "ERR " + err.Error()
	}
	return "OK " + PyRepr(value)
}

func runPythonOracle(t *testing.T, cases []string) []string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — the differential oracle is unavailable, so this test verified nothing")
	}

	script := filepath.Join(t.TempDir(), "oracle.py")
	if err := os.WriteFile(script, []byte(pythonOracle), 0o644); err != nil {
		t.Fatalf("writing oracle: %v", err)
	}

	// Each case travels as a JSON string literal so an embedded newline or quote
	// cannot desynchronise the line protocol.
	var stdin strings.Builder
	for _, c := range cases {
		stdin.WriteString(pyJSONDumpsString(c))
		stdin.WriteByte('\n')
	}

	cmd := exec.Command(python, script)
	cmd.Stdin = strings.NewReader(stdin.String())
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("python oracle failed: %v", err)
	}

	lines := []string{}
	scanner := bufio.NewScanner(strings.NewReader(out.String()))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != len(cases) {
		t.Fatalf("oracle returned %d results for %d cases", len(lines), len(cases))
	}
	return lines
}

// compareAgainstPython is the assertion every case in this file goes through.
func compareAgainstPython(t *testing.T, cases []string) {
	t.Helper()
	want := runPythonOracle(t, cases)
	mismatches := 0
	for i, doc := range cases {
		if strings.HasPrefix(want[i], "SKIP") {
			continue
		}
		got := goResult(doc)
		if got == want[i] {
			continue
		}
		mismatches++
		if mismatches <= 20 {
			t.Errorf("input %s\n  python: %s\n  go:     %s", PyReprString(doc), want[i], got)
		}
	}
	if mismatches > 20 {
		t.Errorf("... and %d further mismatches", mismatches-20)
	}
}

// --------------------------------------------------------------------------- //
// hand-written cases — one per branch of the parser
// --------------------------------------------------------------------------- //

func TestPyJSON_handWrittenCases(t *testing.T) {
	compareAgainstPython(t, []string{
		// documents that parse
		`{}`, `[]`, `{ }`, `[ ]`, `null`, `true`, `false`, `0`, `-0`, `1`, `-1`,
		`1.0`, `1e2`, `1E+2`, `1e-2`, `0.5`, `-1.5`, `1.5e300`, `1e400`,
		`12345678901234567890123456789`, `"a"`, `""`, `"café"`, `"😀"`,
		`{"a": 1}`, `{"a":1,"b":2}`, `{"a":1,"a":2}`, `{"":1}`,
		`{  "a"  :  1  ,  "b" : 2  }`, `[1,2,3]`, `[[1],[2]]`, `{"a":[1,{"b":2}]}`,
		"\t\n\r {\"a\": 1} \t\n\r",
		`NaN`, `Infinity`, `-Infinity`, `[NaN, Infinity, -Infinity]`,
		// escapes
		`"\"\\\/\b\f\n\r\t"`, `"A"`, `"é"`, `"😀"`,
		`"𐀀"`, `"\ud800x"`, `"\ud800é"`, `"\udc00"`, `"\u0000"`, `" "`,
		// documents that do not
		``, `   `, `{`, `[`, `[ `, `[  `, `{ `, `{  `,
		`{"a"`, `{"a":`, `{"a":1`, `{"a":1,`, `{"a":1, `, `{"a":1,}`, `{"a":1,  }`,
		`{"a":1 , }`, `{"a":1 "b":2}`, `{a:1}`, `{1:2}`, `{"a" 1}`, `{"a":x}`,
		`{"a": }`, `{"a"::1}`, `{,}`, `{"a":1,,}`,
		`[1,]`, `[1 2]`, `[1`, `[1 `, `[1,`, `[1, `, `[1 , ]`, `[1,,]`, `[,]`, `["a",]`,
		`[[[`, `{"a":[1,{"b":}]}`, `{}x`, `1 2`, `{"a":1}extra`, `01`, `-`, `+1`,
		`nul`, `tru`, `fals`, `Nan`, `infinity`,
		`"abc`, `"a`, `"a\`, `{"a":"b`, "\"a\tb\"", "\"\x1f\"", `"a\qb"`,
		`"a\u12"`, `"a\uZZZZ"`, `"\U0001F600"`, `"\ud800\uZZZZ"`, `"\ud800\u"`,
		"\ufeff{}", // a BOM the caller was supposed to strip with utf-8-sig
		// multi-line: lineno/colno derivation
		"{\n  \"a\" 1}", "[\n1,\n2\n", "{\n\n\n\"a\"", "\n\n{\"a\": x}",
		// a non-ASCII character BEFORE the error — the case that separates a
		// code-point-indexed port from a byte-indexed one.
		`{"é": x}`, `{"ééé": x}`, `["😀", x]`, `{"entity": café}`,
		// \v and \x85 are line terminators to str.splitlines() but NOT newlines
		// to JSONDecodeError, which counts only "\n". Both must be reproduced.
		"{\v\"a\" 1}", "{\"a\" 1}",
	})
}

// --------------------------------------------------------------------------- //
// generated cases — the failures nobody thought of
// --------------------------------------------------------------------------- //

// jsonTokens is the alphabet the soup generator draws from: every structural
// character, both good and bad literals, whitespace of three different
// classifications (JSON whitespace, Python-only whitespace, neither), non-ASCII,
// and fragments of escapes.
var jsonTokens = []string{
	`{`, `}`, `[`, `]`, `,`, `:`, `"a"`, `"b"`, `""`, `1`, `-1`, `0`, `01`, `1.5`,
	`1e`, `1e5`, `.5`, `-`, `+`, `null`, `true`, `false`, `NaN`, `Infinity`,
	`-Infinity`,
	// Whitespace of three different classifications: whitespace to json's
	// scanner, whitespace to str.isspace() but NOT to the scanner, and
	// control characters that are neither.
	" ", "\t", "\n", "\r", "\v", "\f", "\x1c", "\x1e", "\u0085", "\u00a0",
	`"`, `\`, `x`, `é`, `😀`, `"é"`, `"é"`, `"\u"`, `"\q"`, `"\ud800"`,
	`"𐀀"`, "\"\t\"", `"unterminated`, `}}`, `][`, `::`, `,,`,
}

func TestPyJSON_generatedSoup(t *testing.T) {
	rng := rand.New(rand.NewSource(20260806))
	cases := make([]string, 0, 6000)
	for i := 0; i < 6000; i++ {
		n := 1 + rng.Intn(7)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteString(jsonTokens[rng.Intn(len(jsonTokens))])
		}
		cases = append(cases, b.String())
	}
	compareAgainstPython(t, cases)
}

// TestPyJSON_generatedWellFormed keeps the *accepting* half of the parser under
// the same pressure: soup is overwhelmingly rejected, so a decoder that returned
// an error for everything would still look good above.
func TestPyJSON_generatedWellFormed(t *testing.T) {
	rng := rand.New(rand.NewSource(981))
	cases := make([]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		cases = append(cases, randomJSONValue(rng, 0))
	}
	compareAgainstPython(t, cases)
}

func randomJSONValue(rng *rand.Rand, depth int) string {
	kinds := 8
	if depth >= 3 {
		kinds = 6 // stop recursing
	}
	switch rng.Intn(kinds) {
	case 0:
		return []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"}[rng.Intn(6)]
	case 1:
		return fmt.Sprintf("%d", rng.Intn(2000)-1000)
	case 2:
		return []string{"1.0", "0.5", "-1.5", "1e2", "1E+2", "1e-2", "1.5e300", "-0.0"}[rng.Intn(8)]
	case 3:
		return "12345678901234567890123456789"
	case 4, 5:
		return []string{
			`"a"`, `""`, `"café"`, `"😀"`, `"é"`, `"😀"`,
			`"\ud800"`, `"line\nbreak"`, `"quote\"inside"`, `"tab\there"`,
			`"\u0000"`, `" "`, `"a<b>&c"`,
		}[rng.Intn(12)]
	case 6:
		n := rng.Intn(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = randomJSONValue(rng, depth+1)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		n := rng.Intn(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`"k%d": %s`, i, randomJSONValue(rng, depth+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
}

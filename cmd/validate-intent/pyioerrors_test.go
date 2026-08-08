package main

// Tests for pyioerrors.go and the handler-dependent half of pystdout.go.
//
// The claim under test is the one two rounds of SPGD-107's review found stated
// as a comment and nowhere else:
//
//	round 4 — PYTHONIOENCODING governs sys.stdin AND sys.stdout, so a port that
//	          consults it for one direction and hard-codes the other is wrong
//	          under an environment it already models;
//	round 5 — PYTHONIOENCODING has TWO fields, so a port that consults the error
//	          handler and hard-codes the encoding is wrong the same way. Both
//	          diverge with the SAME exit code, which is why nothing caught either.
//
// So both are checked DIFFERENTIALLY, against python3, for both streams, rather
// than by a table typed from the docs: a hand-written table would encode the
// same misreading twice, and the misreadings here were "the default is
// surrogateescape, therefore the handler is surrogateescape" and "the handler is
// strict, therefore the environment is reproducible".

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// handlerSpecs is every PYTHONIOENCODING spelling the port claims to classify,
// including the three that must be REFUSED. `set: false` is the unset case,
// which is the one whose answer depends on the locale.
var handlerSpecs = []struct {
	name  string
	set   bool
	value string
	want  string
}{
	{"unset", false, "", "surrogateescape"},
	{"empty", true, "", "surrogateescape"},
	{"encoding only", true, "utf-8", "strict"},
	{"encoding with an empty errors part", true, "utf-8:", "strict"},
	{"empty encoding, no errors", true, ":", "surrogateescape"},
	{"empty encoding, explicit errors", true, ":replace", "replace"},
	{"both parts", true, "UTF-8:strict", "strict"},
	{"an unreproducible handler", true, "utf-8:backslashreplace", "backslashreplace"},
	{"ignore", true, "utf-8:ignore", "ignore"},
	{"surrogateescape named explicitly", true, "utf-8:surrogateescape", "surrogateescape"},
}

func TestPyIOErrors_matchesCPythonOnBothStreams(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	probe := filepath.Join(t.TempDir(), "handlers.py")
	script := "import sys\nprint(sys.stdin.errors)\nprint(sys.stdout.errors)\n"
	if err := os.WriteFile(probe, []byte(script), 0o644); err != nil {
		t.Fatalf("writing probe: %v", err)
	}

	for _, tc := range handlerSpecs {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(python, probe)
			// A pristine environment plus exactly the variable under test, so a
			// PYTHONIOENCODING inherited from the caller cannot make an unset
			// case pass by accident.
			cmd.Env = []string{"LANG=" + os.Getenv("LANG"), "PATH=" + os.Getenv("PATH")}
			if tc.set {
				cmd.Env = append(cmd.Env, "PYTHONIOENCODING="+tc.value)
				t.Setenv("PYTHONIOENCODING", tc.value)
			} else {
				os.Unsetenv("PYTHONIOENCODING")
			}
			cmd.Stdin = strings.NewReader("")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("python probe failed: %v", err)
			}
			lines := strings.Fields(string(out))
			if len(lines) != 2 {
				t.Fatalf("probe returned %q, want two handler names", out)
			}
			stdinErrors, stdoutErrors := lines[0], lines[1]

			// The load-bearing assertion: ONE handler, both streams. If CPython
			// ever disagreed with itself here, pyIOErrors' whole premise —
			// answering for the output side from the input side's variable —
			// would be unsound, and this fails rather than the port quietly
			// picking the wrong one.
			if stdinErrors != stdoutErrors {
				t.Fatalf("PYTHONIOENCODING=%q (set=%v): sys.stdin.errors=%q but "+
					"sys.stdout.errors=%q — pyIOErrors assumes they agree",
					tc.value, tc.set, stdinErrors, stdoutErrors)
			}
			if got := pyIOErrors(); got != stdinErrors {
				t.Errorf("PYTHONIOENCODING=%q (set=%v): pyIOErrors()=%q, CPython says %q",
					tc.value, tc.set, got, stdinErrors)
			}
			// The table is checked too, so a CPython that changed its answer
			// fails loudly instead of dragging the port along with it.
			if stdinErrors != tc.want {
				t.Errorf("PYTHONIOENCODING=%q (set=%v): CPython says %q, this test expected %q",
					tc.value, tc.set, stdinErrors, tc.want)
			}
			wantSupported := tc.want == "strict" || tc.want == "surrogateescape"
			if got := pyIOHandlerSupported(); got != wantSupported {
				t.Errorf("pyIOHandlerSupported()=%v for handler %q", got, tc.want)
			}
		})
	}
}

// TestPyEncodeSurrogates_honoursTheHandler is the unit-level statement of the
// round-4 fix.
//
// U+DC80-U+DCFF is the range the surrogateescape DECODER produces, so that
// handler maps each back to its original byte. Under `strict` nothing produced
// that range — the decoder raised instead — so a surrogate reaching the encoder
// can only have come from an explicit \uXXXX escape, and CPython refuses it.
// Measured: `{"\udc82key":"x"}` under PYTHONIOENCODING=utf-8 makes the reference
// die with UnicodeEncodeError mid-report, while the port used to write byte 0x82
// and finish the line.
//
// The two rows for the SAME input are the point. Before the fix this function
// took no handler and both rows were the first one.
func TestPyEncodeSurrogates_honoursTheHandler(t *testing.T) {
	escaped := pySurrogateEscape([]byte{0x82}) // U+DC82, in the escape range

	// U+D800 is outside that range, so it is only reachable from an explicit
	// \uXXXX escape in the input. Built with writeCodePoint because Go's
	// string(rune) turns a surrogate into U+FFFD, which would test nothing.
	var b strings.Builder
	writeCodePoint(&b, rune(0xd800))
	fromEscape := b.String()

	cases := []struct {
		name    string
		input   string
		errors  string
		wantOK  bool
		wantOut string
		wantBad rune
	}{
		{"escape range under surrogateescape", escaped, "surrogateescape",
			true, "\x82", 0},
		{"escape range under strict", escaped, "strict",
			false, "", 0xdc82},
		{"outside the range under surrogateescape", fromEscape, "surrogateescape",
			false, "", 0xd800},
		{"outside the range under strict", fromEscape, "strict",
			false, "", 0xd800},
		{"no surrogates at all, surrogateescape", "café <root> &", "surrogateescape",
			true, "café <root> &", 0},
		{"no surrogates at all, strict", "café <root> &", "strict",
			true, "café <root> &", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, bad, ok := pyEncodeSurrogates(tc.input, tc.errors)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (bad=U+%04X)", ok, tc.wantOK, bad)
			}
			if ok && got != tc.wantOut {
				t.Errorf("encoded to %x, want %x", got, tc.wantOut)
			}
			if !ok && bad != tc.wantBad {
				t.Errorf("refused at U+%04X, want U+%04X", bad, tc.wantBad)
			}
		})
	}
}

// encodingSpecs is the OTHER field of PYTHONIOENCODING, and the table the
// round-5 defect lived in the absence of.
//
// `want` is the codec name CPython reports for sys.stdin.encoding — its
// NORMALISED name, not the spelling, which is why `latin-1` appears as
// `iso8859-1`. The port must accept exactly the rows whose codec is `utf-8`,
// since UTF-8 is what it hard-codes on both sides, and refuse the rest.
//
// The six utf-8 ALIASES are in here deliberately. Refusing them would be a safe
// failure but still a wrong one — CPython answers `U8` and `cp65001` runs, and
// so must the port — and they are the rows a whitelist gets wrong. Without them
// this table would only prove that non-UTF-8 codecs are refused, which the
// obvious over-tight implementation (`encoding == "utf-8"`) also passes.
var encodingSpecs = []struct {
	name  string
	set   bool
	value string
	want  string
}{
	{"unset — the locale default", false, "", "utf-8"},
	{"empty", true, "", "utf-8"},
	{"no encoding, only a handler", true, ":replace", "utf-8"},
	{"canonical", true, "utf-8", "utf-8"},
	{"upper case", true, "UTF-8", "utf-8"},
	{"no separator", true, "utf8", "utf-8"},
	{"underscore separator", true, "utf_8", "utf-8"},
	{"repeated punctuation", true, "utf---8", "utf-8"},
	{"alias u8", true, "U8", "utf-8"},
	{"alias utf", true, "utf", "utf-8"},
	{"alias cp65001", true, "cp65001", "utf-8"},
	{"alias utf8_ucs2", true, "utf8_ucs2", "utf-8"},
	{"alias utf8_ucs4", true, "utf8_ucs4", "utf-8"},
	{"with a handler after it", true, "utf-8:strict", "utf-8"},
	// The rows the gate exists for. Each has handler `strict`, which the port
	// DOES reproduce — so the handler half of the gate waves every one of them
	// through, and only the encoding half can catch them.
	{"latin-1", true, "latin-1", "iso8859-1"},
	{"latin1, spelled without the dash", true, "latin1", "iso8859-1"},
	{"ascii", true, "ascii", "ascii"},
	{"cp1252", true, "cp1252", "cp1252"},
	{"utf-16", true, "utf-16", "utf-16"},
	{"a non-UTF-8 codec with an unreproducible handler too", true,
		"latin-1:replace", "iso8859-1"},
}

func TestPyIOEncoding_matchesCPythonOnBothStreams(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	probe := filepath.Join(t.TempDir(), "encodings.py")
	// os.write to the raw fd, NOT print(). This probe is run under the very
	// encodings it is reporting, and `print` would encode its answer with them:
	// under PYTHONIOENCODING=utf-16 the reply came back as UTF-16 and the parse
	// below failed on a probe that had actually worked. Writing pre-encoded
	// ASCII bytes past sys.stdout is the only spelling that reads the same in
	// every row of the table.
	script := "import os, sys\n" +
		"os.write(1, ('%s %s\\n' % (sys.stdin.encoding, sys.stdout.encoding)).encode('ascii'))\n"
	if err := os.WriteFile(probe, []byte(script), 0o644); err != nil {
		t.Fatalf("writing probe: %v", err)
	}

	for _, tc := range encodingSpecs {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(python, probe)
			// A pristine environment plus exactly the variable under test, so a
			// PYTHONIOENCODING inherited from the caller cannot make the unset
			// case pass by accident.
			cmd.Env = []string{"LANG=" + os.Getenv("LANG"), "PATH=" + os.Getenv("PATH")}
			if tc.set {
				cmd.Env = append(cmd.Env, "PYTHONIOENCODING="+tc.value)
				t.Setenv("PYTHONIOENCODING", tc.value)
			} else {
				os.Unsetenv("PYTHONIOENCODING")
			}
			cmd.Stdin = strings.NewReader("")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("python probe failed: %v", err)
			}
			lines := strings.Fields(string(out))
			if len(lines) != 2 {
				t.Fatalf("probe returned %q, want two codec names", out)
			}
			stdinEncoding, stdoutEncoding := lines[0], lines[1]

			// Same load-bearing premise as the handler test above, on the other
			// field: ONE codec, both streams. pyIOEncodingSupported answers for
			// the output side from the input side's variable, and if CPython
			// ever set the two apart that would be unsound.
			if stdinEncoding != stdoutEncoding {
				t.Fatalf("PYTHONIOENCODING=%q (set=%v): sys.stdin.encoding=%q but "+
					"sys.stdout.encoding=%q — pyIOEncodingSupported assumes they agree",
					tc.value, tc.set, stdinEncoding, stdoutEncoding)
			}
			if stdinEncoding != tc.want {
				t.Errorf("PYTHONIOENCODING=%q (set=%v): CPython says %q, this test expected %q",
					tc.value, tc.set, stdinEncoding, tc.want)
			}

			// The assertion the round-5 blocker would have failed. The port
			// hard-codes UTF-8 on both sides, so it may answer a run exactly
			// when CPython's codec IS utf-8 — an over-refusal (an alias the
			// whitelist misses) fails here just as loudly as the under-refusal
			// that shipped.
			wantSupported := stdinEncoding == "utf-8"
			if got := pyIOEncodingSupported(); got != wantSupported {
				t.Errorf("PYTHONIOENCODING=%q (set=%v): pyIOEncodingSupported()=%v, "+
					"but CPython encodes both streams as %q",
					tc.value, tc.set, got, stdinEncoding)
			}

			// ...and the gate the caller actually uses agrees with the two
			// halves it is made of. Written as an independent expression rather
			// than by calling the halves again, so a pyIOUnsupported that
			// dropped one of them fails here.
			surface, unsupported := pyIOUnsupported()
			if wantUnsupported := !wantSupported || !pyIOHandlerSupported(); unsupported != wantUnsupported {
				t.Errorf("PYTHONIOENCODING=%q: pyIOUnsupported() reported %v (%q), want %v",
					tc.value, unsupported, surface, wantUnsupported)
			}
			// The encoding is named FIRST when both fields are unreproducible,
			// because it is the field that changes which string was validated.
			if !wantSupported && !strings.Contains(surface, "encoding") {
				t.Errorf("PYTHONIOENCODING=%q: refusal names %q, want it to name the encoding",
					tc.value, surface)
			}
		})
	}
}

// TestPyIOEncoding_unknownCodecIsRefused pins the acknowledged NON-parity.
//
// CPython cannot start at all with an encoding it does not know: it dies in
// init_stdio_encoding with a fatal error and exit 1, before bin/validate-intent's
// first line runs. The port exits 2 naming the encoding. Those differ, and that
// is stated rather than smoothed over — neither produces a report, so no
// consumer is handed a wrong answer, and reproducing an interpreter-startup
// failure is not a behaviour of this binary.
//
// What this test guards is the half that WOULD be a wrong answer: before the
// round-5 fix the port read `bogus` as "some encoding, therefore handler strict,
// therefore reproducible" and emitted a clean, plausible report where the
// reference had refused to start.
func TestPyIOEncoding_unknownCodecIsRefused(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "bogus")
	if pyIOEncodingSupported() {
		t.Fatal("an encoding CPython cannot even start with is reported as reproducible")
	}
	surface, unsupported := pyIOUnsupported()
	if !unsupported || !strings.Contains(surface, "'bogus'") {
		t.Errorf("pyIOUnsupported() = %q, %v; want a refusal naming 'bogus'", surface, unsupported)
	}
}

// TestPyNormalizeEncoding is the unit-level statement of the one function above
// that is pure string work, so its edge cases can be stated directly instead of
// being reached only through whichever spellings encodingSpecs happens to list.
//
// The rule reproduced is encodings.normalize_encoding: runs of punctuation
// collapse to a single '_', leading and trailing punctuation vanishes, '.' is
// kept as an ordinary character, and the result is lower-cased (which CPython
// does a layer up, in codecs.lookup).
func TestPyNormalizeEncoding(t *testing.T) {
	cases := []struct{ in, want string }{
		{"utf-8", "utf_8"},
		{"UTF-8", "utf_8"},
		{"utf8", "utf8"},
		{"utf_8", "utf_8"},
		{"utf---8", "utf_8"},
		{"  utf 8  ", "utf_8"},
		{"---", ""},
		{"", ""},
		{"iso8859-1", "iso8859_1"},
		{"latin-1", "latin_1"},
		{"cp1252", "cp1252"},
		{"quopri.codec", "quopri.codec"},
		// Non-ASCII is treated as punctuation here where CPython's isalnum()
		// would keep it. That can only split a name into something the
		// whitelist does not hold — an over-refusal, never a wrong answer —
		// and this pins the direction rather than leaving it to be rediscovered.
		{"utf–8", "utf_8"},
	}
	for _, tc := range cases {
		if got := pyNormalizeEncoding(tc.in); got != tc.want {
			t.Errorf("pyNormalizeEncoding(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestUsageIsNotASCII pins the premise that puts the ENCODING gate ABOVE the
// --help check in run(), where the HANDLER gate sits below it.
//
// The inherited comment said --help was "a compile-time ASCII constant that no
// environment can alter", and that is why the first draft of the encoding gate
// was written below it. Half of it is true — no error HANDLER can touch a
// string the codec represents — and the ASCII half is not: `usage` is compared
// byte-for-byte against the reference's USAGE, which carries an em dash, so
// under `latin-1` the reference dies with UnicodeEncodeError and 0 bytes of
// stdout and under `utf-16` it emits twice the bytes.
//
// If that em dash were ever removed from BOTH texts this test goes red. The fix
// then is not to delete the test: it is to re-measure `--help` under latin-1
// and utf-16 and decide the ordering again with the new evidence, because
// "purely ASCII" would make the encoding gate above --help unnecessary rather
// than merely harmless — and a reader would have no way to tell which.
func TestUsageIsNotASCII(t *testing.T) {
	for i := 0; i < len(usage); i++ {
		if usage[i] > 0x7f {
			return
		}
	}
	t.Error("usage is pure ASCII, so the reason the encoding gate precedes the " +
		"--help check in run() no longer holds — re-measure `--help` under " +
		"PYTHONIOENCODING=latin-1 and utf-16 before trusting either ordering")
}

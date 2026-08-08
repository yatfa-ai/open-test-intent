package main

// `PYTHONIOENCODING` governs BOTH of CPython's std streams, and this file is the
// only place that reads it. Despite the filename — kept so the citations in
// main.go, pystdout.go, stdin_mode.go and the README stay pointing here — it
// owns the WHOLE variable, not just its error-handler half.
//
// It has its own file because getting this wrong is invisible. The variable has
// TWO fields, `ENCODING:HANDLER`, and it is not a stdin setting: it configures
// sys.stdin, sys.stdout and sys.stderr together. A port that reads one field, or
// one direction, and hard-codes the rest is right about half of every invocation
// and confidently wrong about the other half — with the SAME exit code, which is
// the signature of a divergence that ships. This port has now made that mistake
// on each axis in turn, which is why both are answered here and nowhere else:
//
//	round 4  — the DIRECTION. `readStdin` asked; `pyWriteStdout` assumed.
//	round 5  — the FIELD.     the handler was read; the encoding was discarded,
//	                          so `latin-1` (handler `strict`) sailed through the
//	                          gate while CPython encoded both streams as
//	                          iso8859-1 and the port hard-coded UTF-8.
//
// Measured against python3 in this container (CPython 3.13.5, LANG=C.UTF-8),
// printing sys.stdin.encoding / sys.stdout.encoding and sys.stdin.errors /
// sys.stdout.errors for each spelling. Both columns of each pair were identical
// in every row, which is why one function answers for both streams:
//
//	PYTHONIOENCODING          sys.std*.encoding   sys.std*.errors   port
//	------------------------  ------------------  ----------------  --------
//	unset                     utf-8               surrogateescape   answers
//	utf-8                     utf-8               strict            answers
//	utf-8:                    utf-8               strict            answers
//	:                         utf-8               surrogateescape   answers
//	UTF-8 / utf8 / utf_8      utf-8               strict            answers
//	U8 / utf / cp65001        utf-8               strict            answers
//	:replace                  utf-8               replace           REFUSES
//	utf-8:ignore              utf-8               ignore            REFUSES
//	utf-8:backslashreplace    utf-8               backslashreplace  REFUSES
//	utf-8:surrogateescape     utf-8               surrogateescape   answers
//	latin-1                   iso8859-1           strict            REFUSES
//	cp1252                    cp1252              strict            REFUSES
//	ascii                     ascii               strict            REFUSES
//	utf-16                    utf-16              strict            REFUSES
//
// tests/parity/run_parity.sh section 15c ("the OTHER error handler, and the
// assumption behind the default") asks python3 for both handler values at run
// time, and section 15f ("the ENCODING half of PYTHONIOENCODING") asks it for
// both encoding values and pins the divergence the refusal exists for. Either
// FAILS if the answers ever disagree with each other or with what this file
// assumes, rather than trusting the table above to stay true on someone else's
// image.

import (
	"os"
	"strings"
)

// pyIOErrors reproduces CPython's choice of error handler for sys.stdin and
// sys.stdout.
//
// On the way IN it decides whether undecodable bytes are a READ FAILURE or a
// string full of lone surrogates. On the way OUT it decides whether a lone
// surrogate becomes its original byte, raises, or is substituted.
//
// SCOPE OF THE LOCALE RULE: with PYTHONIOENCODING unset, CPython picks
// surrogateescape when the LC_CTYPE locale is the C/POSIX one and strict
// otherwise. Only C, C.utf8 and POSIX are installed here (`locale -a`), and all
// three take the surrogateescape branch, so the unset case is unambiguous *in
// this environment*. Rather than guess at an environment that cannot occur
// here, the harness asks python3 directly and fails if the answer differs — so
// an image that ships a non-POSIX locale reports a broken assumption instead of
// quietly diverging.
func pyIOErrors() string {
	spec, set := os.LookupEnv("PYTHONIOENCODING")
	if !set || spec == "" {
		return "surrogateescape"
	}
	encoding, errors, _ := strings.Cut(spec, ":")
	switch {
	case errors != "":
		return errors
	case encoding != "":
		return "strict"
	default:
		return "surrogateescape"
	}
}

// pyIOEncoding returns the ENCODING half of PYTHONIOENCODING, exactly as the
// caller spelled it, or "" when the variable is unset or names no encoding
// (`:replace`, `:`) and CPython therefore falls back to the locale's codec.
//
// This is the field round 5 of SPGD-107's review found parsed and thrown away:
// `strings.Cut` produced it, one bit of it ("is it non-empty?") was used to
// infer the `strict` handler, and the name itself was discarded. That made
// `PYTHONIOENCODING=latin-1` — whose handler IS `strict`, and therefore
// reproducible — indistinguishable from `utf-8` to the gate below.
func pyIOEncoding() string {
	spec, set := os.LookupEnv("PYTHONIOENCODING")
	if !set {
		return ""
	}
	encoding, _, _ := strings.Cut(spec, ":")
	return encoding
}

// utf8Aliases is every NORMALISED spelling CPython resolves to the utf-8 codec:
// the codec's own name plus the six aliases that map to `utf_8` in
// encodings/aliases.py. Read out of CPython rather than remembered:
//
//	>>> sorted(k for k, v in encodings.aliases.aliases.items() if v == 'utf_8')
//	['cp65001', 'u8', 'utf', 'utf8', 'utf8_ucs2', 'utf8_ucs4']
//
// The membership test is a WHITELIST on purpose, and the direction of its
// failure mode is the reason. An alias this set is missing makes the port refuse
// a run CPython would have answered — visible, exit 2, naming the encoding. A
// blacklist would fail the other way: an unlisted codec would be treated as
// UTF-8 and answered confidently in bytes CPython never wrote, which is the
// exact defect this gate exists to close.
var utf8Aliases = map[string]bool{
	"utf_8":     true,
	"cp65001":   true,
	"u8":        true,
	"utf":       true,
	"utf8":      true,
	"utf8_ucs2": true,
	"utf8_ucs4": true,
}

// pyNormalizeEncoding reproduces encodings.normalize_encoding, which is what
// turns the many spellings of one codec into a single lookup key:
//
//	def normalize_encoding(encoding):
//	    chars = []
//	    punct = False
//	    for c in encoding:
//	        if c.isalnum() or c == '.':
//	            if punct and chars:
//	                chars.append('_')
//	            chars.append(c)
//	            punct = False
//	        else:
//	            punct = True
//	    return ''.join(chars)
//
// Runs of punctuation collapse to one '_' and leading/trailing punctuation
// vanishes, so `utf-8`, `utf_8`, `utf---8` and `  utf 8  ` all become `utf_8`.
// Lower-casing is folded in here because CPython does it a layer up, in
// codecs.lookup's normalizestring, before the alias table is consulted.
//
// DELIBERATELY ASCII-ONLY where CPython's isalnum() is Unicode-aware. A
// non-ASCII letter is treated as punctuation here, which can only ever split a
// name into something the whitelist above does not contain — i.e. it can only
// over-refuse, never under-refuse. Reproducing Unicode isalnum() to widen the
// set of accepted spellings would be work in service of an encoding name nobody
// writes.
func pyNormalizeEncoding(name string) string {
	var out strings.Builder
	punct := false
	for _, r := range name {
		alnum := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !alnum && r != '.' {
			punct = true
			continue
		}
		if punct && out.Len() > 0 {
			out.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out.WriteRune(r)
		punct = false
	}
	return out.String()
}

// pyIOEncodingSupported reports whether the codec in force is the one this port
// hard-codes on both sides: UTF-8.
//
// An EMPTY encoding field is the locale default, and it is accepted because
// pylocale.go has ALREADY refused every environment whose locale default is not
// UTF-8 — the gate above this one in run(). That used to be an assumption
// ("only C, C.utf8 and POSIX are installed here, all three resolve to utf-8"),
// and the assumption was false: `PYTHONUTF8=0 LC_ALL=C` resolves sys.stdin,
// sys.stdout AND the filesystem to `ascii` with nothing in PYTHONIOENCODING set
// at all, which no amount of reading THIS variable can detect. It is now a
// precondition enforced by a gate rather than a property of the image.
//
// tests/parity/run_parity.sh section 15f ("the ENCODING half of
// PYTHONIOENCODING") still asks python3 at run time rather than trusting either
// file, and section 17c ("the locale's default codec") pins the precondition.
func pyIOEncodingSupported() bool {
	encoding := pyIOEncoding()
	if encoding == "" {
		return true
	}
	return utf8Aliases[pyNormalizeEncoding(encoding)]
}

// pyIOHandlerSupported reports whether the handler in force is one this port
// reproduces on BOTH sides. It is HALF the gate — see pyIOUnsupported, which is
// what callers use, and which asks about the encoding field as well.
//
// `strict` and `surrogateescape` are implemented in each direction:
//
//	          reading undecodable bytes        writing a lone surrogate
//	strict    UnicodeDecodeError -> `read`     UnicodeEncodeError, exit 1
//	surrog.   U+DC00+byte, parsed              the original byte back out
//
// `replace`, `ignore`, `backslashreplace`, `namereplace`, ... all SUCCEED in
// both directions and each produces different bytes, so substituting an
// implemented handler for one of them would return a confident verdict about a
// string the reference never saw. The caller set the variable deliberately and
// can be told instead.
//
// THE REFUSAL IS NOT SCOPED TO stdin MODE, and that is the round-4 fix. The
// output side is reachable from every mode: a FILE holding `{"\udc82key": "x"}`
// puts a lone surrogate into the report with no stdin involved (see
// pystdout.go), so a check that only fired for `-` left adopter, --source and
// self-test diverging under the very environment this port already models.
// Measured before the fix, `PYTHONIOENCODING=:replace validate-intent FILE`:
//
//	python3   ... additional property '?key' is not allowed
//	port      ... additional property '\202key' is not allowed
//
// The cost is stated rather than hidden: a caller who sets
// PYTHONIOENCODING=:replace globally and validates plain-ASCII files now gets
// exit 2 where the reference would have answered, because nothing here inspects
// the payload before deciding. Refusing on the environment is the same trade
// this port makes everywhere else — an exit 2 naming the reason beats a clean
// report that is wrong in one line.
func pyIOHandlerSupported() bool {
	switch pyIOErrors() {
	case "strict", "surrogateescape":
		return true
	}
	return false
}

// pyIOUnsupported names the part of PYTHONIOENCODING this port cannot
// reproduce, or reports ok == false when the whole variable is reproducible.
//
// ONE entry point for BOTH fields, because the round-5 defect was structural
// rather than arithmetical: the gate was spelled `pyIOHandlerSupported()`, so it
// could only ever be as wide as its name, and the encoding was not forgotten so
// much as never invited. A caller that asks "is this environment reproducible?"
// now cannot accidentally ask only half the question.
//
// THE ENCODING IS CHECKED FIRST, and the order is observable, so it is a
// decision rather than an accident. `latin-1:replace` is unsupported twice over;
// naming the codec is the more useful diagnostic because it is the field that
// changes which STRING the reference validated, where the handler only changes
// how an unrepresentable character in that string is rendered.
//
// Measured, and the reason this is a refusal rather than a substitution.
// `{"schema":"open-test-intent/v1",...,"layer":"\xe2\x82unit"}` — bytes that are
// not valid UTF-8 — through stdin `--json`:
//
//	PYTHONIOENCODING=utf-8     kind "read",   annotations 0, 1 error,  rc 1
//	PYTHONIOENCODING=latin-1   kind "schema", annotations 1, 5 errors, rc 1
//
// iso8859-1 decodes every byte, so the reference never reaches its read failure
// at all: the document parses and is rejected on SCHEMA. That is this slice's
// own read/parse split (stdin_mode.go, TestRunStdinJSON_readParseSplit),
// inverted, with the same exit code on both sides — which is precisely the shape
// that ships unnoticed. `ascii` is sharper still: the reference raises
// UnicodeEncodeError on the report's own em dash and dies with a truncated
// stdout, where the port had been emitting a full, clean, plausible report.
//
// The cost is stated rather than hidden, and it is the same trade taken for the
// handler: a caller who exports PYTHONIOENCODING=latin-1 globally and validates
// plain-ASCII files now gets exit 2 where the reference would have answered,
// because nothing here inspects the payload before deciding. Reproducing a
// non-UTF-8 output codec — and its decode semantics on the way in — is real work
// and belongs to whoever needs it; answering `latin-1` as though it were UTF-8
// is not a cheaper version of that work, it is a wrong answer.
//
// An encoding CPython does not know at all (`PYTHONIOENCODING=bogus`) is refused
// here too, and that one is an acknowledged non-parity rather than a match: the
// reference dies before main() with a fatal init error and rc 1, the port exits
// 2 naming the encoding. Neither produces a report, so no consumer is misled,
// and reproducing a CPython interpreter-startup failure is not a behaviour of
// this binary. See tests/parity/run_parity.sh section 15f ("the ENCODING half of
// PYTHONIOENCODING").
func pyIOUnsupported() (string, bool) {
	if !pyIOEncodingSupported() {
		return "the PYTHONIOENCODING encoding " + PyReprString(pyIOEncoding()), true
	}
	if !pyIOHandlerSupported() {
		return "the PYTHONIOENCODING error handler " + PyReprString(pyIOErrors()), true
	}
	return "", false
}

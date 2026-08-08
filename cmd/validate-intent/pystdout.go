package main

// stdout, encoded the way CPython's sys.stdout encodes it.
//
// WHY THIS EXISTS. Python's str can hold lone surrogates, and two of this
// tool's paths put them there: `sys.stdin.read()` under the surrogateescape
// error handler maps every undecodable byte to U+DC00+byte (stdin_mode.go), and
// json.loads happily decodes a literal "\udc82" escape out of a file. Both then
// reach the TEXT renderers through the reference's `%s` interpolations of KEY
// names — "missing required property '%s'" and "additional property '%s' is not
// allowed" (bin/validate-intent:169, 178) use %s, not %r, so unlike every other
// interpolated value these are NOT repr'd into ASCII on the way out.
//
// sys.stdout carries the error handler pyIOErrors() reports — the SAME one
// sys.stdin carries, because PYTHONIOENCODING configures both streams together
// (pyioerrors.go has the measured table). Under `surrogateescape` CPython
// writes such a character back out as the ORIGINAL BYTE; under `strict` it
// raises UnicodeEncodeError instead. Go has no equivalent to either: the port
// carries surrogates as WTF-8 (see pystr.go), and writing that straight to the
// fd emits three bytes where the reference emits one. Measured on
// `{"la\xe2\x82yer": "unit"}` piped to stdin, default environment:
//
//	python3   ... additional property 'la\342\202yer' is not allowed
//	naive Go  ... additional property 'la\355\263\242\355\262\202yer' is not allowed
//
// The verdict, the exit code and every other byte agree — which is exactly what
// makes it the kind of divergence that ships.
//
// THE HANDLER IS READ, NOT ASSUMED. This file used to state that sys.stdout
// "carries the same surrogateescape handler as sys.stdin" and apply the
// surrogateescape encoder unconditionally. The first half was the right idea
// stated as the wrong invariant: the two streams do agree, but on whatever
// PYTHONIOENCODING says, which is `strict` under the very `PYTHONIOENCODING=utf-8`
// this port already models on the input side. So the same bytes that are a
// reproduced `read` finding going in were re-encoded going out with a handler
// that was not in force. Round 4 of SPGD-107's review caught it; pyEncodeSurrogates
// now takes the handler as an argument so there is nothing left to assume.
//
// EVERY text-mode write goes through here rather than only stdin's, because the
// json.loads route reaches adopter and --source mode too: a file containing
// `{"\udc82key": "x"}` diverges identically with no stdin involved. --json
// output is routed through here as well; json.dumps' ensure_ascii=True means
// there is nothing left for it to encode, so it costs one fast-path check and
// removes a rule about which writes are special.
//
// The one exception is the `usage` block, written straight to the fd: it is a
// compile-time ASCII constant with no interpolation, so there is nothing here
// that could apply to it.

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// pyPrintf is `print("..." % args, end="")` — fmt.Printf with sys.stdout's
// encoding. Every text renderer in this port calls this instead of fmt.Printf.
func pyPrintf(format string, a ...any) {
	pyWriteStdout(fmt.Sprintf(format, a...))
}

// pyPrintln is `print(...)`.
func pyPrintln(a ...any) {
	pyWriteStdout(fmt.Sprintln(a...))
}

func pyWriteStdout(s string) {
	encoded, bad, ok := pyEncodeSurrogates(s, pyIOErrors())
	if !ok {
		// CPython raises UnicodeEncodeError here and dies with a traceback and
		// exit 1 — verified against the reference on a document whose KEY holds
		// a "\ud800" escape under the default handler, and on one holding
		// "\udc82" under PYTHONIOENCODING=utf-8. That is a defect in the
		// reference, not a surface this port can be byte-identical to (the
		// traceback names CPython's own source lines), so the exit code is
		// reproduced and the reason is stated plainly instead of a
		// plausible-looking line being invented.
		//
		// Nothing of this write reaches stdout, matching TextIOWrapper, which
		// encodes before it buffers — so the bytes already written are
		// byte-identical to the reference's truncated report, and only the
		// stderr prose differs. tests/parity/run_parity.sh section 15e ("lone
		// surrogates on the way OUT") asserts exactly that pair, under both
		// handlers.
		//
		// The offending character goes through writeCodePoint, NOT string(bad):
		// Go's string(rune) silently turns a surrogate into U+FFFD, so the
		// diagnostic would have named the replacement character instead of the
		// one that failed — a message that reads fine and points at nothing.
		// Via WTF-8, PyReprString renders it as CPython's own '\udc82'.
		var offender strings.Builder
		writeCodePoint(&offender, bad)
		fmt.Fprintf(os.Stderr,
			"error: 'utf-8' codec can't encode character %s: surrogates not allowed\n",
			PyReprString(offender.String()))
		fmt.Fprintln(os.Stderr,
			"       (the reference raises UnicodeEncodeError and exits 1 on this input)")
		os.Exit(1)
	}
	os.Stdout.WriteString(encoded)
}

// pyEncodeSurrogates encodes a string the port carries as WTF-8 the way CPython
// encodes it for sys.stdout under the named error handler.
//
// `errors` is pyIOErrors()' answer, threaded in rather than read here so the
// caller cannot forget that this depends on the environment — the round-4
// defect was precisely a hard-coded handler in this function.
//
//	surrogateescape   U+DC80-U+DCFF is the range the DECODER produced on the way
//	                  in, so each maps back to the single byte it came from. Any
//	                  OTHER lone surrogate is one the handler never generated —
//	                  it can only have come from an explicit \uXXXX escape in the
//	                  input — and surrogateescape refuses it, exactly as CPython
//	                  does.
//	strict            EVERY lone surrogate is refused, including U+DC80-U+DCFF.
//	                  Nothing produced that range under this handler: the decoder
//	                  raised instead, so a surrogate here can only be an escape,
//	                  and CPython raises on it.
//
// Any other handler never reaches this function — run() refuses the invocation
// before a single check runs (pyIOHandlerSupported).
//
// Returning the offending character rather than substituting something
// printable keeps the caller able to say which one it was.
func pyEncodeSurrogates(s, errors string) (encoded string, bad rune, ok bool) {
	// Fast path. A surrogate can only be present as WTF-8, which is by
	// definition not valid UTF-8, so a valid string has nothing to encode.
	if utf8.ValidString(s) {
		return s, 0, true
	}
	escaping := errors == "surrogateescape"
	out := make([]byte, 0, len(s))
	for _, r := range pyRunes(s) {
		switch {
		case r < 0xd800 || r > 0xdfff:
			out = utf8.AppendRune(out, r)
		case escaping && r >= 0xdc80 && r <= 0xdcff:
			out = append(out, byte(r&0xff))
		default:
			return "", r, false
		}
	}
	return string(out), 0, true
}

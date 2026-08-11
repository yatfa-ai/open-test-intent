package main

// stdin mode (`-`), text and --json — the port of `run_stdin` and
// `run_stdin_json` (bin/validate-intent:813-866).
//
// This is the roadmap's programmatic input path, so it is also the mode where a
// silent divergence costs the most: the caller has no filename to sanity-check
// the answer against, only the document that came back.

import (
	"io"
	"os"
	"unicode/utf8"
)

// --------------------------------------------------------------------------- //
// reading stdin the way CPython does
// --------------------------------------------------------------------------- //

// readStdin reads stdin and decodes it exactly as sys.stdin.read() would,
// returning the text or the prose of the UnicodeDecodeError it raised.
//
// Which handler is in force is pyIOErrors' answer, not this file's — see
// pyioerrors.go, where the same value also governs the way out.
//
// The two handlers are genuinely different verdicts, not two spellings of one:
// under `strict`, `{"layer":"\xe2\x82"}` never reaches the parser and is a
// `read` finding with `annotations: 0`; under `surrogateescape` the very same
// bytes parse fine and come back as a `schema` finding with `annotations: 1`.
// Implementing only one of them would be right about half the invocations and
// confidently wrong about the other half.
//
// Only those two are implemented; run() refuses any other handler up front
// (see pyIOHandlerSupported) rather than letting one of these stand in for it.
func readStdin() (text string, readError string) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		// An I/O failure on the stream itself, not a decoding failure. Python
		// raises OSError here, which run_stdin does NOT catch — it propagates
		// as a traceback and exit 1. Reproducing a traceback is not something
		// this port attempts, so it is reported as the read failure it is.
		return "", "could not read/parse JSON: " + pyOSError(err)
	}
	if pyIOErrors() == "strict" && !utf8.Valid(data) {
		return "", "could not read/parse JSON: " + pyUnicodeDecodeError(data)
	}
	return pySurrogateEscape(data), ""
}

// --------------------------------------------------------------------------- //
// the two renderers
// --------------------------------------------------------------------------- //

// RunStdin is the port of `run_stdin` (bin/validate-intent:813-834): validate a
// single annotation object read from stdin.
//
// A document that is not valid JSON is a FAILURE (exit 1), not a silent success.
// Note that the reference catches UnicodeDecodeError and JSONDecodeError in one
// `except` here and renders them with one message — the text renderer has no
// `kind` field to tell them apart, so it does not try to. The JSON renderer
// below does, and that asymmetry is the reference's, reproduced rather than
// tidied into a shared helper that would have to be wrong for one of them.
func RunStdin(schema *Schema) int {
	raw, readError := readStdin()
	if readError != "" {
		pyPrintf("FAIL — %s\n", readError)
		return 1
	}
	instance, err := DecodeOrdered([]byte(raw))
	if err != nil {
		pyPrintf("FAIL — could not read/parse JSON: %s\n", err)
		return 1
	}
	errs := schema.Validate(instance)
	if len(errs) > 0 {
		pyPrintln("FAIL")
		for _, e := range errs {
			pyPrintf("        -> %s\n", e)
		}
		return 1
	}
	pyPrintln("PASS")
	return 0
}

// RunStdinJSON is the port of `run_stdin_json` (bin/validate-intent:837-866):
// the --json renderer for stdin mode — always exactly one finding.
//
// The finding's `file` is "-", the same sentinel the caller passed, so a
// consumer aggregating findings across invocations still has a non-null key for
// the stdin document. `files` stays 0: stdin is not a file, and counting it as
// one would make `files` disagree with the number of paths the caller named.
//
// THE READ/PARSE SPLIT IS DELIBERATE, and it is the one thing here a tidier port
// gets wrong. The reference hoists the read out of the json.loads try
// (bin/validate-intent:851-858) so the two failures stay distinguishable:
//
//	bytes that never decoded  -> kind "read",  annotations 0
//	decoded, then rejected    -> kind "parse", annotations 1
//
// `annotations` counts ANNOTATION SITES EXAMINED. A stream that decoded had a
// site — its payload was simply bad — while a stream that never decoded had
// nothing to examine, exactly as an unreadable file contributes none. Collapsing
// the two try blocks into one changes BOTH the kind and the count, and the
// resulting document still looks entirely reasonable.
func RunStdinJSON(schema *Schema) int {
	report := &JSONReport{Mode: "stdin"}

	raw, readError := readStdin()
	if readError != "" {
		report.Add(JSONFinding{File: "-", OK: false, Kind: KindRead, Errors: []string{readError}})
		return report.Emit(1)
	}
	report.Annotations = 1

	instance, err := DecodeOrdered([]byte(raw))
	if err != nil {
		report.Add(JSONFinding{
			File: "-", OK: false, Kind: KindParse,
			Errors: []string{"could not read/parse JSON: " + err.Error()},
		})
		return report.Emit(1)
	}

	errs := schema.Validate(instance)
	kind := ""
	if len(errs) > 0 {
		kind = KindSchema
	}
	report.Add(JSONFinding{File: "-", OK: len(errs) == 0, Kind: kind, Errors: errs, Intent: instance})
	if len(errs) > 0 {
		return report.Emit(1)
	}
	return report.Emit(0)
}

package main

// stdin mode (`-`), text and --json.
//
// This is the programmatic input path, so it is the mode where a wrong answer
// costs the most: the caller has no filename to sanity-check the verdict
// against, only the document that came back.

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// readStdin reads the whole stream and decodes it as UTF-8, returning the text
// or the prose of the failure.
//
// PROTOCOL.md §1.1 makes UTF-8 part of the definition of a JSON text, so bytes
// that are not well-formed UTF-8 are a READ failure: nothing was parsed, and
// nothing is repaired. That distinction is visible in the --json document —
// `kind: "read"` with `annotations: 0`, against `kind: "parse"` with
// `annotations: 1` for a stream that decoded and then failed to parse.
func readStdin() (text string, readError string) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		// An I/O failure on the stream itself, not a decoding failure. Reported
		// as the read failure it is rather than being allowed to escape as a
		// panic: a caller piping into this mode gets a finding, not a stack.
		return "", "could not read/parse JSON: " + err.Error()
	}
	if !utf8.Valid(data) {
		return "", "could not read/parse JSON: " + errNotUTF8
	}
	return string(data), ""
}

// --------------------------------------------------------------------------- //
// the two renderers
// --------------------------------------------------------------------------- //

// RunStdin validates a single annotation object read from stdin.
//
// A document that is not valid JSON is a FAILURE (exit 1), not a silent success.
// The text renderer has no `kind` field, so it reports a read failure and a
// parse failure with one message; the JSON renderer below keeps them apart.
// That asymmetry is deliberate — text mode has nowhere to put the distinction,
// and inventing a second prose line for it would give a human two sentences
// that mean the same thing.
func RunStdin(schema *Schema) int {
	raw, readError := readStdin()
	if readError != "" {
		fmt.Printf("FAIL — %s\n", readError)
		return 1
	}
	instance, err := DecodeJSON([]byte(raw))
	if err != nil {
		fmt.Printf("FAIL — could not read/parse JSON: %s\n", err)
		return 1
	}
	errs := schema.Validate(instance)
	if len(errs) > 0 {
		fmt.Println("FAIL")
		for _, e := range errs {
			fmt.Printf("        -> %s\n", e)
		}
		return 1
	}
	fmt.Println("PASS")
	return 0
}

// RunStdinJSON is the --json renderer for stdin mode — always exactly one
// finding.
//
// The finding's `file` is "-", the same sentinel the caller passed, so a
// consumer aggregating findings across invocations still has a non-null key for
// the stdin document. `files` stays 0: stdin is not a file, and counting it as
// one would make `files` disagree with the number of paths the caller named.
//
// THE READ/PARSE SPLIT IS DELIBERATE, and it is the one thing here a tidier
// rewrite gets wrong. The read is hoisted out of the parse so the two failures
// stay distinguishable:
//
//	bytes that never decoded  -> kind "read",  annotations 0
//	decoded, then rejected    -> kind "parse", annotations 1
//
// `annotations` counts ANNOTATION SITES EXAMINED. A stream that decoded had a
// site — its payload was simply bad — while a stream that never decoded had
// nothing to examine, exactly as an unreadable file contributes none. Merging
// the two branches changes BOTH the kind and the count, and the resulting
// document still looks entirely reasonable.
func RunStdinJSON(schema *Schema) int {
	report := &JSONReport{Mode: "stdin"}

	raw, readError := readStdin()
	if readError != "" {
		report.Add(JSONFinding{File: "-", OK: false, Kind: KindRead, Errors: []string{readError}})
		return report.Emit(1)
	}
	report.Annotations = 1

	instance, err := DecodeJSON([]byte(raw))
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
	report.Add(JSONFinding{File: "-", OK: len(errs) == 0, Kind: kind, Errors: errs})
	if len(errs) > 0 {
		return report.Emit(1)
	}
	return report.Emit(0)
}

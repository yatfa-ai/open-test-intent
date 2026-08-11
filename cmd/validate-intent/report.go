package main

// Machine-readable reporting (`--json`) — the port of `_JsonReport`,
// `run_adopter_json` and `run_source_json` (bin/validate-intent:504-579,
// 731-753, 785-810).
//
// ONE reporter, three modes. The renderers differ only in what they count and
// what a finding is FOR — stdin has exactly one and no files, adopter one per
// file, --source one per annotation — and they share this document, this key
// order and this escaping. Forking a second encoder for a later mode is the
// failure `_JsonReport`'s own docstring exists to prevent: the two drift, and
// the drift is invisible to any consumer that parses before comparing.
//
// Slice 3 (SPGD-107) added the adopter half below and the stdin half in
// stdin_mode.go, completing the matrix. Nothing here is refused any more.

import (
	"fmt"
	"math"
	"strings"
)

// jsonSchemaID is the reference's JSON_SCHEMA_ID (bin/validate-intent:507).
const jsonSchemaID = "open-test-intent.v1.json"

// JSONFinding is one entry of the document's `findings` array.
//
// Every finding has the same shape regardless of mode:
// {"file", "line", "ok", "kind", "errors", "intent"}. `line` is null where a
// finding is not line-scoped, `kind` is null on a passing finding, and `errors`
// is ALWAYS a list of strings so a consumer never has to branch on its type.
//
// `Intent` is WHAT THE PAYLOAD PARSED TO, and it is emitted in every mode
// rather than only in the one that motivated it. This document's whole contract
// is that a consumer never branches on which mode produced a finding; a key
// present under --source and absent elsewhere would make the shape
// mode-dependent and hand that branch straight back. So it is null wherever
// there is no payload — read failures, extraction failures, parse failures,
// no-match — and the decoded value wherever there is one, INCLUDING on an
// annotation the schema rejected: it parsed, and `ok` already reports the
// verdict. The field answers "what does this say", not "is this good".
//
// A nil Intent renders `null`, which is also what a payload whose entire
// content is the literal `null` renders as. The two are therefore
// indistinguishable in the document — and deliberately so, because the
// reference collapses them the same way (its `instance` is `None` in both
// cases). A HasIntent flag here would make the port MORE precise than the
// oracle, which is a parity failure, not an improvement.
type JSONFinding struct {
	File    string
	Line    int  // meaningful only when HasLine
	HasLine bool // false renders `"line": null`
	OK      bool
	Kind    string // "" renders `"kind": null`
	Errors  []string
	Intent  Value // nil renders `"intent": null`
}

// JSONReport collects findings for --json and emits the single stdout document.
//
// Text mode flattens the structured result — which file, which line, which rule
// — into prose at the print site, leaving a consumer only the exit code. This
// collects the same findings instead of printing them, so the --json modes are
// the *same* checks with a different renderer, not a second implementation that
// could drift.
//
// Annotations counts ANNOTATION SITES EXAMINED, the same way in every mode: a
// site whose payload could not be captured or parsed still counts (it was there,
// it was bad), but input that could not be read at all contributes no sites.
// Summing `annotations` across modes is therefore meaningful rather than
// mode-dependent.
type JSONReport struct {
	Mode        string
	Files       int
	Annotations int
	Findings    []JSONFinding
}

// Add records one finding and returns true when it FAILED — the `check_one`
// contract runOverPatterns expects.
func (r *JSONReport) Add(finding JSONFinding) bool {
	if finding.Errors == nil {
		finding.Errors = []string{}
	}
	r.Findings = append(r.Findings, finding)
	return !finding.OK
}

// NoMatch is the on-no-match hook: a pattern matching nothing is a finding too,
// so a stdout-only consumer is never left with a clean pass list and an
// unexplained non-zero exit.
//
// Formatted with %s, not the text path's %r: inside a JSON string a Python
// repr's quoting is noise a consumer has to strip, and `file` already carries
// the pattern verbatim. That asymmetry is in the reference
// (bin/validate-intent:551-558) and is reproduced rather than tidied.
func (r *JSONReport) NoMatch(pattern string) {
	r.Add(JSONFinding{
		File:   pattern,
		OK:     false,
		Kind:   KindNoMatch,
		Errors: []string{"no file(s) match " + pattern},
	})
}

// Emit prints the document and passes exitCode straight through.
//
// `ok` is derived from the exit code the TEXT path would also have produced,
// rather than recomputed from the findings, so the two renderers cannot disagree
// about whether the run passed.
func (r *JSONReport) Emit(exitCode int) int {
	failed := 0
	for _, finding := range r.Findings {
		if !finding.OK {
			failed++
		}
	}

	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"schema\": %s,\n", pyJSONDumpsString(jsonSchemaID))
	fmt.Fprintf(&b, "  \"mode\": %s,\n", pyJSONDumpsString(r.Mode))
	fmt.Fprintf(&b, "  \"ok\": %s,\n", jsonBool(exitCode == 0))
	b.WriteString("  \"summary\": {\n")
	fmt.Fprintf(&b, "    \"files\": %d,\n", r.Files)
	fmt.Fprintf(&b, "    \"annotations\": %d,\n", r.Annotations)
	fmt.Fprintf(&b, "    \"failed\": %d\n", failed)
	b.WriteString("  },\n")
	b.WriteString("  \"findings\": " + renderFindings(r.Findings) + "\n")
	b.WriteString("}")

	pyPrintln(b.String())
	return exitCode
}

// renderFindings reproduces json.dumps(..., indent=2) for the findings array.
//
// Written by hand rather than with encoding/json for three reasons, each of
// which alone would be disqualifying: encoding/json escapes <, > and & and does
// NOT escape non-ASCII (json.dumps does the exact opposite on both counts — see
// pyJSONDumpsString); it sorts or reflects rather than preserving the
// reference's key order; and Python renders an EMPTY list as `[]` on one line
// while indenting a non-empty one, which no Go marshaller does by default. The
// em dashes and repr'd quotes that show up in error strings make the escaping
// difference immediately visible rather than theoretical.
func renderFindings(findings []JSONFinding) string {
	if len(findings) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		var b strings.Builder
		b.WriteString("    {\n")
		fmt.Fprintf(&b, "      \"file\": %s,\n", pyJSONDumpsString(f.File))
		if f.HasLine {
			fmt.Fprintf(&b, "      \"line\": %d,\n", f.Line)
		} else {
			b.WriteString("      \"line\": null,\n")
		}
		fmt.Fprintf(&b, "      \"ok\": %s,\n", jsonBool(f.OK))
		if f.Kind == "" {
			b.WriteString("      \"kind\": null,\n")
		} else {
			fmt.Fprintf(&b, "      \"kind\": %s,\n", pyJSONDumpsString(f.Kind))
		}
		b.WriteString("      \"errors\": " + renderErrors(f.Errors) + ",\n")
		b.WriteString("      \"intent\": " + renderJSONValue(f.Intent, 6) + "\n")
		b.WriteString("    }")
		parts = append(parts, b.String())
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n  ]"
}

// renderJSONValue reproduces json.dumps(value, indent=2) for an arbitrary
// decoded value, nested at `indent` spaces.
//
// `indent` is the column the value's CLOSING delimiter sits at — i.e. the
// indentation of the line the value starts on — so its members are written at
// indent+2. A finding's keys are at column 6, which is why the one call site
// passes 6 and the nested object lands at 8, exactly where renderErrors already
// puts an error string.
//
// This is a THIRD hand-written encoder in this file, and the reasons
// renderFindings gives for not reaching for encoding/json all apply again with
// one addition that is specific to arbitrary values: an intent is user text,
// so it is the one field where a `<`, `>`, `&` or a non-ASCII character is
// likely rather than theoretical — and those are precisely the four characters
// encoding/json and json.dumps disagree about. pyJSONDumpsString settles them
// the reference's way, including the lone surrogate that motivated this whole
// key (a payload CPython accepts and re-emits as `\ud800`).
//
// The empty-container cases are not tidy-up: Python renders an empty list as
// `[]` and an empty dict as `{}` on ONE line while indenting a non-empty one,
// so a renderer that always expands emits `[\n\n  ]` where the oracle emits
// `[]`.
func renderJSONValue(v Value, indent int) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return jsonBool(t)
	case string:
		return pyJSONDumpsString(t)
	case Number:
		return jsonNumber(t)
	case []Value:
		if len(t) == 0 {
			return "[]"
		}
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, indentOf(indent+2)+renderJSONValue(item, indent+2))
		}
		return "[\n" + strings.Join(parts, ",\n") + "\n" + indentOf(indent) + "]"
	case *Object:
		keys := t.Keys()
		if len(keys) == 0 {
			return "{}"
		}
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			value, _ := t.Get(key)
			parts = append(parts, indentOf(indent+2)+pyJSONDumpsString(key)+": "+
				renderJSONValue(value, indent+2))
		}
		return "{\n" + strings.Join(parts, ",\n") + "\n" + indentOf(indent) + "}"
	}
	// Unreachable: DecodeOrdered produces exactly the six cases above. Rendering
	// `null` rather than panicking keeps a hypothetical seventh from taking down
	// a run, and it is the honest answer — this encoder could not say what the
	// value was.
	return "null"
}

func indentOf(n int) string {
	return strings.Repeat(" ", n)
}

// jsonNumber renders a decoded number the way json.dumps does, which is NOT the
// way repr does: the non-finite values Python's parser accepts (`NaN`,
// `Infinity`, `-Infinity`, and any literal that overflows to infinity such as
// `1e400`) come back out with those spellings, where PyReprFloat — correctly,
// for its own callers — gives `nan`, `inf` and `-inf`. Every finite value is
// float.__repr__/int.__repr__, which is what json.dumps uses, so PyReprFloat
// serves the rest unchanged.
func jsonNumber(n Number) string {
	if n.IsInt && n.Int != nil {
		return n.Int.String()
	}
	switch {
	case math.IsInf(n.Float, 1):
		return "Infinity"
	case math.IsInf(n.Float, -1):
		return "-Infinity"
	case math.IsNaN(n.Float):
		return "NaN"
	}
	return PyReprFloat(n.Float)
}

func renderErrors(errs []string) string {
	if len(errs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, "        "+pyJSONDumpsString(err))
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n      ]"
}

func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// RunAdopterJSON is the port of `run_adopter_json` (bin/validate-intent:731-753):
// the --json renderer for adopter mode — one finding per file checked.
//
// THREE DIFFERENT COUNTING RULES share these few lines, and a port that reaches
// for one counter and reuses it collapses them. Measured on a mixed batch
// (valid + malformed + schema-failing + unreadable + a non-matching glob), the
// reference answers `files: 4, annotations: 3, failed: 4` — three different
// numbers over five arguments:
//
//	files       every file the globs matched, readable or not (4).
//	            The no-match PATTERN is not a file and is not counted.
//	annotations annotation SITES EXAMINED (3), the same rule --source uses. In
//	            adopter mode each file is one site, so the MALFORMED file counts
//	            — the site existed, its payload was unparseable — while the
//	            UNREADABLE one does not, because there was nothing to examine.
//	            This is the only place `kind` is consulted for arithmetic rather
//	            than for reporting.
//	failed      every failing finding (4), INCLUDING the no-match, which
//	            contributed to neither of the other two.
//
// Summing `annotations` across modes is meaningful precisely because the
// malformed/unreadable distinction is drawn the same way everywhere.
func RunAdopterJSON(patterns []string, schema *Schema) int {
	report := &JSONReport{Mode: "adopter"}

	checkOne := func(path string) bool {
		report.Files++
		valid, errs, parseError, kind, instance := CheckFile(path, schema)
		if parseError != "" {
			if kind != KindRead {
				report.Annotations++
			}
			return report.Add(JSONFinding{
				File: path, OK: false, Kind: kind, Errors: []string{parseError},
			})
		}
		report.Annotations++
		findingKind := KindSchema
		if valid {
			// A passing finding carries `kind: null`, not "schema": `kind`
			// names why a finding FAILED, and a non-null kind on a passing row
			// would read as a failure a consumer then has to second-guess.
			findingKind = ""
		}
		return report.Add(JSONFinding{
			File: path, OK: valid, Kind: findingKind, Errors: errs, Intent: instance,
		})
	}

	return report.Emit(runOverPatterns(patterns, checkOne, report.NoMatch))
}

// RunSourceJSON is the port of `run_source_json` (bin/validate-intent:785-810):
// the --json renderer for --source mode, one finding per annotation.
//
// A file carrying NO annotations contributes to summary.files and no findings.
// Text mode's `----` line is the absence of anything to report, not a result,
// and emitting it as a finding would inflate the annotation count with rows a
// consumer then has to filter back out. That asymmetry is deliberate and is
// pinned by a parity case.
func RunSourceJSON(patterns []string, schema *Schema) int {
	report := &JSONReport{Mode: "source"}

	checkOne := func(path string) bool {
		report.Files++
		findings, readError := CheckSourceFile(path, schema)
		if readError != "" {
			// A file that could not be read contributes NO annotation sites —
			// the same rule an unreadable adopter file follows.
			return report.Add(JSONFinding{
				File: path, OK: false, Kind: KindRead, Errors: []string{readError},
			})
		}
		failed := false
		for _, finding := range findings {
			report.Annotations++
			// `problem` and `errors` are the same thing to a consumer: why this
			// annotation failed. `kind` is what tells them apart, so both are
			// normalized into the one `errors` list.
			errs := finding.Errors
			if finding.Problem != "" {
				errs = []string{finding.Problem}
			}
			if report.Add(JSONFinding{
				File: path, Line: finding.Line, HasLine: true,
				OK: finding.Valid, Kind: finding.Kind, Errors: errs,
				Intent: finding.Intent,
			}) {
				failed = true
			}
		}
		return failed
	}

	return report.Emit(runOverPatterns(patterns, checkOne, report.NoMatch))
}

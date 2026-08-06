package main

// Machine-readable reporting (`--json`) — the port of `_JsonReport` and
// `run_source_json` (bin/validate-intent:504-579, 785-810).
//
// SCOPE, and a correction to the originating proposal: slice 1 does NOT provide
// a reporter this could be "wired into". SPGD-98 lists --json as out of scope
// and refuses it with exit 2, so the whole rendering path is built here. Only
// the `--source` half is in scope for SPGD-102; adopter-mode and stdin --json
// remain refused, and tests/parity/run_parity.sh asserts that refusal so
// "out of scope" cannot quietly become "falls through to the wrong renderer".

import (
	"fmt"
	"strings"
)

// jsonSchemaID is the reference's JSON_SCHEMA_ID (bin/validate-intent:507).
const jsonSchemaID = "open-test-intent.v1.json"

// JSONFinding is one entry of the document's `findings` array.
//
// Every finding has the same shape regardless of mode:
// {"file", "line", "ok", "kind", "errors"}. `line` is null where a finding is
// not line-scoped, `kind` is null on a passing finding, and `errors` is ALWAYS a
// list of strings so a consumer never has to branch on its type.
type JSONFinding struct {
	File    string
	Line    int  // meaningful only when HasLine
	HasLine bool // false renders `"line": null`
	OK      bool
	Kind    string // "" renders `"kind": null`
	Errors  []string
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

	fmt.Println(b.String())
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
		b.WriteString("      \"errors\": " + renderErrors(f.Errors) + "\n")
		b.WriteString("    }")
		parts = append(parts, b.String())
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n  ]"
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
			}) {
				failed = true
			}
		}
		return failed
	}

	return report.Emit(runOverPatterns(patterns, checkOne, report.NoMatch))
}

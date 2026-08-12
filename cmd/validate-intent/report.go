package main

// Machine-readable reporting (`--json`).
//
// ONE reporter, three modes. The renderers differ only in what they count and
// what a finding is FOR — stdin has exactly one and no files, adopter one per
// file, --source one per annotation — and they share this document, this key
// order and this escaping. Forking a second encoder for a fourth mode is the
// failure this type exists to prevent: the two drift, and the drift is
// invisible to any consumer that parses before comparing.

import (
	"fmt"
	"strings"
)

// jsonSchemaID names the contract a finding was graded against. It is the
// document's self-description, not a path: a consumer reads it to know which
// version of the protocol produced these findings.
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
// The message carries the pattern BARE, where the text path quotes it: inside a
// JSON string a second layer of quoting is noise a consumer has to strip, and
// `file` already carries the pattern verbatim.
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
	fmt.Fprintf(&b, "  \"schema\": %s,\n", EncodeJSONString(jsonSchemaID))
	fmt.Fprintf(&b, "  \"mode\": %s,\n", EncodeJSONString(r.Mode))
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

// renderFindings writes the findings array, two-space indented.
//
// Written by hand rather than with encoding/json for two reasons, either of
// which alone would be disqualifying: encoding/json escapes <, > and & — a
// legacy of embedding JSON in HTML — which mangles every diagnostic quoting an
// author's payload; and it reflects or sorts rather than preserving this
// document's fixed key order, which a consumer diffing two reports depends on.
// See EncodeJSONString in render.go.
//
// `file` is encoded by EncodeJSONPath rather than EncodeJSONString: it carries
// operating-system bytes and it is the key a consumer groups and compares on,
// so it is the one value in the document that has to be injective. The rest of
// a finding is prose.
func renderFindings(findings []JSONFinding) string {
	if len(findings) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		var b strings.Builder
		b.WriteString("    {\n")
		fmt.Fprintf(&b, "      \"file\": %s,\n", EncodeJSONPath(f.File))
		if f.HasLine {
			fmt.Fprintf(&b, "      \"line\": %d,\n", f.Line)
		} else {
			b.WriteString("      \"line\": null,\n")
		}
		fmt.Fprintf(&b, "      \"ok\": %s,\n", jsonBool(f.OK))
		if f.Kind == "" {
			b.WriteString("      \"kind\": null,\n")
		} else {
			fmt.Fprintf(&b, "      \"kind\": %s,\n", EncodeJSONString(f.Kind))
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
		parts = append(parts, "        "+EncodeJSONString(err))
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n      ]"
}

func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// RunAdopterJSON is the --json renderer for adopter mode — one finding per file
// checked.
//
// THREE DIFFERENT COUNTING RULES share these few lines, and a rewrite that
// reaches for one counter and reuses it collapses them. On a mixed batch (valid
// + malformed + schema-failing + unreadable + a non-matching glob) the answer is
// `files: 4, annotations: 3, failed: 4` — three different numbers over five
// arguments:
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
		valid, errs, parseError, kind := CheckFile(path, schema)
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
		return report.Add(JSONFinding{File: path, OK: valid, Kind: findingKind, Errors: errs})
	}

	return report.Emit(runOverPatterns(patterns, checkOne, report.NoMatch))
}

// RunSourceJSON is the --json renderer for --source mode, one finding per
// annotation.
//
// A file carrying NO annotations contributes to summary.files and no findings.
// Text mode's `----` line is the absence of anything to report, not a result,
// and emitting it as a finding would inflate the annotation count with rows a
// consumer then has to filter back out. That asymmetry is deliberate.
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

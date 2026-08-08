package main

// The `--source` mode — the port of `run_source` (bin/validate-intent:755-782).

import "fmt"

// RunSource validates `@intent` annotations in place inside test source file(s).
//
// It mirrors RunAdopter but reads *source* rather than strict JSON, and reports
// each finding at its `file:line` — the location information the other modes
// cannot give. A file with no annotations is reported and skipped, not failed:
// the protocol allows unannotated tests.
//
// Note that the `----` line for such a file is the *absence* of anything to
// report, not a result. It is why this mode cannot be collapsed into adopter
// mode's one-finding-per-file shape, and it is the distinction the --json
// renderer has to preserve (see report.go).
func RunSource(patterns []string, schema *Schema) int {
	checkOne := func(path string) bool {
		findings, readError := CheckSourceFile(path, schema)
		if readError != "" {
			pyPrintf("FAIL  %s — %s\n", path, readError)
			return true
		}
		if len(findings) == 0 {
			pyPrintf("----  %s — no @intent annotations\n", path)
			return false
		}
		failed := false
		for _, finding := range findings {
			where := fmt.Sprintf("%s:%d", path, finding.Line)
			if finding.Valid {
				pyPrintf("PASS  %s\n", where)
				continue
			}
			if finding.Problem != "" {
				// The em dash (U+2014) is load-bearing output, not decoration.
				pyPrintf("FAIL  %s — %s\n", where, finding.Problem)
			} else {
				pyPrintf("FAIL  %s\n", where)
			}
			for _, err := range finding.Errors {
				pyPrintf("        -> %s\n", err)
			}
			failed = true
		}
		return failed
	}
	return runOverPatterns(patterns, checkOne, nil)
}

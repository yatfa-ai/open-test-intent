// Package report holds the one translation the release-verification tools in
// tests/cross share: turning a (problems, notes, err) triple into the process
// exit code their callers read.
//
// It exists because that translation is a CONTRACT, not a formatting detail.
// tests/cross/run_cross_build.sh reads it with `case $?` and discriminates all
// three outcomes, with a comment explaining that collapsing them (`if ! ...`)
// would report "could not check" as "checked and wrong" — the vacuous-green
// shape this repo keeps having to name. scripts/build-release.sh is a second
// consumer. When each tool carried its own copy of the block, exit 1 could
// become exit 0 in one of them by a one-character edit, leaving that tool
// printing every problem it found and still reporting success. One copy means
// one place to get that right and one place to test it.
package report

import (
	"fmt"
	"os"
)

// Exit prints the outcome and terminates the process with the code that
// outcome means. It does not return.
//
// The three outcomes, and the exact output each produces:
//
//   - err non-nil — the thing could not be examined at all. Writes
//     "<tool>: <err>" to stderr and exits 2. This is deliberately NOT exit 1:
//     "could not check" is a different fact from "checked and found wrong",
//     and a caller must never be able to read one as the other, or as a pass.
//   - problems non-empty — examined, and wrong. Writes each problem to stderr
//     on its own line prefixed with "  FAIL  ", ALL of them rather than only
//     the first, so one run reports the whole story. Exits 1.
//   - otherwise — examined, and right. Exits 0.
//
// notes are observations worth printing on a pass. They go to stdout, before
// any problems, whether or not problems follow: they are what makes a passing
// run auditable instead of a bare silent zero.
//
// tool is the program name to prefix the error line with. It is a parameter
// rather than a constant precisely because more than one tool shares this;
// baking in a name here would put the wrong program in the diagnostic.
func Exit(tool string, problems, notes []string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", tool, err)
		os.Exit(2)
	}

	for _, n := range notes {
		fmt.Println(n)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  FAIL  "+p)
		}
		os.Exit(1)
	}
	os.Exit(0)
}

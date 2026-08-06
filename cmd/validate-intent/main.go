// Command validate-intent is the Go port of bin/validate-intent.
//
// Slice 1 implements the adopter mode — `validate-intent FILE...`, where each
// argument is a path or glob validated as a standalone intent JSON document —
// plus `-h`/`--help`. Its stdout, stderr and exit code are proven byte-identical
// to `python3 bin/validate-intent` over the same arguments by
// tests/parity/run_parity.sh, which is the acceptance test for this port.
//
// Self-test mode, stdin (`-`), `--source` and `--json` are later slices. They
// are not silently absent: each refuses with exit 2 and a diagnostic naming
// itself. That matters more than it looks. Python's main() falls through to
// adopter mode for anything it does not recognise, so a naive port would read
// `--source foo.rb` as a *filename glob*, find no match, and report a confident
// "no file(s) match" — the wrong answer delivered with the right formatting.
// This project has a name for that failure mode (a gate reporting success, or a
// specific-sounding failure, having verified nothing), and refusing loudly is
// the fix.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: validate-intent                    # self-test the in-repo fixtures
       validate-intent -                  # validate one annotation JSON read from stdin
       validate-intent FILE...            # validate FILE(s)/glob(s) as valid intent JSON
       validate-intent --source FILE...   # validate @intent annotations inside test
                                          #   source files (.rb/.py/.js/...), reported
                                          #   per finding as file:line. Alias: -s

       --json   emit one machine-readable JSON document on stdout instead of the
                human report — for the stdin, FILE... and --source modes only.
                Position-independent. Exit codes are identical either way.

Validates JSON against schemas/open-test-intent.v1.json (zero dependencies).
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	// Checked first, exactly as in the reference (bin/validate-intent:886), so
	// `--help` wins over everything else on the command line.
	for _, arg := range argv {
		if arg == "-h" || arg == "--help" {
			os.Stdout.WriteString(usage)
			return 0
		}
	}

	// Out-of-scope surfaces, refused before anything else can misread them.
	// These sit ahead of the schema load on purpose: "this mode is not in the Go
	// port" is the actionable diagnostic for them, and it stays the same answer
	// whether or not the schema happens to be loadable.
	for _, arg := range argv {
		if arg == "--json" {
			return notImplemented("--json output")
		}
	}
	if len(argv) == 0 {
		return notImplemented("self-test mode (run with no arguments)")
	}
	if argv[0] == "-" {
		return notImplemented("stdin mode (the `-` argument)")
	}
	if argv[0] == "-s" || argv[0] == "--source" {
		return notImplemented("source mode (`" + argv[0] + "`)")
	}
	for _, arg := range argv {
		if hasRecursiveComponent(arg) {
			return notImplemented("recursive `**` glob patterns, as in " + PyReprString(arg))
		}
	}

	schema, schemaPath, err := LoadSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not load schema %s: %s\n", schemaPath, err)
		return 2
	}

	return RunAdopter(argv, schema)
}

// notImplemented refuses a surface this slice does not implement. Exit 2 is the
// reference's "usage error / could not load the schema" code
// (bin/validate-intent:67) — the same class of "this run produced no verdict".
func notImplemented(surface string) int {
	fmt.Fprintf(os.Stderr,
		"error: %s is not implemented in the Go port yet; use `python3 bin/validate-intent` for it\n",
		surface)
	return 2
}

// RunAdopter is the port of `run_adopter` (bin/validate-intent:713-728):
// validate the given path(s)/glob(s) as valid intent JSON.
func RunAdopter(patterns []string, schema *Schema) int {
	checkOne := func(path string) bool {
		valid, errs, parseError, _ := CheckFile(path, schema)
		if parseError != "" {
			fmt.Printf("FAIL  %s — %s\n", path, parseError)
			return true
		}
		if valid {
			fmt.Printf("PASS  %s\n", path)
			return false
		}
		fmt.Printf("FAIL  %s\n", path)
		for _, err := range errs {
			fmt.Printf("        -> %s\n", err)
		}
		return true
	}
	return runOverPatterns(patterns, checkOne)
}

// runOverPatterns is the port of `_run_over_patterns`
// (bin/validate-intent:467-501): expand each pattern and run checkOne over
// every file it matches.
//
// checkOne returns true when that file failed. The aggregate exit code is 1 if
// any file failed *or* any pattern matched nothing, else 0 — a pattern matching
// nothing is never a silent pass.
//
// The `on_no_match` hook the reference takes is omitted: its only caller is the
// --json renderer, which is a later slice.
func runOverPatterns(patterns []string, checkOne func(string) bool) int {
	exitCode := 0
	for _, pattern := range patterns {
		files := ExpandFiles(pattern)
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "error: no file(s) match %s\n", PyReprString(pattern))
			exitCode = 1
			continue
		}
		for _, path := range files {
			if checkOne(path) {
				exitCode = 1
			}
		}
	}
	return exitCode
}

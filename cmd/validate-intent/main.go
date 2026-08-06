// Command validate-intent is the Go port of bin/validate-intent.
//
// Implemented and proven byte-identical to `python3 bin/validate-intent` by
// tests/parity/run_parity.sh, which is the acceptance test for this port:
//
//	FILE...            adopter mode        (slice 1, SPGD-98)
//	-h / --help        usage               (slice 1)
//	--source / -s      in-source mode      (slice 2, SPGD-102)
//	<no arguments>     self-test           (slice 2)
//	--source --json    machine-readable    (slice 2)
//
// Still a later slice: stdin (`-`), and `--json` for the stdin and FILE... modes.
// They are not silently absent — each refuses with exit 2 and a diagnostic
// naming itself. That matters more than it looks. Python's main() falls through
// to adopter mode for anything it does not recognise, so a naive port would read
// `-` as a *filename*, find no match, and report a confident "no file(s) match"
// — the wrong answer delivered with the right formatting. This project has a
// name for that failure mode (a gate reporting success, or a specific-sounding
// failure, having verified nothing), and refusing loudly is the fix.
package main

import (
	"fmt"
	"os"
)

// usage is compared byte-for-byte against the reference's USAGE
// (bin/validate-intent) by tests/parity/run_parity.sh, in section 6 ("--help")
// and wherever a refusal prints the usage block.
//
// KNOWN-MISLEADING LAST LINE, and why it is still here.
//
// "Validates JSON against schemas/open-test-intent.v1.json" is true of the
// Python script — it always lives at <repo>/bin/ — and is no longer true of
// this binary. Since SPGD-131 an installed copy at /usr/local/bin/ validates
// against its embedded schema; the path this line names does not exist there
// and governs nothing.
//
// Because the two texts are compared byte-for-byte, they can only move
// together: editing this constant alone fails section 6 ("--help") and every
// refusal that prints the usage block, which is deleting real coverage to fix
// a sentence.
//
// The matched edit belongs in bin/validate-intent, and SPGD-131 put that file
// out of scope. That — a scope boundary, nothing more — is the whole reason
// the line still reads this way. Nothing in the tooling prevents the change.
//
// A follow-up slice permitted to touch the reference should edit both texts in
// one commit. The accurate statement of where the schema actually comes from
// lives on LoadSchema in fileio.go.
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

	// --json is stripped before the positional dispatch below, so it may be
	// written anywhere on the command line (bin/validate-intent:891-893).
	asJSON := false
	positional := make([]string, 0, len(argv))
	for _, arg := range argv {
		if arg == "--json" {
			asJSON = true
			continue
		}
		positional = append(positional, arg)
	}

	// Out-of-scope surfaces, refused before anything else can misread them.
	// These sit ahead of the schema load on purpose: "this mode is not in the Go
	// port" is the actionable diagnostic for them, and it stays the same answer
	// whether or not the schema happens to be loadable. They are deliberate
	// Go-only divergences, so they owe the reference nothing on ordering.
	//
	// Contrast the self-test `--json` refusal below, which claims to *reproduce*
	// the reference and therefore has to sit exactly where the reference put it.
	if len(positional) > 0 && positional[0] == "-" {
		return notImplemented("stdin mode (the `-` argument)")
	}
	isSource := len(positional) > 0 && (positional[0] == "-s" || positional[0] == "--source")
	if asJSON && len(positional) > 0 && !isSource {
		// Only the --source JSON path is in scope for slice 2. Adopter-mode
		// --json must NOT quietly fall through to the text renderer: that would
		// answer a --json request with prose the consumer then fails to parse.
		return notImplemented("--json output for adopter (FILE...) mode")
	}
	for _, arg := range positional {
		if hasRecursiveComponent(arg) {
			return notImplemented("recursive `**` glob patterns, as in " + PyReprString(arg))
		}
	}

	schema, schemaPath, err := LoadSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not load schema %s: %s\n", schemaPath, err)
		return 2
	}

	if len(positional) == 0 {
		if asJSON {
			// Self-test is the in-repo fixture harness, not an adopter surface.
			// Falling back to its prose report would answer a --json request
			// with text a consumer then fails to parse — refuse instead,
			// exactly as the reference does (bin/validate-intent:900-909).
			// This is a *reproduced* exit 2, not a port limitation.
			//
			// It therefore has to sit HERE, below the schema load, and not up
			// with the out-of-scope refusals above: the reference loads the
			// schema first (bin/validate-intent:895), so on a tree with no
			// readable schema it answers `could not load schema ...` and never
			// reaches this branch at all. Refusing earlier would still exit 2,
			// but with different stderr — which is exactly the claim of
			// reproduction this comment makes, broken in the one combination
			// nobody diffs by hand. tests/parity/run_parity.sh section 7
			// ("OS-level failures") covers it: a tree whose schema is present
			// but MALFORMED, invoked with a bare `--json`.
			//
			// It used to be a tree with no schemas/ directory at all, and the
			// difference matters to anyone editing that case. Since the
			// embedded fallback landed (SPGD-131, see LoadSchema in fileio.go)
			// a schema-LESS tree no longer fails to load here — the port finds
			// its compiled-in copy — so the crossing would pass whichever side
			// of the load this refusal sat on. The probe has to be a schema
			// that EXISTS and cannot be loaded. Moving it back to a
			// schema-less tree would leave a green case that had quietly
			// stopped testing the thing it is named for.
			//
			// The section NAME is quoted alongside the number
			// because that file renumbers as slices are added: if the two
			// ever disagree the name is the one to trust, and
			// tests/parity/check_section_refs.py fails the harness loudly
			// when they do.
			fmt.Fprintln(os.Stderr, "error: --json is not supported in self-test mode "+
				"(it needs -, FILE... or --source FILE...)")
			os.Stderr.WriteString(usage)
			return 2
		}
		return RunSelfTest(schema)
	}

	if isSource {
		if len(positional) == 1 {
			fmt.Fprintf(os.Stderr,
				"error: %s requires at least one FILE/glob argument\n", positional[0])
			os.Stderr.WriteString(usage)
			return 2
		}
		if asJSON {
			return RunSourceJSON(positional[1:], schema)
		}
		return RunSource(positional[1:], schema)
	}

	return RunAdopter(positional, schema)
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
	return runOverPatterns(patterns, checkOne, nil)
}

// runOverPatterns is the port of `_run_over_patterns`
// (bin/validate-intent:467-501): expand each pattern and run checkOne over
// every file it matches.
//
// checkOne returns true when that file failed. The aggregate exit code is 1 if
// any file failed *or* any pattern matched nothing, else 0 — a pattern matching
// nothing is never a silent pass.
//
// onNoMatch replaces the default stderr diagnostic when non-nil: --json routes
// the no-match into the document as a finding on stdout, so a stdout-only
// consumer is not left with a clean pass list and an unexplained non-zero exit.
// Either way the no-match still drives the exit code.
func runOverPatterns(patterns []string, checkOne func(string) bool, onNoMatch func(string)) int {
	exitCode := 0
	for _, pattern := range patterns {
		files := ExpandFiles(pattern)
		if len(files) == 0 {
			// Never a silent pass: a pattern that matches nothing (or only
			// directories) is an error the caller must see.
			if onNoMatch == nil {
				fmt.Fprintf(os.Stderr, "error: no file(s) match %s\n", PyReprString(pattern))
			} else {
				onNoMatch(pattern)
			}
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

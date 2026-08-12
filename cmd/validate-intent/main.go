// Command validate-intent is the canonical OpenTestIntent validator.
//
// It decides one question — is this a valid OpenTestIntent annotation? — and
// PROTOCOL.md plus schemas/open-test-intent.v1.json are what decide it. This
// binary implements that specification; where the two disagree, the
// specification is right.
//
// The modes:
//
//	FILE...            adopter mode: validate FILE(s)/glob(s) as intent JSON
//	--source / -s      in-source mode: validate @intent annotations inside
//	                   test source files, reported per finding as file:line
//	<no arguments>     self-test over the shipped fixture corpus
//	-                  stdin mode: one annotation JSON on stdin
//	-h / --help        usage
//	--version          the binary's identity and the schema it carries
//	--schema-source    the schema a run on THIS host would actually enforce
//	--json             a machine-readable document instead of the human
//	                   report, for the stdin, FILE... and --source modes
//
// Every glob-expanding mode accepts a recursive `**` component. See glob.go.
//
// Exit codes: 0 clean, 1 a verdict of "invalid" (or a pattern that matched
// nothing), 2 the run produced no verdict at all — a usage error, or a schema
// that exists and could not be loaded.
//
// HOW IT IS GRADED. The fixture corpus under examples/ is the instrument: every
// examples/*.json must validate, every examples/invalid/*.json must be
// rejected, and the same both ways for the in-source fixtures. That is what the
// self-test runs, so the binary carries its own acceptance test — see
// selftest.go, and note the empty-set guard there, which makes a corpus that
// matched nothing a failure rather than a green run over no fixtures.
package main

import (
	"fmt"
	"os"
)

// usage is printed by --help and by every refusal that cannot name a better
// next step.
//
// THE LAST LINE names a contract, not a path, and that is deliberate. It used
// to read "Validates JSON against schemas/open-test-intent.v1.json", which is
// false everywhere this binary actually ships: an installed copy falls back to
// its compiled-in schema when no file sits at <exe>/../schemas/, and
// tests/cross/run_cross_build.sh ASSERTS that directory is absent beside an
// installed binary. So the release artifact's only on-host documentation named
// the one path the repository guarantees is not there.
//
// Where the bytes come from has one accurate answer and a usage line is not the
// place for it: see LoadSchema in fileio.go, and `--schema-source`, which
// reports it for a given host.
const usage = `usage: validate-intent                    # self-test the in-repo fixtures
       validate-intent -                  # validate one annotation JSON read from stdin
       validate-intent FILE...            # validate FILE(s)/glob(s) as valid intent JSON
       validate-intent --source FILE...   # validate @intent annotations inside test
                                          #   source files (.rb/.py/.js/...), reported
                                          #   per finding as file:line. Alias: -s

       --json   emit one machine-readable JSON document on stdout instead of the
                human report — for the stdin, FILE... and --source modes only.
                Position-independent. Exit codes are identical either way.

Validates JSON against the OpenTestIntent v1 schema (zero dependencies).
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	// --help is checked first among the arguments, so it wins over everything
	// else on the command line.
	//
	// usage is the shared block; helpTrailer (version.go) documents the two
	// flags that report on the binary itself. On a host holding nothing but
	// this binary, --help is the entire documentation set, so a flag the
	// installer runs cannot be invisible on it.
	for _, arg := range argv {
		if arg == "-h" || arg == "--help" {
			os.Stdout.WriteString(usage + helpTrailer)
			return 0
		}
	}

	// --version, checked second. It answers from ANY argv position for the same
	// reason --json does — the loop runs over the whole command line before
	// anything reads a positional.
	//
	// --help deliberately still wins when both are passed.
	for _, arg := range argv {
		if arg == "--version" {
			fmt.Println(VersionLine())
			return 0
		}
	}

	// --schema-source, checked third. It reports the schema a run on this host
	// actually ENFORCES — resolved origin plus the digest of the bytes loaded —
	// which is the question `--version` structurally cannot answer, since it
	// returns before LoadSchema is ever called.
	//
	// --version wins over this flag because it is the surface scripts/install.sh,
	// scripts/build-release.sh and specguard-rspec's identity probe already
	// parse: a crossing that changed what --version prints would break them for
	// a report none of them asked for.
	//
	// Unlike the two above, this one is not an early return that ignores the
	// filesystem — it loads the schema, and exits 2 with the verdict path's own
	// diagnostic if that fails.
	for _, arg := range argv {
		if arg == schemaSourceFlag {
			return runSchemaSource()
		}
	}

	// --json is stripped before the positional dispatch below, so it may be
	// written anywhere on the command line.
	asJSON := false
	positional := make([]string, 0, len(argv))
	for _, arg := range argv {
		if arg == "--json" {
			asJSON = true
			continue
		}
		positional = append(positional, arg)
	}

	isStdin := len(positional) > 0 && positional[0] == "-"
	isSource := len(positional) > 0 && (positional[0] == "-s" || positional[0] == "--source")

	schema, schemaSource, err := LoadSchema()
	if err != nil {
		os.Stderr.WriteString(schemaLoadError(schemaSource, err))
		return 2
	}

	if len(positional) == 0 {
		if asJSON {
			// Self-test is the in-repo fixture harness, not an adopter surface.
			// Falling back to its prose report would answer a --json request
			// with text a consumer then fails to parse — refuse instead.
			fmt.Fprintln(os.Stderr, "error: --json is not supported in self-test mode "+
				"(it needs -, FILE... or --source FILE...)")
			os.Stderr.WriteString(usage)
			return 2
		}
		return RunSelfTest(schema)
	}

	// Per-mode --json routing. Each mode picks its own renderer: they share the
	// checks and the JSONReport, not a single "if asJSON" at the bottom, because
	// what a finding MEANS differs by mode — stdin has exactly one and counts no
	// files, adopter counts one site per file, --source counts one per
	// annotation. Flattening that into one renderer is what makes the three
	// summaries silently agree when they should not.
	if isStdin {
		if asJSON {
			return RunStdinJSON(schema)
		}
		return RunStdin(schema)
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

	if asJSON {
		return RunAdopterJSON(positional, schema)
	}
	return RunAdopter(positional, schema)
}

// schemaLoadError renders the diagnostic for a schema that EXISTS and could not
// be loaded, trailing newline included:
//
//	error: could not load schema /repo/schemas/open-test-intent.v1.json: open /repo/schemas/open-test-intent.v1.json: permission denied
//
// It is a function rather than a format string written at each site because it
// has two sites — the verdict path in run(), and `--schema-source`
// (schemasource.go) — and those two must fail identically. schemasource_test.go
// asserts their stderr is equal on the same broken tree rather than trusting
// that they share this function.
//
// The origin comes from the SchemaSource the load resolved, which is the path it
// tried to read — or EmbeddedSchemaLabel, though not reachably from here: a
// fallback to the embedded copy only happens when the file is ABSENT, and the
// embedded bytes are pinned by schema_test.go, so an error carrying that label
// would mean the compiled-in schema itself no longer loads.
func schemaLoadError(source SchemaSource, err error) string {
	return fmt.Sprintf("error: could not load schema %s: %s\n", source.Origin, err)
}

// RunAdopter validates the given path(s)/glob(s) as valid intent JSON.
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

// runOverPatterns expands each pattern and run checkOne over
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
				fmt.Fprintf(os.Stderr, "error: no file(s) match %s\n", Quote(pattern))
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

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
// Every glob-expanding mode above accepts Python's recursive `**` (slice 4,
// SPGD-123). It expands to the same set of files the reference's
// `glob.glob(pattern, recursive=True)` does, rather than being downgraded to a
// single `*` — see cmd/validate-intent/pyglob.go.
//
// One surface is NOT in that table, because it is not byte-identical and never
// will be: `--version` (slice 6, SPGD-141) is a Go-only flag. The reference has
// no such flag — it reads `--version` as a filename, reports "no file(s) match"
// and exits 1 — and a released binary that cannot state what it is cannot be
// released. The divergence is declared in tests/parity/run_parity.sh (excluded
// group 5 in its header) and asserted Go-side in section 16 ("Go-side refusals
// — the excluded surfaces, still asserted"). See version.go.
//
// `--schema-source` (slice 19, SPGD-301) is the second such flag, and it exists
// because the first one answers a narrower question than it looks like it does.
// `--version` reports the digest of the schema COMPILED IN; LoadSchema gives a
// schema file found beside the executable priority over that copy, so a run can
// enforce a contract `--version` has never seen. This flag runs the real loader
// and reports the resolved origin plus the digest of the bytes actually loaded —
// the contract a run on this host ENFORCES. Declared as excluded group 6 and
// asserted in the same section 16. See schemasource.go.
//
// Both are also DOCUMENTED, in `--help` and only there, by a trailer appended to
// the shared usage block (helpTrailer in version.go). On a host holding just
// this binary, `--help` is the entire documentation set, and a flag the
// installer runs cannot be invisible on it. Section 7 ("--help") still compares
// the shared block byte-for-byte and checks the trailer against an exact
// expectation; neither half is waved through. See version.go (SPGD-279).
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
// (bin/validate-intent) by tests/parity/run_parity.sh, in section 7 ("--help")
// and wherever a refusal prints the usage block.
//
// It therefore holds only text that is true of BOTH implementations. The one
// Go-only line this binary prints — the `--version` row — is not here: it lives
// in helpTrailer (version.go) and is appended on the `--help` path alone. See
// that file for why a row in this constant would have broken three compared
// refusals as well as section 7, and why no edit to the reference could have
// restored them.
//
// THE LAST LINE, and why it names a contract instead of a path.
//
// It used to read "Validates JSON against schemas/open-test-intent.v1.json".
// That was true of the Python script, which always lives at <repo>/bin/, and
// false of this binary everywhere it actually ships. Since SPGD-131 an
// installed copy falls back to its compiled-in schema when no file sits at
// <exe>/../schemas/ — and tests/cross/run_cross_build.sh ASSERTS that directory
// is absent beside an installed binary, aborting the run if it exists. So the
// release artifact's only on-host documentation named the one path the repo
// guarantees is not there.
//
// Because the two texts are compared byte-for-byte they can only move together,
// which is why they were corrected in a single commit (SPGD-279) rather than
// by relaxing the comparison — relaxing it would have deleted real coverage to
// fix a sentence.
//
// Naming the CONTRACT is true of both, because the disagreement was never about
// which schema is enforced; it is about where the bytes come from. That second
// question has one accurate answer and a usage line is not the place for it:
// see LoadSchema in fileio.go.
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
	// Checked first, exactly as in the reference (bin/validate-intent:886), so
	// `--help` wins over everything else on the command line.
	for _, arg := range argv {
		if arg == "-h" || arg == "--help" {
			// usage is the block both implementations share; helpTrailer is the
			// Go-only addendum documenting --version (version.go). It is
			// appended HERE and nowhere else — the refusal paths below write
			// bare `usage`, because those are byte-compared against the
			// reference too, and a Go-only line inside the shared constant
			// would break them rather than only this surface.
			os.Stdout.WriteString(usage + helpTrailer)
			return 0
		}
	}

	// --version, checked second: a Go-only surface (see version.go), placed
	// here so it answers from ANY argv position for the same reason --json does
	// — the loop runs over the whole command line before anything reads a
	// positional. It exits 0 and writes to stdout, which is what separates it
	// from every other Go-only surface in this file: those are refusals.
	//
	// --help deliberately still wins when both are passed, consistent with the
	// "wins over everything" comment above and with the reference, which prints
	// usage for `--help --version` exactly as this does. That crossing is a
	// real byte-for-byte comparison in tests/parity/run_parity.sh section 7
	// ("--help"), not a Go-side assertion.
	for _, arg := range argv {
		if arg == "--version" {
			fmt.Println(VersionLine())
			return 0
		}
	}

	// --schema-source, checked third: the other Go-only surface that SUCCEEDS
	// (schemasource.go). It reports the schema a run on this host actually
	// ENFORCES — resolved origin plus the digest of the bytes loaded from it —
	// which is the question `--version` above structurally cannot answer, since
	// it returns before LoadSchema is ever called.
	//
	// Answered from any argv position, for the same reason --version and --json
	// are: the loop runs over the whole command line before anything reads a
	// positional.
	//
	// The ORDER of these three loops is the precedence, and each step of it is
	// deliberate. --help wins over everything, in both implementations
	// (bin/validate-intent:886), and the crossing with this flag is a real
	// byte-for-byte comparison in tests/parity/run_parity.sh section 7 rather
	// than a Go-side claim — the reference pre-empts `--schema-source` as a
	// filename with its own help loop, so both print usage. --version wins over
	// this flag because it is the surface scripts/install.sh, scripts/build-release.sh
	// and specguard-rspec's identity probe already parse: a crossing that changed
	// what --version prints would break them for a report none of them asked for.
	//
	// Unlike the two above, this one is NOT an early return that ignores the
	// filesystem — it loads the schema, and exits 2 with the verdict path's own
	// diagnostic if that fails. It therefore sits above the out-of-scope refusals
	// below but does its own loading rather than sharing theirs.
	for _, arg := range argv {
		if arg == schemaSourceFlag {
			return runSchemaSource()
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

	schema, schemaSource, err := LoadSchema()
	if err != nil {
		os.Stderr.WriteString(schemaLoadError(schemaSource, err))
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
			// nobody diffs by hand. tests/parity/run_parity.sh section 8
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
			// ever disagree the name is the one to trust. Nothing checks
			// the pair, so grep for the name rather than counting to the
			// number.
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

// schemaLoadError renders the reference's diagnostic for a schema that exists
// and could not be loaded (bin/validate-intent:898), trailing newline included:
//
//	error: could not load schema /repo/schemas/open-test-intent.v1.json: [Errno 13] Permission denied: '...'
//
// It is a function rather than a format string written at each site because it
// now HAS two sites — the verdict path in run(), and `--schema-source`
// (schemasource.go) — and those two must fail identically. The port's whole
// acceptance test is byte-for-byte agreement with python3 on this line, so a
// second copy that drifted would produce a surface that reports this host's
// broken schema in prose no comparison covers. tests/parity/run_parity.sh
// asserts the two paths' stderr are equal on the same broken tree rather than
// trusting that they share this function.
//
// The origin comes from the SchemaSource the load resolved, which is the path it
// tried to read — or EmbeddedSchemaLabel, though not reachably from here: a
// fallback to the embedded copy only happens when the file is ABSENT, and the
// embedded bytes are pinned by schema_test.go, so an error carrying that label
// would mean the compiled-in schema itself no longer loads.
func schemaLoadError(source SchemaSource, err error) string {
	return fmt.Sprintf("error: could not load schema %s: %s\n", source.Origin, err)
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

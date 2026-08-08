// Command validate-intent is the Go port of bin/validate-intent.
//
// Implemented and proven byte-identical to `python3 bin/validate-intent` by
// tests/parity/run_parity.sh, which is the acceptance test for this port:
//
//	FILE...            adopter mode        (slice 1, SPGD-98)
//	-h / --help        usage               (slice 1)
//	--source / -s      in-source mode      (slice 2, SPGD-102)
//	<no arguments>     self-test           (slice 2)
//	-                  stdin mode          (slice 3, SPGD-107)
//	--json             machine-readable, for all three input modes
//	                     (--source: slice 2; stdin and FILE...: slice 3)
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
// group 4 in its header) and asserted Go-side in section 16b ("--version — the
// excluded surface that SUCCEEDS"). See version.go.
//
// `--schema-source` (slice 19, SPGD-301) is the second such flag, and it exists
// because the first one answers a narrower question than it looks like it does.
// `--version` reports the digest of the schema COMPILED IN; LoadSchema gives a
// schema file found beside the executable priority over that copy, so a run can
// enforce a contract `--version` has never seen. This flag runs the real loader
// and reports the resolved origin plus the digest of the bytes actually loaded —
// the contract a run on this host ENFORCES. Declared as excluded group 5 and
// asserted in section 16c. See schemasource.go.
//
// Both are also DOCUMENTED, in `--help` and only there, by a trailer appended to
// the shared usage block (helpTrailer in version.go). On a host holding just
// this binary, `--help` is the entire documentation set, and a flag the
// installer runs cannot be invisible on it. Section 7 ("--help") still compares
// the shared block byte-for-byte and checks the trailer against an exact
// expectation; neither half is waved through. See version.go (SPGD-279).
//
// The mode matrix is otherwise complete: every surface the REFERENCE exposes is
// now implemented rather than refused. What remains outside the port is
// cross-compilation/release packaging and the Ruby wrapper, neither of which is
// a behaviour of this binary.
//
// The refusals that guarded the unported modes are gone with them. What remains
// was never about missing work:
//
//   - a schema whose `pattern` RE2 cannot reproduce exactly (see pypattern.go);
//   - a PYTHONIOENCODING this port cannot reproduce — either half of it: an
//     ENCODING that is not UTF-8, or an error HANDLER that is neither `strict`
//     nor `surrogateescape` (see pyioerrors.go, stdin_mode.go and pystdout.go);
//   - an environment whose LOCALE gives CPython a default codec that is not
//     UTF-8, which governs the std streams AND the filesystem/argv channel at
//     once (see pylocale.go and pyfspath.go).
//
// (Recursive `**` used to be a fourth. Slice 4 implemented it rather than
// refusing it, which is why no `hasRecursiveComponent` gate survives here.)
//
// All three refuse with exit 2 rather than answering. That distinction —
// refusing beats a confident wrong answer — is the reason the stubs were written
// that way in the first place: Python's main() falls through to adopter mode for
// anything it does not recognise, so a naive port would have read `-` as a
// *filename*, found no match, and reported "no file(s) match '-'" with perfect
// formatting.
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

// main decodes argv before run() sees it, so nothing below this line ever
// handles an undecoded argument.
//
// CPython decodes sys.argv with the filesystem encoding under surrogateescape,
// so a byte that is not valid UTF-8 in a PATH the caller typed becomes
// U+DC00+byte. Go performs no decoding at all: os.Args carries the raw bytes in
// a string, and every renderer in this port iterates strings as CODE POINTS —
// which turned such a byte into U+FFFD, a lossy answer on the --json contract's
// own `file` key. See pyfspath.go, which owns both directions of this channel.
func main() {
	os.Exit(run(pyFSDecodeArgs(os.Args[1:])))
}

func run(argv []string) int {
	// The LOCALE gate, and it sits above the PYTHONIOENCODING one because it is
	// upstream of it: CPython derives sys.std*.encoding (whenever
	// PYTHONIOENCODING names no codec) AND sys.getfilesystemencoding() from one
	// default codec, and the locale is what picks that codec.
	//
	// Measured, and it is the round-5 defect one variable over. With NOTHING in
	// PYTHONIOENCODING set, `PYTHONUTF8=0 LC_ALL=C` makes all three streams and
	// the filesystem `ascii`, and `python3 bin/validate-intent --help` DIES —
	// UnicodeEncodeError on the usage block's em dash, 0 bytes, rc 1 — where
	// the port printed 824 clean bytes and exited 0. The PYTHONIOENCODING gate
	// cannot see that environment at all. See pylocale.go.
	if surface, unsupported := pyLocaleUnsupported(); unsupported {
		return notImplemented(surface)
	}

	// The ENCODING gate sits ABOVE --help, where the HANDLER gate below sits
	// under it, and the asymmetry is measured rather than stylistic.
	//
	// A handler only ever alters characters the codec cannot represent. UTF-8
	// represents every character in `usage`, so no handler can change a byte of
	// --help — which is why it is answered under `:replace` and compared
	// byte-for-byte there.
	//
	// A codec changes the bytes of EVERY character, and can fail outright on
	// characters UTF-8 handles fine. `usage` is not ASCII: it carries an em dash
	// (U+2014) at offset 619, copied from the reference's USAGE, which those two
	// texts are compared byte-for-byte to keep. Measured on
	// `validate-intent --help`:
	//
	//	PYTHONIOENCODING=latin-1   reference DIES, UnicodeEncodeError, rc 1, 0 B
	//	PYTHONIOENCODING=utf-16    reference emits 1644 B; UTF-8 is 824 B
	//
	// So "--help is a compile-time ASCII constant no environment can alter" was
	// half true, and the ASCII half of it was simply wrong. The port refuses
	// --help under a codec it does not implement, and still answers it under a
	// handler it does not implement. tests/parity/run_parity.sh section 16
	// ("Go-side refusals — the excluded surfaces, still asserted") pins both
	// halves, and would have caught this ordering written the other way round.
	if !pyIOEncodingSupported() {
		return notImplemented("the PYTHONIOENCODING encoding " +
			PyReprString(pyIOEncoding()))
	}

	// Checked first among the ARGUMENTS, exactly as in the reference
	// (bin/validate-intent:886), so `--help` wins over everything else on the
	// command line.
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

	// --version STAYS BELOW BOTH ENVIRONMENT GATES, and that is a decision
	// rather than where it happened to land when the two slices merged.
	//
	// The case for hoisting it is real and worth stating: `--version` is the one
	// surface in this file with NO reference behaviour (excluded group 4), so no
	// parity gate can be about it, and version.go names the CI preflight
	// `validate-intent --version || echo "not installed"` — which under a
	// refused environment reports "not installed" for a binary that is
	// installed.
	//
	// It stays below anyway, because the gates above do not refuse a MODE, they
	// refuse the PROCESS: they say this binary cannot faithfully decode its own
	// argv or encode its own stdout here. Answering one flag from inside an
	// environment the previous line just declared unreproducible would be the
	// port asserting something about itself that it has otherwise refused to
	// assert. A diagnostic naming the variable is more actionable to that same
	// CI job than a version string is — it says what to change.
	//
	// The consequence is asserted rather than left implicit, in
	// tests/parity/run_parity.sh section 17c ("the locale's default codec"):
	// `--version` refuses there alongside every other surface. Hoisting it also
	// breaks `--help --version`, which IS compared, so the mutation shows up in
	// five cases rather than one — measured, not assumed.
	//
	// Placed here rather than in a positional check so it answers from ANY argv
	// position for the same reason --json does
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

	isStdin := len(positional) > 0 && positional[0] == "-"
	isSource := len(positional) > 0 && (positional[0] == "-s" || positional[0] == "--source")

	// The one Go-only refusal on this path, ahead of the schema load on purpose:
	// "the port cannot reproduce this" is the actionable diagnostic, and it is
	// the same answer whether or not the schema happens to be loadable.
	//
	// NOT scoped to stdin mode, and that scoping was a real defect. The
	// PYTHONIOENCODING governs sys.stdout as well as sys.stdin, and the output
	// side is reachable from every mode — a FILE whose key carries a "\udc82"
	// escape puts a lone surrogate into the report with no stdin involved.
	// Gating this on `-` left adopter, --source and self-test diverging under an
	// environment the port already models. See pyioerrors.go.
	//
	// BOTH FIELDS OF THE VARIABLE, and that is the round-5 fix. The gate used to
	// be spelled pyIOHandlerSupported() and read only `ENCODING:HANDLER`'s
	// second half, so `latin-1` — whose handler is `strict`, which this port
	// does reproduce — passed while CPython encoded both streams as iso8859-1.
	// It changed the verdict in this file's own stdin mode, with the same exit
	// code: iso8859-1 decodes every byte, so the reference reported `schema`
	// where the port reported `read`.
	//
	// pyIOUnsupported answers for both fields, so this stays the single gate
	// even though the encoding half has already fired above --help. Calling
	// pyIOHandlerSupported() here instead would work today and would silently
	// stop covering the encoding if that earlier check were ever moved.
	//
	// Slice 4 removed the sibling that used to sit here — recursive `**` is
	// implemented now, not refused, so no glob gate remains.
	if surface, unsupported := pyIOUnsupported(); unsupported {
		// The port reproduces UTF-8 and CPython's `strict` and `surrogateescape`
		// in both directions. Every other codec and handler SUCCEEDS in both
		// directions too, and each produces different bytes, so substituting an
		// implemented one would answer confidently about a string the reference
		// never saw.
		return notImplemented(surface)
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
			// exactly as the reference does (bin/validate-intent:902-909).
			//
			// DECISION — this sits BELOW LoadSchema, unlike the Go-only
			// refusal above it, and the difference is deliberate. That one is
			// a limitation: the port cannot answer at all, so it must say so
			// whatever else is wrong. This one is a *reproduced* reference
			// behaviour, so it has to fire at the point the reference fires
			// it — and the reference loads the schema first (:895), before it
			// ever looks at as_json. Ordering it earlier is observable: with
			// an unloadable schema, `validate-intent --json` reports the
			// schema error where the port reported this refusal. Same exit
			// code, so only a stderr comparison catches it.
			//
			// tests/parity/run_parity.sh section 8 ("OS-level failures")
			// covers it: a tree whose schema is present but MALFORMED,
			// invoked with a bare `--json`, alongside a case asserting the
			// refusal still fires when the schema DOES load — so the pair
			// cannot go green by having lost the refusal.
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
//
// THE `%s` ON THE ORIGIN IS THE ONE UNMODELED STDERR WRITE IN THIS PORT, and it
// is a declared divergence rather than an oversight. CPython gives sys.stderr
// the `backslashreplace` handler by default — independently of
// PYTHONIOENCODING, and differently from sys.stdout, which is the stream
// pyioerrors.go/pystdout.go actually model. On an install directory whose name
// is not valid UTF-8, the origin carries a surrogate, and `%s` writes it as
// three WTF-8 bytes where the reference writes the six ASCII characters
// `\udce9`. Declared as excluded group 7 in tests/parity/run_parity.sh and
// pinned in its section 16e, which also pins the `[Errno ...]` half of this
// same line staying byte-identical (it goes through pyOSError -> PyReprString)
// and every other mode on such a tree staying byte-identical too.
//
// Every other stderr write in this port is ASCII by the time it is written: a
// compile-time constant UTF-8 encodes identically on both sides (`usage`), or a
// string through PyReprString. Anyone adding a non-ASCII `%s` to stderr is
// EXTENDING group 7, not using a channel someone modeled.
func schemaLoadError(source SchemaSource, err error) string {
	return fmt.Sprintf("error: could not load schema %s: %s\n", source.Origin, err)
}

// RunAdopter is the port of `run_adopter` (bin/validate-intent:713-728):
// validate the given path(s)/glob(s) as valid intent JSON.
func RunAdopter(patterns []string, schema *Schema) int {
	checkOne := func(path string) bool {
		valid, errs, parseError, _ := CheckFile(path, schema)
		if parseError != "" {
			pyPrintf("FAIL  %s — %s\n", path, parseError)
			return true
		}
		if valid {
			pyPrintf("PASS  %s\n", path)
			return false
		}
		pyPrintf("FAIL  %s\n", path)
		for _, err := range errs {
			pyPrintf("        -> %s\n", err)
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

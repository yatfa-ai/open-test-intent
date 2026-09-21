// Command validate-intent is the canonical OpenTestIntent validator.
//
// It decides one question — is this a valid OpenTestIntent annotation? — and
// PROTOCOL.md plus schemas/open-test-intent.v1.json are what decide it. This
// binary implements that specification; where the two disagree, the
// specification is right.
//
// The modes:
//
//	FILE...            adopter mode: validate FILE(s)/glob(s)/directory(ies) as
//	                   intent JSON
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
// Every glob-expanding mode accepts a recursive `**` component, and a bare
// argument naming an existing directory is descended as `DIR/**`. See glob.go.
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
       validate-intent FILE...            # validate FILE(s)/glob(s)/directory(ies) as valid
                                          #   intent JSON. A directory is descended as DIR/**
       validate-intent --source FILE...   # validate @intent annotations inside test
                                          #   source files (.rb/.py/.js/...), reported
                                          #   per finding as file:line. A directory is
                                          #   descended as DIR/**. Alias: -s

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

// noMatchDetail is the clause that tells the no-match situations APART, written
// once and worn by both renderers.
//
// It returns "" for every situation that keeps the generic bytes — a
// nonexistent path, the empty pattern, a magic glob that matched only
// directories — and a discriminating clause for the two the expansion can name:
// an argument it read as a DIRECTORY TO DESCEND whose descent found no file,
// and the same argument where the descent's dependency/build fence refused part
// of the tree. That is the whole disambiguation, and the reason a user can now
// tell "my argument is a typo" from "the tree I named holds nothing" from "the
// part of the tree I named that this tool reads holds nothing".
//
// `fenced` is the descent's own report that the fence fired (see descentFence
// in glob.go), not a re-derivation here. An arm that re-probed the tree from
// this side would be a second opinion about a walk that has already happened,
// and the two could disagree.
//
// The FENCED arm exists because its sentence was not merely unhelpful but
// UNTRUE: an all-fenced directory printed the empty directory's words verbatim,
// so the tool blamed the tree for a silence its own fence had produced. The
// register is this product's own — `specguard-rspec`'s `all_fenced_reason`
// says "…, all in dependency or build directories" for exactly this situation,
// and the shared binary saying what its own client says is the point.
//
// It says "no file to read OUTSIDE" rather than the twin's "files found, all
// in", and that is the one place the twin's sentence may not be copied. The
// twin counts what its selector rejected, so it can assert that files existed;
// this walk learns only that it PRUNED A DIRECTORY, and a fenced directory can
// be empty — a cleaned `dist/`, a bare `node_modules/`. Asserting files it
// never looked for would be a second confidently-wrong sentence in the place
// the first one was removed from. What both spellings do carry is the fact that
// matters: the silence is the fence's and not the tree's.
//
// `quote` is the CALLER'S renderer, not a formatting knob. The text path quotes
// a path for a human and the --json path carries it bare — inside a JSON string
// a second layer of quoting is noise a consumer has to strip — so the two
// renderers legitimately spell the same path differently. Passing the quoting
// in keeps that the ONLY thing they differ by: one sentence, one condition, two
// spellings. A second copy of the sentence in report.go is exactly the drift
// the shared JSONReport type was written to prevent, and it would be invisible
// to any test that checks one renderer at a time.
//
// It names the DESCENT rather than only the directory because that is the fact
// the user cannot otherwise see: the tool did not refuse the argument, it
// expanded it and walked it. A reader who is told the walk happened knows to
// look at the tree rather than at their spelling — EXCEPT under the fence,
// where the tree is fine and that instruction would be the wrong one, which is
// why the fenced arm names the fence instead.
func noMatchDetail(pattern string, fenced bool, quote func(string) string) string {
	if !readAsDirectoryArgument(pattern) {
		return ""
	}
	detail := ": it is a directory, and the descent " +
		quote(expandDirectoryArgument(pattern)) + " found no file to read"
	if fenced {
		detail += " outside dependency or build directories, which it does not enter"
	}
	return detail
}

// noMatchDiagnostic is the TEXT renderer's no-match line, including its
// trailing newline.
//
// The generic bytes are unchanged, deliberately: every other no-match situation
// still produces `error: no file(s) match 'X'` exactly as it did, so the pins
// that assert those bytes stay green without being edited, and a user's grep
// still finds them.
func noMatchDiagnostic(pattern string, fenced bool) string {
	return "error: no file(s) match " + Quote(pattern) +
		noMatchDetail(pattern, fenced, Quote) + "\n"
}

// fencedSelectionNote is the PARTIAL arm's disclosure: the run selected files
// AND the dependency/build fence refused part of the tree, so the read set is
// narrower than the argument names.
//
// It is gated on the fence having FIRED, which is this repo's spelling of the
// count-gating the Ruby twin does (`selection_line`: the clause appears only
// when `skipped.positive?`). A run whose fence removed nothing therefore prints
// byte-identically to a run made before the fence existed — the narrowing must
// never be silent, and the absence of a narrowing must never be noise.
//
// It carries no figure, and it names DIRECTORIES rather than files. The twin
// prints a file count because its selector counts what it rejected; this walk
// declines a directory at the prune and never enters it, so it knows only that
// it pruned. Sizing the refusal would mean descending the tree the fence exists
// to stay out of, and naming files it never looked for would be a confidently
// wrong sentence inside a disclosure written to remove one.
//
// STDERR, on BOTH renderers, and written at one site so they cannot drift. The
// report is the contract on stdout — the --json document above all, which a
// consumer parses whole — and provenance about the selection is not a finding.
// The twin makes the same split for the same reason, routing its selection line
// to stderr under `--json`.
func fencedSelectionNote() string {
	return "note: skipping dependency or build directories\n"
}

// RunAdopter validates the given path(s)/glob(s) as valid intent JSON.
func RunAdopter(patterns []string, schema *Schema) int {
	checkOne := func(path string) bool {
		valid, errs, parseError, _, _ := CheckFile(path, schema)
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
// Either way the no-match still drives the exit code. It takes the descent's
// fence report alongside the pattern so both renderers name the same situation
// — see noMatchDetail.
func runOverPatterns(patterns []string, checkOne func(string) bool, onNoMatch func(string, bool)) int {
	exitCode := 0
	for _, pattern := range patterns {
		files, fenced := expandFilesFenced(pattern)
		if len(files) == 0 {
			// Never a silent pass: a pattern that matches no FILE is an error
			// the caller must see. An argument naming a directory is descended
			// rather than refused (the expansion rewrites it to `DIR/**`), so
			// what reaches here is a pattern that found nothing to READ — a
			// nonexistent path, an EMPTY directory, a directory whose readable
			// part the dependency/build fence left empty, or a glob that
			// matched only directories.
			//
			// Three of those four are told apart here. `pattern` is the
			// ORIGINAL argument — the `DIR/**` rewrite happens inside the
			// expansion and feeds the matcher only — so readAsDirectoryArgument
			// can ask the expansion itself which reading it used, and an
			// argument that WAS descended gets a diagnostic saying so; `fenced`
			// is the descent's own report of whether its fence fired, which
			// splits the two descended situations. Without them, "that path is
			// not there", "the tree you named holds no files" and "the part of
			// the tree you named that this tool reads holds no files" arrive as
			// the same sentence with a different name in it — and the last of
			// those three is not merely unhelpful but FALSE, because this
			// tool's own fence is what produced the silence it blames the tree
			// for. The fourth situation — a magic glob that matched only
			// directories — keeps the generic bytes: it is not a directory
			// ARGUMENT, and describing it as one would name an interpretation
			// the tool did not use.
			if onNoMatch == nil {
				fmt.Fprint(os.Stderr, noMatchDiagnostic(pattern, fenced))
			} else {
				onNoMatch(pattern, fenced)
			}
			exitCode = 1
			continue
		}
		// The PARTIAL arm: files WERE selected and the fence refused part of the
		// tree, so the read set is narrower than the argument names. The run
		// succeeds, which is precisely why this has to be said — a successful
		// run that silently skipped a directory is indistinguishable from one
		// over a tree that never held it, and this product's own Ruby client
		// already decided that question the same way (`selection_line`: the
		// narrowing must never be silent). Gated on the fence having fired, so
		// a run that fenced nothing prints byte-identically to before the fence
		// existed.
		if fenced {
			fmt.Fprint(os.Stderr, fencedSelectionNote())
		}
		for _, path := range files {
			if checkOne(path) {
				exitCode = 1
			}
		}
	}
	return exitCode
}

package main

// A bare argument NAMING AN EXISTING DIRECTORY is descended as `DIR/**`.
//
// This file exists because of a failure that looked exactly like a different
// one. `--source spec` died with `error: no file(s) match 'spec'` — the SAME
// bytes a nonexistent path produces — so "you pointed me at a tree of test
// source and I declined to read any of it" was indistinguishable from "that
// path is not there". The mechanism was upstream of the diagnostic: globPath
// resolves a magic-free pattern through lexists and returned the directory
// itself, ExpandFiles' isFile filter then dropped it, and runOverPatterns saw
// an empty list. The fact the user needed existed exactly where it was thrown
// away.
//
// The fix is a REWRITE, not a second walk, so what is pinned here is
// EQUIVALENCE rather than a new selection rule. `spec` and `spec/**` must
// produce the same files, the same stdout and the same exit code; every rule
// glob_test.go already pins for `**` therefore applies unchanged, and the cases
// below that re-check a `**` rule through the sugar (hidden directories, the
// isFile drop) are asserting that the rewrite really does land on that path —
// not restating the rule.
//
// The arms that must KEEP today's bytes are pinned beside it, because the value
// of a rewrite is entirely in what it does not touch: a FILE argument is
// unchanged, and a pattern carrying magic is not rewritten at all.
//
// The two shapes that still find nothing — an empty directory and a
// nonexistent path — keep the never-silent-pass contract (both still exit 1),
// and SPGD-1301 then split their DIAGNOSTIC, which the rewrite had deliberately
// left collided: see the discrimination pins at the foot of this file. The
// expansion layer is unchanged by that split, so the ExpandFiles cases below
// still pin both arms as finding nothing.
//
// That split's guard is pinned the same way this rewrite is — by what it does
// NOT touch. It fires on "the expansion read this argument as a directory to
// descend", not on a second `isDir` probe, and the two inputs where those two
// readings disagree (a magic-named directory, the empty pattern) are asserted
// at the diagnostic layer beside the discrimination pins. Without them the
// swap is a mutation the whole suite survives.
//
// Both glob-expanding modes are driven, not one: the rewrite lives at the
// ExpandFiles chokepoint precisely so `--source` and adopter `FILE...` cannot
// diverge, and a test of one mode would not notice if they did.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dirArgTree writes a tree with something to find at two depths, a hidden
// directory that must stay unreachable through the descent, and an empty
// directory — and returns its root.
//
//	spec/order_spec.rb          <- at the top, so the ZERO-segment `**` match matters
//	spec/models/product_spec.rb <- one level down
//	spec/.cache/cached_spec.rb  <- hidden DIRECTORY, and an annotated file under it
//	emptydir/                   <- a real directory holding no files
//
// The annotated files are written here rather than copied from examples/
// because the point is the WALK, and a fixture the corpus might renumber would
// make a failure here read as a corpus change.
func dirArgTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	annotation := "# @intent: { entity: \"Order\", action: \"checkout\", " +
		"behavior: \"returns 402 on an expired card\", layer: \"request\" }\n"
	for _, rel := range []string{
		"spec/order_spec.rb",
		"spec/models/product_spec.rb",
		"spec/.cache/cached_spec.rb",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(annotation), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestExpandFiles_bareDirectoryIsTheDocumentedDescent asserts the expansion
// itself, at the layer the rewrite lives in.
//
// Sets are compared, not counts: a count says nothing about WHICH files, and
// the whole claim is that the sugar lands on the same set the explicit pattern
// does. `spec/**` is the expectation rather than a hand-written list, so a
// change to the descent moves both sides together and this stays an
// equivalence assertion rather than a second, drifting copy of the walk's
// rules.
func TestExpandFiles_bareDirectoryIsTheDocumentedDescent(t *testing.T) {
	root := dirArgTree(t)

	bare := ExpandFiles(filepath.Join(root, "spec"))
	explicit := ExpandFiles(filepath.Join(root, "spec") + "/**")

	if !reflect.DeepEqual(bare, explicit) {
		t.Errorf("ExpandFiles(spec) and ExpandFiles(spec/**) disagree:\n bare     %q\n explicit %q",
			bare, explicit)
	}
	// The positive control. An equivalence between two EMPTY results would also
	// satisfy the check above while proving that the walk has stopped working.
	if len(bare) == 0 {
		t.Fatal("ExpandFiles(spec) found nothing; the equivalence above proves nothing")
	}
	// The zero-segment case specifically: a rewrite that read `**` as "at least
	// one directory" would find the nested file and silently drop the top-level
	// one, and the equivalence above would still hold.
	if !containsSuffix(bare, "spec/order_spec.rb") {
		t.Errorf("the file directly inside spec/ is missing: %q", bare)
	}
	if !containsSuffix(bare, "spec/models/product_spec.rb") {
		t.Errorf("the file one level down is missing: %q", bare)
	}
}

// A hidden directory is not entered through the sugar either.
//
// This is glob_test.go's rule, re-checked HERE for one reason: it is evidence
// the rewrite lands on the documented descent rather than on some other walk.
// A rewrite that used filepath.WalkDir would pass the equivalence test above
// only if it happened to agree on this tree, and would yield the cached file.
//
// The non-empty control is load-bearing rather than decorative: an expansion
// that found NOTHING — which is exactly what this argument did before the
// rewrite existed — satisfies "no hidden path is present" vacuously, so
// without it this case would read as green against the very defect it sits
// beside.
func TestExpandFiles_bareDirectoryDoesNotDescendIntoAHiddenDirectory(t *testing.T) {
	root := dirArgTree(t)
	matches := ExpandFiles(filepath.Join(root, "spec"))
	if len(matches) == 0 {
		t.Fatal("ExpandFiles(spec) found nothing; the hidden-path check below would be vacuous")
	}
	for _, match := range matches {
		if strings.Contains(filepath.ToSlash(match), "/.cache/") {
			t.Errorf("the descent entered a hidden directory: %q", match)
		}
	}
	// The other half of the rule, unchanged from glob_test.go's: naming the
	// hidden component literally still reaches it, so the two do not conflict.
	literal := ExpandFiles(filepath.Join(root, "spec", ".cache"))
	if !containsSuffix(literal, "spec/.cache/cached_spec.rb") {
		t.Errorf("a literally named hidden directory must still be descended: %q", literal)
	}
}

// The three shapes the rewrite must LEAVE ALONE.
//
// Each is a byte-for-byte equality against what the same input produced before
// the rewrite existed, expressed as the property that survives: a nonexistent
// path and an empty directory find nothing (so runOverPatterns still errors),
// a FILE argument resolves to itself, and a MAGIC pattern is not rewritten —
// `spec/*` still matches `models` and `.cache` and still has them dropped by
// the isFile filter, which is glob_test.go's "directories are dropped" pin.
func TestExpandFiles_shapesTheRewriteLeavesAlone(t *testing.T) {
	root := dirArgTree(t)
	file := filepath.Join(root, "spec", "order_spec.rb")

	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			// An empty directory IS rewritten — to `emptydir/**`, which matches
			// no file. The never-silent-pass contract is preserved by the
			// result, not by an exemption.
			name:    "an empty directory still finds nothing",
			pattern: filepath.Join(root, "emptydir"),
			want:    nil,
		},
		{
			name:    "a nonexistent path still finds nothing",
			pattern: filepath.Join(root, "nope"),
			want:    nil,
		},
		{
			// The empty pattern is exempted explicitly, because os.Stat reads
			// "" as "." — so without the guard a caller passing an empty
			// argument would silently get a walk of the working directory
			// instead of the nothing it gets today.
			name:    "the empty pattern still finds nothing",
			pattern: "",
			want:    nil,
		},
		{
			name:    "a FILE argument resolves to itself",
			pattern: file,
			want:    []string{file},
		},
		{
			// hasMagic is true, so no rewrite happens: `spec/*` matches the two
			// subdirectories as well, and they are dropped rather than
			// descended. A rewrite applied here would turn this into the whole
			// tree.
			name:    "a magic pattern is not rewritten",
			pattern: filepath.Join(root, "spec") + "/*",
			want:    []string{file},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandFiles(tc.pattern)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExpandFiles(%q):\n got  %q\n want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

// containsSuffix reports whether any path ends with the given slash-separated
// suffix, so an expectation can name a file without spelling the temp root.
func containsSuffix(paths []string, suffix string) bool {
	for _, path := range paths {
		if strings.HasSuffix(filepath.ToSlash(path), suffix) {
			return true
		}
	}
	return false
}

// The `hasMagic` guard, pinned on the ONLY input that discriminates it.
//
// The `spec/*` case in the table above is not that input: `spec/*` is not a
// directory, so the `isDir` term refuses it either way and the case would stay
// green with the magic guard deleted. What needs the guard is a directory
// whose NAME carries a metacharacter — `a*b` — where the two terms disagree.
// Run and recorded rather than argued: deleting `hasMagic(pattern) ||` leaves
// the entire repo suite green without this test, and turns this exact argument
// from an `error: no file(s) match 'a*b'` into a passing walk.
//
// The guard's answer is the one AC6 asks for: a magic glob keeps today's
// bytes. `a*b` reaches globPath as a PATTERN — `*` matches any run of
// characters, including the literal `*` in the name — which matches the
// directory, and ExpandFiles' isFile filter then drops it. Rewriting it would
// silently reinterpret a glob as a path, so a user with an `a*b` directory
// beside an `ab` one could no longer name either unambiguously.
//
// A `*` is legal in a filename on Linux and refused on Windows, so a
// filesystem that will not host the input skips — asserting nothing there is
// correct, and asserting nothing on Linux is not.
func TestExpandFiles_aDirectoryWhoseNameCarriesMagicIsStillAPattern(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a*b")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Skipf("this filesystem will not host a directory named %q: %v", "a*b", err)
	}
	inside := filepath.Join(dir, "nested", "x_spec.rb")
	if err := os.WriteFile(inside, []byte("# nothing to annotate\n"), 0o644); err != nil {
		t.Skipf("this filesystem will not host a file under %q: %v", "a*b", err)
	}

	if got := ExpandFiles(dir); len(got) != 0 {
		t.Errorf("ExpandFiles(%q) = %q, want no matches: a pattern carrying magic must not be "+
			"rewritten into a descent", dir, got)
	}
	// The control. A correct answer of zero is the one shape that reads like a
	// test which found nothing, so the same tree must still be reachable by
	// the descent the user can type — or the emptiness above proves only that
	// the walk has stopped working.
	if got := ExpandFiles(dir + "/**"); !containsSuffix(got, "nested/x_spec.rb") {
		t.Errorf("ExpandFiles(%q) = %q, want the nested file — the empty result above proves nothing",
			dir+"/**", got)
	}
}

// --------------------------------------------------------------------------- //
// through the modes
// --------------------------------------------------------------------------- //

// TestRunSource_bareDirectoryProducesTheSameOutputAsTheExplicitDescent is the
// adopter-visible half of the equivalence: the same FILES is necessary but not
// sufficient, because the report prints the path it was HANDED, so a rewrite
// that reached the right files under a different spelling would produce a
// different document from the same run.
//
// Whole-output equality, not a set of Contains checks, for source_mode_test.go's
// reason: order across files, the two spaces after PASS, and the absence of a
// summary line all survive a substring assertion.
func TestRunSource_bareDirectoryProducesTheSameOutputAsTheExplicitDescent(t *testing.T) {
	schema := repoSchema(t)
	dir := filepath.Join(dirArgTree(t), "spec")

	bare, bareCode := runSourceText(t, []string{dir}, schema)
	explicit, explicitCode := runSourceText(t, []string{dir + "/**"}, schema)

	if bare != explicit {
		t.Errorf("--source DIR and --source DIR/** printed different reports:\n bare:\n%s\n explicit:\n%s",
			bare, explicit)
	}
	if bareCode != explicitCode {
		t.Errorf("--source DIR exited %d, --source DIR/** exited %d", bareCode, explicitCode)
	}
	if bareCode != 0 {
		t.Errorf("a tree of valid annotations should pass; exit %d, output:\n%s", bareCode, bare)
	}
	// The control: an identical pair of EMPTY reports would satisfy both checks
	// above. Two PASS lines, one per annotated file the walk must reach.
	if got := strings.Count(bare, "PASS  "); got != 2 {
		t.Errorf("expected one PASS line per annotated file (2), got %d:\n%s", got, bare)
	}
}

// The `--json` transition, which is the machine channel's half of the same
// defect: the document used to carry `kind: "no-match"` with `summary.files: 0`
// — a false sentence a consumer cannot argue with — for a directory full of
// annotated source.
func TestRunSourceJSON_bareDirectoryEmitsFindingsRatherThanNoMatch(t *testing.T) {
	schema := repoSchema(t)
	dir := filepath.Join(dirArgTree(t), "spec")

	document, code := runSourceJSON(t, []string{dir}, schema)

	if code != 0 {
		t.Errorf("RunSourceJSON exited %d over a tree of valid annotations; document:\n%s", code, document)
	}
	if strings.Contains(document, `"kind": "no-match"`) {
		t.Errorf("a directory of annotated source must not report as no-match:\n%s", document)
	}
	if !strings.Contains(document, `"files": 2`) {
		t.Errorf(`expected "files": 2 — one per annotated file the walk reached; document:`+"\n%s", document)
	}
	// The empty-directory arm of the same renderer, which must STILL be a
	// no-match: the sugar changes which inputs find files, not what happens
	// when none are found.
	empty := filepath.Join(dirArgTree(t), "emptydir")
	document, code = runSourceJSON(t, []string{empty}, schema)
	if code != 1 {
		t.Errorf("an empty directory must still fail the run; exit %d, document:\n%s", code, document)
	}
	if !strings.Contains(document, `"kind": "no-match"`) {
		t.Errorf("an empty directory must still report as no-match:\n%s", document)
	}
}

// Adopter `FILE...` mode gets the same expansion — one contract, no mode
// asymmetry.
//
// The rewrite lives in ExpandFiles, which both modes call, so this is the test
// that would catch a future fix applied in `--source`'s own path instead.
func TestRunAdopter_bareDirectoryWalksTheTree(t *testing.T) {
	schema := repoSchema(t)
	root := t.TempDir()
	intent := []byte(`{ "entity": "Order", "action": "checkout", ` +
		`"behavior": "returns 402 on an expired card", "layer": "request" }` + "\n")
	for _, rel := range []string{"intents/a.json", "intents/nested/b.json"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, intent, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "intents")

	var bareCode, explicitCode int
	bare := captureStdout(t, func() { bareCode = RunAdopter([]string{dir}, schema) })
	explicit := captureStdout(t, func() { explicitCode = RunAdopter([]string{dir + "/**"}, schema) })

	if bare != explicit {
		t.Errorf("FILE... DIR and FILE... DIR/** printed different reports:\n bare:\n%s\n explicit:\n%s",
			bare, explicit)
	}
	if bareCode != explicitCode || bareCode != 0 {
		t.Errorf("exit codes: bare %d, explicit %d, want 0 for both", bareCode, explicitCode)
	}
	if got := strings.Count(bare, "PASS  "); got != 2 {
		t.Errorf("expected one PASS line per intent file (2), got %d:\n%s", got, bare)
	}
}

// The loud-read contract is INHERITED, not weakened: a directory holding bytes
// that are not well-formed UTF-8 fails exactly as the explicit `DIR/**` does.
//
// Skipping such a file on the walk would have been the convenient choice and
// would have made `spec` mean something `spec/**` does not — which is the one
// thing this rewrite promises it is not.
func TestRunSource_bareDirectoryFailsLoudlyOnUnreadableBytes(t *testing.T) {
	schema := repoSchema(t)
	root := t.TempDir()
	dir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob.png"), []byte("\x89PNG\r\n\x1a\n\xff\xfe\x00"), 0o644); err != nil {
		t.Fatal(err)
	}

	bare, bareCode := runSourceText(t, []string{dir}, schema)
	explicit, explicitCode := runSourceText(t, []string{dir + "/**"}, schema)

	if bare != explicit {
		t.Errorf("the two spellings reported the unreadable file differently:\n bare:\n%s\n explicit:\n%s",
			bare, explicit)
	}
	if bareCode != 1 {
		t.Errorf("an unreadable file must fail the run; exit %d, output:\n%s", bareCode, bare)
	}
	if bareCode != explicitCode {
		t.Errorf("--source DIR exited %d, --source DIR/** exited %d", bareCode, explicitCode)
	}
	if !strings.Contains(bare, "FAIL  ") {
		t.Errorf("expected a loud FAIL line for the unreadable file:\n%s", bare)
	}
}

// --------------------------------------------------------------------------- //
// the diagnostic
// --------------------------------------------------------------------------- //

// The two residual no-match situations must be TOLD APART.
//
// This is the other half of the defect the rewrite above was written for, and
// the reason this file's opening paragraph can name the collision as the
// founding bug. The rewrite fixed the tree that HAS files — `--source spec` now
// descends it — and left the tree that has NONE producing the same bytes a
// nonexistent path produces, so `--source emptydir` and `--source nope` were
// still one sentence with a different name in it. A user could not tell a typo
// from an empty tree, and a --json consumer could not branch on the situation
// without parsing the argument name back out of prose.
//
// What is pinned is the PROPERTY, not the wording: the two stderr lines must
// differ BEYOND the echoed argument. So the assertion is on each diagnostic
// with its OWN argument substituted out for a fixed token — which is what
// "template-identical apart from the name" means, and the one check a wording
// change may not break. Asserting a literal sentence here would pin prose the
// ticket deliberately left to the implementer, and would go red on a rephrasing
// that fixed nothing and broke nothing.
//
// The contract the previous pin carried is kept rather than replaced: both
// situations still exit 1 (never a silent pass) and the diagnostic still
// belongs to stderr alone. The generic half is pinned positively too — a
// nonexistent path keeps today's exact bytes — because "they differ" is also
// satisfied by rewriting BOTH, and the whole value of this change is that only
// the situation the expansion can name acquires a clause.
func TestRunSource_residualNoMatchDiagnosticsAreDistinguishable(t *testing.T) {
	root := dirArgTree(t)
	emptyDir := filepath.Join(root, "emptydir")
	nonexistent := filepath.Join(root, "nope")

	cases := []struct {
		name    string
		pattern string
	}{
		{"an empty directory", emptyDir},
		{"a nonexistent path", nonexistent},
	}

	templates := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := captureRun(t, "--source", tc.pattern)
			if code != 1 {
				t.Errorf("run(--source %s) = %d, want 1", tc.pattern, code)
			}
			if stdout != "" {
				t.Errorf("the diagnostic belongs on stderr alone; stdout = %q", stdout)
			}
			if !strings.Contains(stderr, tc.pattern) {
				t.Errorf("the diagnostic must still echo the argument it is about; stderr = %q", stderr)
			}
			// The TEMPLATE: this situation's diagnostic with its own argument
			// replaced by a fixed token. Two situations whose templates match
			// are the collision this test exists to refuse.
			templates[tc.name] = strings.ReplaceAll(stderr, tc.pattern, "<ARG>")
		})
	}

	if len(templates) != len(cases) {
		t.Fatalf("a case did not record a template (%d of %d); the comparison below would be vacuous",
			len(templates), len(cases))
	}
	if templates["an empty directory"] == templates["a nonexistent path"] {
		t.Errorf("the two situations are still template-identical apart from the echoed argument: %q",
			templates["an empty directory"])
	}
	// The generic half, positively: everything that is NOT a directory argument
	// keeps today's bytes, so the pins elsewhere that assert them stay true and
	// a user's grep still finds them.
	if got, want := templates["a nonexistent path"], "error: no file(s) match '<ARG>'\n"; got != want {
		t.Errorf("a nonexistent path must keep the generic diagnostic:\n got  %q\n want %q", got, want)
	}
}

// The same discrimination on the MACHINE channel, which is the half a consumer
// cannot work around.
//
// A human reading stderr can at least see the difference once the sentence
// differs; a --json consumer branching on `kind` cannot, because both
// situations are still — correctly — a no-match. So the distinguishing fact has
// to be in the finding's own fields, and it is asserted here as a property of
// `errors[]` rather than as a sentence: the two error lists must differ once
// each finding's own argument is substituted out.
//
// The three pins this must not break are re-checked in place rather than
// trusted: both findings still carry kind no-match (version_test.go's --json
// row and intent_key_test.go's null-intent pin both depend on the finding
// staying a no-match finding), and both runs still exit 1.
func TestRunSourceJSON_residualNoMatchFindingsAreDistinguishable(t *testing.T) {
	schema := repoSchema(t)
	root := dirArgTree(t)
	emptyDir := filepath.Join(root, "emptydir")
	nonexistent := filepath.Join(root, "nope")

	errorsFor := func(pattern string) string {
		t.Helper()
		document, code := runSourceJSON(t, []string{pattern}, schema)
		if code != 1 {
			t.Errorf("--source --json %s exited %d, want 1; document:\n%s", pattern, code, document)
		}
		if !strings.Contains(document, `"kind": "`+KindNoMatch+`"`) {
			t.Errorf("%s must still report as a no-match finding; document:\n%s", pattern, document)
		}
		block := errorsBlock(t, document)
		return strings.ReplaceAll(block, pattern, "<ARG>")
	}

	empty := errorsFor(emptyDir)
	missing := errorsFor(nonexistent)

	if empty == missing {
		t.Errorf("the two no-match findings are indistinguishable from errors[] alone: %q", empty)
	}
	if !strings.Contains(missing, "no file(s) match <ARG>") {
		t.Errorf("a nonexistent path must keep the generic machine-readable message, got %q", missing)
	}
}

// errorsBlock returns the text of a one-finding document's `errors` array.
//
// Cut out of the raw document rather than parsed, because this package has no
// JSON decoder of its own and the renderer's fixed key order is precisely what
// makes the cut reliable. It fails loudly rather than returning "" on a shape
// it does not recognise: an empty string compares equal to another empty string,
// so a silent miss would make the caller's discrimination check pass vacuously.
func errorsBlock(t *testing.T, document string) string {
	t.Helper()
	const open = `"errors": [`
	start := strings.Index(document, open)
	if start < 0 {
		t.Fatalf("no errors array in document:\n%s", document)
	}
	rest := document[start+len(open):]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatalf("unterminated errors array in document:\n%s", document)
	}
	return strings.TrimSpace(rest[:end])
}

// The two inputs that make the guard's DISCRIMINATOR load-bearing, pinned on
// both renderers.
//
// The clause above fires on "the expansion read this argument as a directory to
// descend" — `expandDirectoryArgument(pattern) != pattern` — and NOT on the
// obvious-looking `pattern != "" && isDir(pattern)`. The tests above cannot
// tell those two apart: an empty directory and a nonexistent path are treated
// identically by both, so swapping one guard for the other leaves every other
// test in this package green while the binary starts describing an
// interpretation it did not use. Run and recorded rather than argued — that
// swap is exactly the mutation this test exists to fail.
//
// The two inputs where they disagree are the two the shipped guard exempts:
//
//   - A directory whose NAME carries magic. `a*b` reaches the matcher as a
//     PATTERN (the expansion pin above asserts that), so its empty result is
//     not "a directory holding no files" — and under the isDir guard the
//     diagnostic would confidently say it was, and would even quote the
//     un-rewritten argument as "the descent".
//   - The EMPTY pattern. os.Stat("") reads ".", so an isDir probe answers true
//     for it and an empty argument would acquire a directory clause naming a
//     descent that never happened.
//
// So what is pinned is the EXEMPTION: both keep today's generic bytes on
// stderr, and neither acquires the clause in `errors[]`. The generic half is
// asserted as an exact line rather than as an absence, because "does not
// contain the clause" is also satisfied by a diagnostic that broke some other
// way.
//
// A `*` is legal in a filename on Linux and refused on Windows, so a
// filesystem that will not host the magic-named directory skips that case
// alone — the empty pattern still asserts everywhere.
func TestRunSource_noMatchExemptionsKeepTheGenericDiagnostic(t *testing.T) {
	schema := repoSchema(t)

	magicNamed := filepath.Join(t.TempDir(), "a*b")
	magicNamedHosted := os.MkdirAll(filepath.Join(magicNamed, "nested"), 0o755) == nil

	cases := []struct {
		name    string
		pattern string
		skip    bool
	}{
		{
			// A directory of this name EXISTS, so an isDir-based guard fires
			// here; the shipped guard does not, because the tool read the
			// argument as a pattern.
			name:    "a directory whose name carries magic",
			pattern: magicNamed,
			skip:    !magicNamedHosted,
		},
		{
			// os.Stat("") reads "." — which is a directory — so an isDir-based
			// guard fires here too, naming a descent of the working directory
			// that never happened.
			name:    "the empty pattern",
			pattern: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("this filesystem will not host a directory named %q", "a*b")
			}

			code, stdout, stderr := captureRun(t, "--source", tc.pattern)
			if code != 1 {
				t.Errorf("run(--source %q) = %d, want 1", tc.pattern, code)
			}
			if stdout != "" {
				t.Errorf("the diagnostic belongs on stderr alone; stdout = %q", stdout)
			}
			if want := "error: no file(s) match '" + tc.pattern + "'\n"; stderr != want {
				t.Errorf("this situation must keep the generic diagnostic:\n got  %q\n want %q",
					stderr, want)
			}

			document, jsonCode := runSourceJSON(t, []string{tc.pattern}, schema)
			if jsonCode != 1 {
				t.Errorf("--source --json %q exited %d, want 1; document:\n%s",
					tc.pattern, jsonCode, document)
			}
			if !strings.Contains(document, `"kind": "`+KindNoMatch+`"`) {
				t.Errorf("%q must still report as a no-match finding; document:\n%s", tc.pattern, document)
			}
			if got, want := errorsBlock(t, document), `"no file(s) match `+tc.pattern+`"`; got != want {
				t.Errorf("this situation must keep the generic machine-readable message:\n got  %s\n want %s",
					got, want)
			}
		})
	}
}

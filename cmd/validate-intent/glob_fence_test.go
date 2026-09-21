package main

// The descent does not enter dependency or build directories.
//
// This file exists because of what the directory sugar read without it.
// `--source .` in a Node repository descended into `node_modules/` and `dist/`
// and reported on both, with three consequences the user could not work around
// — there is no `--exclude` flag, and the only escape was to stop using the
// bare-directory argument the README teaches:
//
//   - the overwhelming majority of the read set was code the user did not
//     write;
//   - every finding-bearing module was reported TWICE, because `tsc` emits its
//     source's docstring into `dist/` and the walk re-scanned the copy as an
//     independent annotation;
//   - a malformed `@intent:` inside a third-party package moved the EXIT CODE.
//     A clean suite reported a failed run over a file the user cannot edit,
//     which is the CI gate and the contract README.md states for it.
//
// The fence is the HIDDEN RULE's mechanism applied to a second category, at the
// same layer and with the same semantics, so what is pinned here mirrors what
// glob_dirarg_test.go pins for hidden directories: the walk does not enter a
// fenced directory, and NAMING ONE LITERALLY STILL REACHES IT. A user can
// always ask for a dependency tree; they never get it unasked.
//
// Two properties are pinned that no acceptance-level assertion would reach on
// its own, and each is the mutation a plausible rewrite produces:
//
//   - WHOLE SEGMENTS, never substrings. A `strings.Contains` spelling of this
//     fence silently deletes `spec/vendor_helpers/` and `src/distribution/`
//     from the walk, and every other test here stays green while it does.
//   - DIRECTORIES ONLY. Dropping the isDir term deletes a FILE named `log` or
//     `tmp` — ordinary filenames — from the walk, and again nothing else here
//     would notice.
//
// Counts are not asserted anywhere in this file. Which files were selected is
// the whole claim; a count is satisfied by the wrong set of the right size, and
// it rots the day a fixture gains a member.

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// passingAnnotation is a well-formed annotation that validates against the
// shipped schema, so a file carrying it contributes a PASS rather than noise.
const passingAnnotation = "// @intent: { entity: \"Order\", action: \"checkout\", " +
	"behavior: \"returns 402 on an expired card\", layer: \"request\" }\n"

// writeTree writes every rel -> body pair under a fresh temp root and returns
// the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// relativeMatches expands pattern and returns the matches as sorted
// root-relative slash paths, so an expectation can be written as the path list
// it is about rather than as the temp root plus a suffix check.
func relativeMatches(t *testing.T, root, pattern string) []string {
	t.Helper()
	var out []string
	for _, match := range ExpandFiles(pattern) {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			t.Fatalf("%q is not under the tree root %q: %v", match, root, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

// fencedTree writes one user-owned file beside one file inside EVERY fenced
// directory that the fence alone excludes.
//
// `.git` and `.test-build` are deliberately absent: both are hidden, so the
// rule directly above the fence already excludes them and a case built on
// either would stay green with the fence deleted. They are named in the list
// so it reads complete beside its two client twins — not because they
// discriminate. Every OTHER member is present, so deleting any one of them
// from skippedDirectories turns this red and names the member it lost.
//
// The two near-miss directories are user code and must survive: `vendor_helpers`
// contains `vendor`, `distribution` contains `dist`. They are inside `src/` so
// that the fence cannot be satisfied by a rule about top-level names.
func fencedTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"src/a.ts":                     passingAnnotation,
		"src/vendor_helpers/h.ts":      passingAnnotation,
		"src/distribution/d.ts":        passingAnnotation,
		"node_modules/pkg/index.js":    passingAnnotation,
		"dist/a.d.ts":                  passingAnnotation,
		"coverage/lcov-report/i.js":    passingAnnotation,
		"vendor/bundle/gem/lib/g.rb":   passingAnnotation,
		"tmp/cache/c.rb":               passingAnnotation,
		"log/development.rb":           passingAnnotation,
		"node_modules/pkg/nested/n.js": passingAnnotation,
	})
}

// The acceptance claim, asserted as the path list: the descent selects the
// user's own files and nothing from a fenced directory.
//
// The expectation is written out in full rather than as a "no fenced segment"
// scan, because an absence check is also satisfied by a walk that found
// nothing, and by one that lost `src/` along with `node_modules/`. Over-fencing
// and under-fencing both go red here, and the failure names the difference.
func TestExpandFiles_bareDirectoryDoesNotEnterAFencedDirectory(t *testing.T) {
	root := fencedTree(t)

	want := []string{
		"src/a.ts",
		"src/distribution/d.ts",
		"src/vendor_helpers/h.ts",
	}
	if got := relativeMatches(t, root, root); !reflect.DeepEqual(got, want) {
		t.Errorf("the bare-directory descent selected the wrong files:\n got  %q\n want %q", got, want)
	}
	// The explicit spelling of the same descent must agree, because the fence
	// lives in the shared walk rather than in the `DIR` -> `DIR/**` rewrite. A
	// fence applied at the rewrite passes the check above and fails this one.
	if got := relativeMatches(t, root, root+"/**"); !reflect.DeepEqual(got, want) {
		t.Errorf("DIR/** disagreed with the bare DIR — the fence is at the wrong layer:\n got  %q\n want %q",
			got, want)
	}
}

// The near-miss directories, pinned on their own so the failure is legible.
//
// The check above would also go red if `vendor_helpers` and `distribution`
// disappeared, but it would report "the wrong files" over a ten-line diff. This
// case says which rule broke: the fence matched a SUBSTRING of a component
// instead of the whole component, which is what `strings.Contains` produces and
// what the Ruby twin calls out explicitly.
func TestExpandFiles_theFenceMatchesWholeSegmentsNotSubstrings(t *testing.T) {
	root := fencedTree(t)
	matches := relativeMatches(t, root, root)

	for _, want := range []string{"src/vendor_helpers/h.ts", "src/distribution/d.ts"} {
		found := false
		for _, match := range matches {
			if match == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is project code and must still be walked; selected: %q", want, matches)
		}
	}
}

// Only a DIRECTORY is fenced.
//
// `log` and `tmp` are ordinary filenames as well as build-directory names, so a
// fence that tests the name alone deletes a file the user wrote. Deleting the
// isDir term from isFencedDirectory leaves every other case in this package
// green and fails only here — which is the whole reason this case exists rather
// than being argued in a comment.
func TestExpandFiles_theFenceSkipsDirectoriesNotFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"spec/log":       passingAnnotation,
		"spec/tmp":       passingAnnotation,
		"spec/a_spec.rb": passingAnnotation,
	})

	want := []string{"spec/a_spec.rb", "spec/log", "spec/tmp"}
	if got := relativeMatches(t, root, filepath.Join(root, "spec")); !reflect.DeepEqual(got, want) {
		t.Errorf("a FILE whose name is a fenced directory's must still be selected:\n got  %q\n want %q",
			got, want)
	}
}

// A tree containing no fenced directory is walked exactly as before.
//
// This is the no-op half of the change, and it is asserted as the complete
// sorted list for the reason the acceptance case above gives: a fence that
// over-matched would still satisfy any check phrased as an absence. The tree
// deliberately mixes depths and carries a hidden directory, so the rules the
// fence sits beside are re-checked on the same walk rather than assumed.
func TestExpandFiles_aTreeWithNoFencedDirectoryIsUnchanged(t *testing.T) {
	root := writeTree(t, map[string]string{
		"spec/a_spec.rb":               passingAnnotation,
		"spec/models/b_spec.rb":        passingAnnotation,
		"spec/requests/deep/c_spec.rb": passingAnnotation,
		"spec/.cache/hidden_spec.rb":   passingAnnotation,
	})

	want := []string{
		"spec/a_spec.rb",
		"spec/models/b_spec.rb",
		"spec/requests/deep/c_spec.rb",
	}
	if got := relativeMatches(t, root, filepath.Join(root, "spec")); !reflect.DeepEqual(got, want) {
		t.Errorf("a tree with nothing to fence must be walked exactly as before:\n got  %q\n want %q",
			got, want)
	}
}

// --------------------------------------------------------------------------- //
// the escape hatch
// --------------------------------------------------------------------------- //

// Naming a fenced directory literally still reaches it — the hidden rule's
// other half, inherited verbatim.
//
// Both spellings are pinned because they take different paths through the
// matcher: the bare directory is rewritten to `DIR/**` and resolves the fenced
// component through globInDir before the descent begins, while the FILE
// argument never reaches the walk at all. A fence placed in globInDir or in
// ExpandFiles' filter would pass one of these and fail the other.
func TestExpandFiles_aLiterallyNamedFencedDirectoryIsStillReached(t *testing.T) {
	root := fencedTree(t)
	fenced := filepath.Join(root, "node_modules", "pkg")

	bare := relativeMatches(t, root, fenced)
	want := []string{"node_modules/pkg/index.js", "node_modules/pkg/nested/n.js"}
	if !reflect.DeepEqual(bare, want) {
		t.Errorf("a literally named fenced directory must still be descended:\n got  %q\n want %q",
			bare, want)
	}

	file := filepath.Join(fenced, "index.js")
	if got := relativeMatches(t, root, file); !reflect.DeepEqual(got, []string{"node_modules/pkg/index.js"}) {
		t.Errorf("a FILE argument inside a fenced directory must still resolve to itself: %q", got)
	}
}

// Naming a fenced directory opens THAT directory and nothing further: the
// descent beneath it still applies both rules, at every level.
//
// This is the hidden rule's behaviour verbatim, and it is pinned because it is
// the sharpest available evidence that the fence really does live at the same
// layer rather than merely producing similar answers on a flat tree. Both
// checks run on ONE tree so the symmetry is asserted rather than described: a
// literally named hidden directory yields its own files and not its nested
// hidden one, and a literally named fenced directory does exactly the same.
func TestExpandFiles_aLiterallyNamedDirectoryOpensThatLevelOnly(t *testing.T) {
	root := writeTree(t, map[string]string{
		".cache/a.js":                        passingAnnotation,
		".cache/deep/b.js":                   passingAnnotation,
		".cache/.nested/c.js":                passingAnnotation,
		"node_modules/d.js":                  passingAnnotation,
		"node_modules/pkg/e.js":              passingAnnotation,
		"node_modules/pkg/node_modules/f.js": passingAnnotation,
	})

	hidden := relativeMatches(t, root, filepath.Join(root, ".cache"))
	wantHidden := []string{".cache/a.js", ".cache/deep/b.js"}
	if !reflect.DeepEqual(hidden, wantHidden) {
		t.Errorf("a literally named hidden directory opens that level only:\n got  %q\n want %q",
			hidden, wantHidden)
	}

	fencedMatches := relativeMatches(t, root, filepath.Join(root, "node_modules"))
	wantFenced := []string{"node_modules/d.js", "node_modules/pkg/e.js"}
	if !reflect.DeepEqual(fencedMatches, wantFenced) {
		t.Errorf("a literally named fenced directory must behave as the hidden one does:\n got  %q\n want %q",
			fencedMatches, wantFenced)
	}
}

// --------------------------------------------------------------------------- //
// through the mode
// --------------------------------------------------------------------------- //

// poisonedTree is the reproduction the defect was measured on: one well-formed,
// passing annotation in the user's own code, and one MALFORMED annotation in a
// third-party package the user cannot edit.
func poisonedTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"src/a.ts": passingAnnotation,
		// Unterminated: the extractor reports this as a finding rather than
		// skipping it, which is correct — the question is whether the tool
		// should have been reading the file at all.
		"node_modules/pkg7/f3.js": "// @intent: { entity: \"Broken\", action:\n",
	})
}

// A malformed annotation inside a fenced directory no longer moves the exit
// code.
//
// This is the consequence that makes the fence more than a volume improvement:
// `--source .` exited 1 with `ok: false` over `node_modules/pkg7/f3.js`, while
// `--source src` on the identical tree exited 0. The user's suite was clean and
// the CI gate said the run failed.
//
// The equivalence is asserted against `--source src` rather than against a
// literal 0, on the dirarg file's reasoning: the claim is that the two
// invocations now agree about the user's code, and two identical FAILURES would
// satisfy a bare "they match" check — so the passing control is asserted too.
func TestRunSource_aMalformedAnnotationInAFencedDirectoryDoesNotFailTheRun(t *testing.T) {
	schema := repoSchema(t)
	root := poisonedTree(t)

	whole, wholeCode := runSourceText(t, []string{root}, schema)
	_, ownCode := runSourceText(t, []string{filepath.Join(root, "src")}, schema)

	if wholeCode != 0 {
		t.Errorf("a clean suite must pass; --source DIR exited %d, output:\n%s", wholeCode, whole)
	}
	if wholeCode != ownCode {
		t.Errorf("--source DIR exited %d and --source DIR/src exited %d: the fenced file still moves "+
			"the exit code", wholeCode, ownCode)
	}
	if strings.Contains(whole, "f3.js") {
		t.Errorf("the report named a file inside a fenced directory:\n%s", whole)
	}
	// The control: an empty report exits 0 too, so the user's own file must be
	// there for the exit code above to mean anything.
	if !strings.Contains(whole, "PASS  ") {
		t.Errorf("expected the user's own annotation to be reported:\n%s", whole)
	}
}

// The machine channel's half of the same claim, which is the one a CI consumer
// branches on.
//
// `ok` is asserted directly because it is the field that was false, and the
// finding's absence is asserted by path because `ok: true` alone would also
// hold for a run that read the fenced file and somehow passed it.
func TestRunSourceJSON_aFencedDirectoryContributesNoFinding(t *testing.T) {
	schema := repoSchema(t)
	root := poisonedTree(t)

	document, code := runSourceJSON(t, []string{root}, schema)

	if code != 0 {
		t.Errorf("--source --json exited %d over a clean suite; document:\n%s", code, document)
	}
	if !strings.Contains(document, `"ok": true`) {
		t.Errorf("the run must report ok; document:\n%s", document)
	}
	if strings.Contains(document, "f3.js") {
		t.Errorf("a file inside a fenced directory reached findings[]:\n%s", document)
	}
	if !strings.Contains(document, "a.ts") {
		t.Errorf("the user's own file is missing from findings[]; document:\n%s", document)
	}
}

// Naming the fenced tree explicitly still fails the run, on both renderers.
//
// The escape hatch is only worth having if what it reaches is reported
// normally, and this is also the positive control for the two cases above: the
// malformed file really is malformed, so their exit 0 is the fence working
// rather than the extractor having stopped reading.
func TestRunSource_anExplicitlyNamedFencedDirectoryIsStillReported(t *testing.T) {
	schema := repoSchema(t)
	root := poisonedTree(t)
	fenced := filepath.Join(root, "node_modules", "pkg7")

	for _, pattern := range []string{fenced, filepath.Join(fenced, "f3.js")} {
		text, code := runSourceText(t, []string{pattern}, schema)
		if code != 1 {
			t.Errorf("--source %s exited %d, want 1; output:\n%s", pattern, code, text)
		}
		if !strings.Contains(text, "FAIL  ") {
			t.Errorf("--source %s must report the malformed annotation loudly:\n%s", pattern, text)
		}
	}
}

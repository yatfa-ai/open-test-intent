// Package corpus holds the agreement check for the invalid-fixture corpus that
// scripts/build-release.sh and tests/cross/run_cross_build.sh each restate.
//
// WHY THIS EXISTS
// ===============
//
// Both scripts smoke-test a built artifact by handing it a corpus of documents
// the schema must REJECT and requiring exit 1 for each. Both name that corpus in
// a `BAD_FIXTURES=(...)` block, and the two blocks are character-identical by
// intent — restated rather than sourced, for the reasons each script gives at
// the block: one is a release pipeline with its own exit-code contract, the
// other a harness, and neither is a library the other can source without
// building a set of binaries it has nothing to do with.
//
// Both scripts already guard that corpus, and both guard it in the ONE direction
// that cannot see the drift that actually happened. Each preflight walks the
// LIST and asks the disk whether each entry is there — which catches a fixture
// renamed or deleted out from under the list, and is blind to a fixture ADDED to
// examples/invalid/ and not to the list. That blind direction is not
// hypothetical: it is how the two blocks came to name four fixtures while
// examples/invalid/ held seven, with both scripts fully green and both preflights
// passing, for as long as it took someone to notice the sentence above
// run_cross_build.sh's block — "the same ones the binary's own self-test uses" —
// had quietly become false. The self-test globs the directory; the lists did not.
//
// So the property this file pins is the one neither script can pin about itself:
// the two restatements agree with EACH OTHER, and both agree with what is on
// DISK. Nothing here executes either script — the lists are read as text, which
// is what makes the check cheap enough to run in the normal `go test ./...` gate
// rather than only behind a release build.
//
// It is a package of its own rather than a test bolted onto tests/cross/install
// or tests/cross/sha256sums because its subject is neither of those tools: those
// packages calibrate a program that ships, and this one compares two shell
// scripts and a directory. The list-agreement precedents it is modelled on live
// beside the tools whose lists they check —
// install_test.go's TestTheFourTargetListsAgree and sha256sums'
// TestTheseThreeListsAgreeWithBuildRelease — and by that same rule this one
// belongs beside neither.
package corpus

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/yatfa-ai/open-test-intent/tests/cross/internal/crosstest"
)

const (
	buildRelease  = "scripts/build-release.sh"
	crossBuild    = "tests/cross/run_cross_build.sh"
	selfTestFile  = "cmd/validate-intent/selftest.go"
	invalidGlob   = "examples/invalid/*.json"
	invalidGlobGo = `invalidExamplesGlob\s*=\s*"([^"]*)"`
)

// TestTheCorpusListsAgree joins the two restated BAD_FIXTURES/GOOD_FIXTURE
// blocks to each other and to examples/invalid/*.json on disk.
//
// Three comparisons, with the order semantics each claim actually carries:
//
//   - The two BAD_FIXTURES blocks, as ORDERED lists. Both scripts describe the
//     other's block as character-identical to their own, and that is a claim
//     about the text, not merely about the membership. Comparing them as sets
//     would accept a reordering that makes both comments false.
//   - Each BAD_FIXTURES block against the disk, as a SET. The glob's order is
//     filepath.Glob's, which is lexical by accident of the API rather than by
//     anyone's decision, and the scripts are free to order their runs however
//     reads best. What must not differ is WHICH fixtures are named.
//   - GOOD_FIXTURE, as a plain string. It is one element long, so there is
//     nothing to say about order; it is checked because it sits in the same
//     restated block and drifts by the same mechanism.
//
// GOOD_FIXTURE is deliberately NOT compared against a disk glob. It names one
// accepted document out of examples/*.json, and picking one is the scripts'
// decision — unlike BAD_FIXTURES, whose neighbouring prose claims to be the
// whole invalid corpus. A subset is a defect only where wholeness was claimed.
func TestTheCorpusListsAgree(t *testing.T) {
	// The disk is the authority. Every other party here is a transcription of
	// what examples/invalid/ holds, and the self-test — the instrument that
	// decides whether the binary conforms — reads the directory rather than any
	// list, so the directory is what the lists owe agreement to.
	onDisk := invalidFixturesOnDisk(t)

	release := crosstest.ShellList(t, buildRelease, "BAD_FIXTURES")
	cross := crosstest.ShellList(t, crossBuild, "BAD_FIXTURES")

	// ORDERED. Both scripts call the other's block character-identical.
	if !equalOrdered(release, cross) {
		t.Errorf("the BAD_FIXTURES blocks in %s and %s are not identical:\n  %s: %v\n  %s: %v\n"+
			"Both scripts describe the other's block as character-identical to their own; one of them is now wrong.",
			buildRelease, crossBuild, buildRelease, release, crossBuild, cross)
	}

	// SETS, against the authority.
	for _, list := range []struct {
		name    string
		entries []string
	}{
		{buildRelease + "'s BAD_FIXTURES", release},
		{crossBuild + "'s BAD_FIXTURES", cross},
	} {
		missing, extra := crosstest.SetDiff(onDisk, list.entries)
		if len(missing) == 0 && len(extra) == 0 {
			continue
		}
		t.Errorf("%s does not name the same fixtures as %s on disk:\n  missing from %s: %v\n  named only by %s: %v\n"+
			"The fixtures missing from the list are rejected by the binary's own self-test and never seen by the smoke test, "+
			"and the ones named only by the list are asserted to exit 1 for a reason — a path that cannot be found — that is not a verdict.",
			list.name, invalidGlob,
			list.name, missing,
			list.name, extra)
	}

	// The scalar half of the same restated block.
	releaseGood := crosstest.ShellScalar(t, buildRelease, "GOOD_FIXTURE")
	crossGood := crosstest.ShellScalar(t, crossBuild, "GOOD_FIXTURE")
	if releaseGood != crossGood {
		t.Errorf("GOOD_FIXTURE differs between the two scripts:\n  %s: %q\n  %s: %q",
			buildRelease, releaseGood, crossBuild, crossGood)
	}
	if _, err := os.Stat(filepath.Join(crosstest.RepoRoot(t), releaseGood)); err != nil {
		t.Errorf("%s names GOOD_FIXTURE %q, which is not in the checkout: %v\n"+
			"The smoke tests assert it exits 0; a missing path exits 1, so this would surface as a schema failure it is not.",
			buildRelease, releaseGood, err)
	}
}

// TestTheGlobThisPackageComparesAgainstIsTheSelfTestsGlob keeps this file's own
// premise from being the thing that drifts.
//
// TestTheCorpusListsAgree treats examples/invalid/*.json as the authority
// because that is the glob cmd/validate-intent's self-test expands to decide
// which documents the binary must reject. Written here as a constant, that is
// one more unexecuted claim about another file of exactly the kind this package
// exists to close: point the self-test at a different directory and every
// comparison above would keep passing, in perfect agreement about a corpus
// nothing consults any more.
func TestTheGlobThisPackageComparesAgainstIsTheSelfTestsGlob(t *testing.T) {
	path := filepath.Join(crosstest.RepoRoot(t), selfTestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", selfTestFile, err)
	}

	matches := regexp.MustCompile(invalidGlobGo).FindAllStringSubmatch(string(data), -1)
	// Fatal rather than skip on both counts, for the reason crosstest's parsers
	// give: a premise this test could not read is not a premise it agrees with.
	if len(matches) == 0 {
		t.Fatalf("%s holds no `invalidExamplesGlob = \"...\"` constant. This package compares against %q on the strength of that constant, and can no longer see it — re-point this test rather than deleting it",
			selfTestFile, invalidGlob)
	}
	if len(matches) > 1 {
		t.Fatalf("%s assigns `invalidExamplesGlob` %d times, so which one the self-test expands is a guess", selfTestFile, len(matches))
	}
	if got := matches[0][1]; got != invalidGlob {
		t.Errorf("%s expands %q, but this package compares the BAD_FIXTURES lists against %q.\n"+
			"The agreement checks here would pass over a directory the self-test no longer reads.",
			selfTestFile, got, invalidGlob)
	}
}

// invalidFixturesOnDisk expands the glob from the repository root and returns
// repo-relative, slash-separated paths — the vocabulary the shell lists are
// written in, so all three parties are compared as the same kind of thing.
//
// An empty expansion is fatal. Every comparison in this package is a set
// difference against this result, and an empty authority agrees with an empty
// list and disagrees loudly with a correct one: the failure would be reported
// as the LISTS being wrong, which is the most expensive possible way to say
// "this test could not find the fixtures".
func invalidFixturesOnDisk(t *testing.T) []string {
	t.Helper()
	root := crosstest.RepoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(invalidGlob)))
	if err != nil {
		t.Fatalf("expanding %s: %v", invalidGlob, err)
	}
	if len(matches) == 0 {
		t.Fatalf("%s matched nothing under %s. The corpus this package compares the lists against is not there, which is not the same as the lists being wrong", invalidGlob, root)
	}

	fixtures := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			t.Fatalf("relativising %s against %s: %v", match, root, err)
		}
		fixtures = append(fixtures, filepath.ToSlash(rel))
	}
	return fixtures
}

func equalOrdered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

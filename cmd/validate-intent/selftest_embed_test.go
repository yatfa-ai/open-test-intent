package main

// The corpus fallback, property by property.
//
// tests/parity/run_parity.sh proves the bare self-test now works on a tree with
// no examples/ (its section 16d), and scripts/install.sh makes an adopter's
// install fail when it does not. Neither can say WHICH property broke: both
// report "exit 1" whether the cause is a lost fixture, a reordered expansion or
// a fallback that fired when it should not have. These name the properties one
// at a time.
//
// Two of them cannot be staged from outside the process at all, which is why
// runSelfTest takes its corpus as an argument:
//
//   - an EMBEDDED set that matches nothing. Staging it from the outside means
//     building and shipping a binary whose corpus is broken, which is the thing
//     the guard exists to catch rather than a way to test it.
//   - the two corpora expanding differently. The harness compares stdout on ONE
//     tree at a time; nothing out there runs both against each other.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// checkoutRoot is the repository root as seen from cmd/validate-intent, where
// `go test` runs. The on-disk half of every comparison below reads the real
// corpus through it — a synthetic one would prove the two code paths agree
// about a tree neither of them ships.
func checkoutRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "examples")); err != nil {
		t.Fatalf("no examples/ at %s, so the on-disk half cannot be read: %v", root, err)
	}
	return root
}

func onDiskSource(t *testing.T) fixtureSource {
	t.Helper()
	src := newFixtureSource(checkoutRoot(t))
	if src.corpus != nil {
		t.Fatal("newFixtureSource fell back to the embedded corpus inside a checkout; disk must win")
	}
	return src
}

func embeddedSource(root string) fixtureSource {
	return fixtureSource{root: root, corpus: opentestintent.ExamplesFS()}
}

var allPatterns = []string{
	validExamplesGlob, invalidExamplesGlob, validSourcesGlob, invalidSourcesGlob,
}

// TestEmbeddedAndOnDiskExpansionsAgree is the guard corpus_test.go's file-level
// comparison cannot be: equal bytes are not equal OUTPUT.
//
// The self-test prints one line per expanded path, in expansion order, so its
// stdout is a function of the four expansions and not of the file set. Two
// corpora holding identical files could still be walked into different lists —
// a different sort, a dotfile rule applied on one side only, a directory entry
// left in — and every byte of the corpus would still match while the released
// binary printed something a checkout never prints.
//
// Both sides go through this package's own fnmatch by construction (see
// expandEmbedded), so this asserts that construction rather than hoping for it.
func TestEmbeddedAndOnDiskExpansionsAgree(t *testing.T) {
	disk := onDiskSource(t)
	embedded := embeddedSource(disk.root)

	for _, pattern := range allPatterns {
		want := disk.expand(pattern)
		got := embedded.expand(pattern)

		if len(want) == 0 {
			t.Fatalf("%s expands to nothing on disk; this comparison would be vacuous", pattern)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s expands differently\n  on disk:  %v\n  embedded: %v", pattern, want, got)
		}
	}
}

// TestEmbeddedSelfTestIsByteIdenticalToTheCheckout is the whole promise of the
// slice, at the level where a difference names itself.
//
// An installed binary and a checkout must print the SAME bytes: the same
// relative paths, the same PASS/FAIL ordering, the same trailing line, and the
// same `checked`-versus-printed-lines disagreement RunSelfTest documents as
// deliberate. Anything less makes "the release self-tests itself" a claim about
// a different program than the one the parity harness proved correct.
func TestEmbeddedSelfTestIsByteIdenticalToTheCheckout(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
	schema := repoSchema(t)
	disk := onDiskSource(t)

	var diskCode, embeddedCode int
	fromDisk := captureStdout(t, func() { diskCode = runSelfTest(schema, disk) })
	fromEmbed := captureStdout(t, func() { embeddedCode = runSelfTest(schema, embeddedSource(disk.root)) })

	if diskCode != 0 || embeddedCode != 0 {
		t.Fatalf("the shipped corpus does not self-test clean: on disk %d, embedded %d\n%s", diskCode, embeddedCode, fromDisk)
	}
	if !strings.HasSuffix(fromDisk, "fixtures matched expectation.\n") {
		t.Fatalf("the on-disk run did not reach its summary line, so there is nothing to compare:\n%s", fromDisk)
	}
	if fromEmbed != fromDisk {
		t.Errorf("the embedded corpus prints different bytes than the checkout\n--- on disk ---\n%s\n--- embedded ---\n%s", fromDisk, fromEmbed)
	}
}

// TestTheEmptyFixtureGuardSurvivesTheFallback is SPGD-56's guard, asserted
// against the corpus that cannot be inspected on the host it runs on.
//
// An embedded set that matched nothing is the greenest possible lie: a released
// binary reporting "8/8 fixtures matched expectation." having lost its ability
// to reject anything, on a host with no corpus for anyone to look at. Every
// empty set must still be named, and the run must still fail BEFORE checking a
// single fixture — an incomplete inventory leaves no verdict to report.
func TestTheEmptyFixtureGuardSurvivesTheFallback(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
	schema := repoSchema(t)

	// Everything except examples/invalid/ — the set whose loss reads greenest,
	// because 12/12 becomes 8/8 and exit 0.
	gutted := fstest.MapFS{}
	full := opentestintent.ExamplesFS()
	for _, pattern := range allPatterns {
		if pattern == invalidExamplesGlob {
			continue
		}
		for _, rel := range (fixtureSource{corpus: full}).expand(pattern) {
			data, err := full.ReadFile(rel)
			if err != nil {
				t.Fatalf("reading %s out of the embedded corpus: %v", rel, err)
			}
			gutted[rel] = &fstest.MapFile{Data: data}
		}
	}

	var code int
	out := captureStdout(t, func() {
		code = runSelfTest(schema, fixtureSource{root: "/nowhere", corpus: gutted})
	})

	if code != 1 {
		t.Errorf("a gutted embedded corpus exited %d, want 1", code)
	}
	if out != "" {
		t.Errorf("the run checked fixtures before reporting an incomplete inventory:\n%s", out)
	}
}

// TestTheFallbackIsPerTreeAndNeverPerGlob is the load-bearing one.
//
// A checkout whose examples/invalid/ has been deleted must still fail loudly.
// The tempting alternative — fall back for whichever of the four sets came back
// empty — would heal that tree from the binary and report 12/12, so deleting
// the rejection fixtures would delete the coverage without a single case going
// red. That is the failure fileio.go names as this project's house defect
// wearing a new hat, and it is the reason the decision is taken once, for the
// whole tree, before anything is expanded.
func TestTheFallbackIsPerTreeAndNeverPerGlob(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
	schema := repoSchema(t)

	root := t.TempDir()
	copyCorpusInto(t, root, func(rel string) bool {
		return !strings.HasPrefix(rel, "examples/invalid/")
	})

	src := newFixtureSource(root)
	if src.corpus != nil {
		t.Fatal("a tree WITH examples/ fell back to the embedded corpus; the decision is not per-tree")
	}

	stderr := captureStderr(t, func() {
		if code := runSelfTest(schema, src); code != 1 {
			t.Errorf("a checkout missing examples/invalid/ exited %d, want 1", code)
		}
	})

	const want = "no fixtures match 'examples/invalid/*.json' — self-test cannot verify rejection"
	if !strings.Contains(stderr, want) {
		t.Errorf("the gutted set was not reported\n  want it to contain: %s\n  got: %s", want, stderr)
	}
	// And only that one: the three sets that ARE there must not be reported
	// missing, which is what would happen if the tree had been abandoned
	// wholesale rather than used as found.
	if strings.Contains(stderr, "no fixtures match 'examples/*.json'") {
		t.Errorf("a set that exists on disk was reported missing:\n%s", stderr)
	}
}

// TestAnExamplesTreeOnDiskWins is the other direction of the same rule, and the
// one that keeps the parity harness meaningful: run_parity.sh plants trees and
// expects the binary to read what is IN them. A fallback that fired while a
// real corpus sat there would make every such case test the binary's own copy.
func TestAnExamplesTreeOnDiskWins(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
	schema := repoSchema(t)

	root := t.TempDir()
	copyCorpusInto(t, root, func(string) bool { return true })

	// A fixture that must now FAIL. If the embedded copy were consulted the run
	// would pass, having validated a document nobody in this tree can see.
	broken := filepath.Join(root, "examples", "unit-order-total.json")
	if err := os.WriteFile(broken, []byte(`{"layer": "not-a-layer"}`), 0o644); err != nil {
		t.Fatalf("modifying a fixture: %v", err)
	}

	var code int
	out := captureStdout(t, func() { code = runSelfTest(schema, newFixtureSource(root)) })

	if code != 1 {
		t.Errorf("a modified on-disk fixture exited %d, want 1 — the embedded copy was used", code)
	}
	if !strings.Contains(out, "FAIL  examples/unit-order-total.json") {
		t.Errorf("the modification was not reported:\n%s", out)
	}
}

// TestSomethingOtherThanAnAbsentTreeDoesNotFallBack pins the narrow half of
// LoadSchema's absent/present rule, which this reuses: only "there is no
// examples/ here" is an absence of intent. A plain FILE on the name is somebody
// having done something, and answering it with a clean 12/12 out of the binary
// would be a tool failure wearing the costume of a pass.
func TestSomethingOtherThanAnAbsentTreeDoesNotFallBack(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
	schema := repoSchema(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "examples"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("writing the decoy: %v", err)
	}

	src := newFixtureSource(root)
	if src.corpus != nil {
		t.Fatal("a file sitting on examples/ was treated as an absent tree")
	}

	stderr := captureStderr(t, func() {
		if code := runSelfTest(schema, src); code != 1 {
			t.Errorf("exited %d, want 1", code)
		}
	})
	for _, pattern := range allPatterns {
		if !strings.Contains(stderr, "no fixtures match '"+pattern+"'") {
			t.Errorf("%s was not reported missing:\n%s", pattern, stderr)
		}
	}
}

// TestAnAbsentTreeUsesTheEmbeddedCorpus is the release-asset case at the level
// of the decision itself, so a fallback that stopped firing is a failure here
// and not only three layers up in a shell harness.
func TestAnAbsentTreeUsesTheEmbeddedCorpus(t *testing.T) {
	src := newFixtureSource(t.TempDir())
	if src.corpus == nil {
		t.Fatal("a tree with no examples/ did not fall back; an installed binary self-tests nothing")
	}
	for _, pattern := range allPatterns {
		if len(src.expand(pattern)) == 0 {
			t.Errorf("%s matched nothing in the embedded corpus", pattern)
		}
	}
}

// --- helpers ----------------------------------------------------------------

// copyCorpusInto writes the real corpus into root, keeping the paths for which
// keep returns true. Sourced from the EMBEDDED copy rather than from disk so a
// test tree cannot pick up a stray file someone left in examples/.
func copyCorpusInto(t *testing.T, root string, keep func(rel string) bool) {
	t.Helper()
	corpus := opentestintent.ExamplesFS()
	err := fs.WalkDir(corpus, "examples", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !keep(rel) {
			return nil
		}
		data, readErr := corpus.ReadFile(rel)
		if readErr != nil {
			return readErr
		}
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		t.Fatalf("staging a corpus under %s: %v", root, err)
	}
}

// captureStderr is captureStdout for the stream the empty-fixture guard writes
// to. The guard reports through fmt.Fprintf(os.Stderr, ...), which resolves the
// variable per call, so swapping it is enough.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = write

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := read.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()

	fn()
	os.Stderr = saved
	write.Close()
	out := <-done
	read.Close()
	return out
}

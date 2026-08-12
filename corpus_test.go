package opentestintent

// The guard that lets the embedded corpus be trusted.
//
// It is schema_test.go's argument applied to twelve files instead of one: an
// embedded asset is invisible, and nothing about reading the resulting binary
// says which bytes went in. What can go wrong here is narrower than for the
// schema, and worth naming precisely, because it decides what this file checks.
//
// The corpus has no reviewed-constant pin, and deliberately so. The schema is
// pinned to CanonicalV1SHA256 because a SECOND, independent copy of it exists
// outside this module — specguard-rspec vendors it and pins the same digest —
// so the constant is what makes the two halves comparable. There is no second
// copy of examples/. A constant here would pin the corpus against a number in
// this file and nothing else, and the only thing it could catch is "a fixture
// was edited", which is a reviewable diff rather than a drift between copies.
// A pin that has to be updated in the same commit as every fixture edit teaches
// people to update pins without reading them, which is how the schema's pin
// would eventually get updated too.
//
// What CAN go wrong, and what is checked:
//
//  1. The embed directive is RE-POINTED or NARROWED — at a different directory,
//     or back to a bare `//go:embed examples` that drops dotfiles and
//     underscore-prefixed names. Then the binary carries a corpus that is not
//     the one in the tree, and every claim about the two producing
//     identical output is void. TestEmbeddedCorpusIsTheOnDiskCorpus compares
//     them file for file and byte for byte, and names the file that differs.
//
//     That comparison can only see the NARROWING half if the tree contains a
//     name the narrower directive drops. Twelve ordinary names contain none, so
//     for as long as that was the whole corpus this test passed identically
//     with and without `all:` — green about a rule it was not being asked. It
//     is asked now: examples/.dotfile-canary.json is carried for exactly this
//     purpose, the assertion below refuses to run without it, and that file
//     explains itself at length.
//
//  2. The embedded corpus and the on-disk corpus EXPAND DIFFERENTLY. The
//     self-test's output is a function of the four globs, not of the file set,
//     so equal files with unequal expansions still produce different stdout,
//     which is the one thing the fallback promises not to do. The two are
//     expanded by two CALLERS of one matcher — ExpandFiles walks the disk and
//     expandEmbedded walks the embed, and both go through this project's own
//     fnmatch/pySplit/pyJoin. (fs.Glob was rejected precisely so there would
//     not be a second matcher to keep in agreement with the on-disk one; it
//     appears nowhere in the sources but the comments saying so.) One matcher
//     makes agreement likely, not certain — the two callers still apply the
//     dotfile rule and the isFile filter themselves. That belongs to the
//     consumer, so it is asserted there —
//     cmd/validate-intent/selftest_embed_test.go's
//     TestEmbeddedAndOnDiskExpansionsAgree and
//     TestBothExpansionsApplyTheDotfileRule.
//
// Digests here go through SHA256Hex, the module's one fold, rather than a
// second local one: see that function's comment for why a difference must mean
// the BYTES differ and nothing else.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// corpusDir is the tree corpus.go embeds, named once so the walk below and the
// embed directive cannot drift into describing different directories.
const corpusDir = "examples"

// walkOnDisk returns every regular file under examples/ as a slash-separated
// path relative to the module root, mapped to its bytes.
//
// Every file, with no filtering at all — this is the mirror check, and a filter
// here would be an exception the embed is allowed to have. The dotfile rule
// that DOES apply to the self-test is a property of the glob, not of the
// corpus, and is asserted where it applies.
func walkOnDisk(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(corpusDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files[filepath.ToSlash(path)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read the corpus at %s/: %v", corpusDir, err)
	}
	return files
}

// walkEmbedded returns the same shape, read out of the compiled-in copy.
func walkEmbedded(t *testing.T) map[string][]byte {
	t.Helper()
	fsys := ExamplesFS()
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, corpusDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}
		files[path] = data
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read %s/ out of the embedded corpus: %v\n"+
			"The //go:embed directive in corpus.go no longer covers a directory of that "+
			"name, so an installed binary carries no fixtures at all and its self-test "+
			"reports four empty sets.", corpusDir, err)
	}
	return files
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// anyKeySatisfies reports whether at least one path in the set matches pred.
func anyKeySatisfies(m map[string][]byte, pred func(string) bool) bool {
	for key := range m {
		if pred(key) {
			return true
		}
	}
	return false
}

// droppedByABareEmbed reports whether `//go:embed examples` (without `all:`)
// would leave this path out: go:embed's default excludes any entry whose own
// name begins with "." or "_", at any depth, so the test is per SEGMENT and not
// on the path as a whole.
func droppedByABareEmbed(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "_") {
			return true
		}
	}
	return false
}

func TestEmbeddedCorpusIsTheOnDiskCorpus(t *testing.T) {
	onDisk := walkOnDisk(t)
	embedded := walkEmbedded(t)

	if len(onDisk) == 0 {
		// Without this the comparisons below are satisfied by two empty sets —
		// a check that passes having verified nothing, which is the failure the
		// self-test's own empty-fixture guard exists to refuse. The count is
		// not asserted: a corpus grows, and a number in a test that has to be
		// edited whenever a fixture is added is a number people edit without
		// reading.
		t.Fatalf("found no files under %s/; this test cannot verify anything", corpusDir)
	}

	// The second way this test can verify less than it appears to, and the
	// subtler one: a corpus of ordinary names is a corpus on which `all:` and a
	// bare directive produce the SAME bytes. Both halves below would then agree
	// under either directive, this test would pass either way, and the error
	// message it prints about dropped names could never print at all — a guard
	// green about a rule nothing was asking it. Fatal rather than skipped: the
	// binary's corpus would stop mirroring the tree only on the release asset,
	// the one host where nobody can open examples/ to find out.
	if !anyKeySatisfies(onDisk, droppedByABareEmbed) {
		t.Fatalf("no file under %s/ has a path segment beginning with '.' or '_', so a bare\n"+
			"`//go:embed examples` would carry exactly what `all:examples` carries and this test\n"+
			"cannot tell the two apart. %s/.dotfile-canary.json existed to prevent that and has\n"+
			"been deleted or renamed; restore it, or replace it with another name go:embed's\n"+
			"default drops. See that file, and corpus.go's directive, for why it is load-bearing.",
			corpusDir, corpusDir)
	}

	// (1) Nothing the tree has is missing from the binary, and every byte
	// agrees. A fixture edited without a rebuild cannot reach here — go:embed
	// re-reads at build time — so a difference means the DIRECTIVE no longer
	// names this tree.
	for _, name := range sortedKeys(onDisk) {
		want := onDisk[name]
		got, ok := embedded[name]
		if !ok {
			t.Errorf("%s is in the corpus on disk and NOT in the binary.\n"+
				"The //go:embed directive in corpus.go no longer covers it — a bare "+
				"`//go:embed examples` drops names beginning with '.' or '_'; `all:` does not. "+
				"A self-test on a host with no checkout would silently skip this fixture.",
				name)
			continue
		}
		if SHA256Hex(got) != SHA256Hex(want) {
			t.Errorf("%s differs between the binary and the tree\n  embedded: %s (%d bytes)\n  on disk:  %s (%d bytes)\n"+
				"The //go:embed directive has been re-pointed at a different tree. There must "+
				"be exactly one copy of this corpus.",
				name, SHA256Hex(got), len(got), SHA256Hex(want), len(want))
		}
	}

	// (2) And nothing the binary carries is absent from the tree. Without this
	// the check above passes on a binary carrying the corpus PLUS a fixture
	// that no longer exists — which a self-test would then run, and report a
	// verdict on, in a checkout where nobody can read it.
	for _, name := range sortedKeys(embedded) {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("%s is compiled into the binary and is NOT in the corpus on disk.\n"+
				"An installed binary would self-test a fixture this repository does not have.",
				name)
		}
	}
}

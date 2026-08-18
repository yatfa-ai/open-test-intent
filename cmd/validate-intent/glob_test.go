package main

// Coverage for the matcher's DIRECTORY WALK.
//
// validator_test.go covers matchName, which decides one name against one pattern.
// Everything here is about the walk around it, and the two are worth separating
// because a matcher that is right about names can still be wrong about which
// names it is ever shown — and that failure is silent. A `**` that quietly
// skips a directory reports a clean pass over a smaller set of files, which
// looks exactly like a clean pass over all of them.
//
// The one exception is the OS-BYTE group at the end, which is about name
// identity rather than the walk. It lives here because it is only observable
// through a real directory: a filename holding an undecodable byte cannot be
// typed as a literal argument and reach the matcher at all — globPath resolves
// a magic-free pattern through lexists — so the property has to be driven by
// expanding a pattern over files that actually exist on disk.
//
// The tree is built here rather than committed, so a reader can see the whole
// input beside the expectation instead of holding two files in their head.

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// globTree writes the fixture tree and returns its root.
//
//	spec/a.json
//	spec/models/b.json
//	spec/models/order_spec.rb
//	spec/requests/c.json
//	spec/requests/admin/d.json
//	spec/real/e.json
//	spec/linked -> real      <- SYMLINKED directory
//	spec/.secret/f.json      <- hidden DIRECTORY, and what is under it
//	spec/.hidden.json        <- hidden FILE
//
// The symlink is in the SHARED tree rather than in a test of its own because
// descent through it is not an optional behaviour: glob.go:listDir classifies
// with os.Stat, which follows symlinks, and says so as one of the three
// load-bearing properties of the walk. The obvious rewrite — filepath.WalkDir
// plus a suffix filter — does not follow them, and would then skip every test
// file under a symlinked spec directory while reporting a confident clean pass.
// Carried here, that rewrite fails the primary `**` case below rather than one
// case a reader might read as exotic.
func globTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{
		"spec/a.json",
		"spec/models/b.json",
		"spec/models/order_spec.rb",
		"spec/requests/c.json",
		"spec/requests/admin/d.json",
		"spec/real/e.json",
		"spec/.secret/f.json",
		"spec/.hidden.json",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("real", filepath.Join(root, "spec", "linked")); err != nil {
		t.Fatalf("the tree needs a symlinked directory to be worth walking: %v", err)
	}
	return root
}

// expandRelative runs ExpandFiles against the tree and returns the matches as
// slash-separated paths relative to root, so the expectations below read as the
// tree does.
func expandRelative(t *testing.T, root, pattern string) []string {
	t.Helper()
	matches := ExpandFiles(root + "/" + pattern)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			t.Fatalf("Rel(%q, %q): %v", root, match, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

func TestExpandFiles(t *testing.T) {
	root := globTree(t)

	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			// The zero-segment match. `**` matches NO directories as well as
			// some, which is the only reason a file directly in spec/ is found.
			// A recursion that reads `**` as "at least one directory" drops
			// spec/a.json and reports a confident pass over the rest.
			// A `**` that quietly skips a symlinked directory reports a clean
			// pass over a smaller set of files. spec/linked/e.json is the same
			// file as spec/real/e.json and is listed twice on purpose: the user
			// can name both paths, so the walk yields both.
			name:    "** matches zero segments",
			pattern: "spec/**/*.json",
			want: []string{
				"spec/a.json",
				"spec/linked/e.json",
				"spec/models/b.json",
				"spec/real/e.json",
				"spec/requests/admin/d.json",
				"spec/requests/c.json",
			},
		},
		{
			// A hidden DIRECTORY is not entered, so nothing beneath it is
			// reachable — the rule applies at every level of the descent, not
			// only to the last component.
			name:    "** never descends into a hidden directory",
			pattern: "spec/**/f.json",
			want:    nil,
		},
		{
			// Naming the hidden component literally still reaches it. That path
			// does not go through the descent, so the two rules do not conflict.
			name:    "a literal hidden component is reachable",
			pattern: "spec/.secret/*.json",
			want:    []string{"spec/.secret/f.json"},
		},
		{
			// `*` never matches a leading dot. Without this, an editor's swap
			// file or a draft is validated as though it were a fixture.
			name:    "* skips a hidden file",
			pattern: "spec/*.json",
			want:    []string{"spec/a.json"},
		},
		{
			// ...unless the pattern is itself hidden.
			name:    "a dot-leading pattern matches hidden files",
			pattern: "spec/.*.json",
			want:    []string{"spec/.hidden.json"},
		},
		{
			// Only a WHOLE component is recursive. `re**s` is an ordinary
			// wildcard, so it matches `requests` and does not descend past it —
			// admin/d.json is NOT found.
			name:    "a partial ** is an ordinary wildcard",
			pattern: "spec/re**s/*.json",
			want:    []string{"spec/requests/c.json"},
		},
		{
			// A directory matched by a pattern must not be handed to a reader
			// as though it were a file. `spec/*` matches `models`, `requests`
			// and `.secret` too; ExpandFiles drops them.
			name:    "directories are dropped, not reported as unreadable files",
			pattern: "spec/*",
			want:    []string{"spec/a.json"},
		},
		{
			name:    "** finds every file at every depth",
			pattern: "spec/**/*.rb",
			want:    []string{"spec/models/order_spec.rb"},
		},
		{
			name:    "a pattern matching nothing returns nothing",
			pattern: "spec/**/*.ts",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandRelative(t, root, tc.pattern)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExpandFiles(%q):\n got  %v\n want %v", tc.pattern, got, tc.want)
			}
		})
	}
}

// A `**` whose parent directory does not exist is a NO-MATCH, not a match on
// the parent.
//
// This needs a pattern whose LAST component is the recursive one. A
// `nope/**/*.json` shape cannot show it — the trailing `*.json` finds nothing
// inside a directory that is not there either way — and ExpandFiles' isFile
// filter hides it at the CLI. So it is asserted at Glob, below ExpandFiles.
func TestGlobDoesNotInventAMatchForAMissingDirectory(t *testing.T) {
	root := globTree(t)
	for _, pattern := range []string{"nope/**", "spec/a.json/**"} {
		if got := Glob(root + "/" + pattern); len(got) != 0 {
			t.Errorf("Glob(%q) = %v, want no matches", pattern, got)
		}
	}
	// The positive control: the same shape over a directory that DOES exist
	// must still match, so the guard above cannot be satisfied by a matcher
	// that has stopped answering at all.
	if got := Glob(root + "/spec/**"); len(got) == 0 {
		t.Error("Glob(spec/**) found nothing; the guard above proves nothing")
	}
}

// A symlink LOOP must terminate, and glob.go says how: not with a visited-inode
// guard, but because the OS eventually refuses the too-deep path and the failed
// read contributes no names.
//
// That is a deliberate choice with a failure mode on each side, which is why it
// is pinned. Cutting the walk off earlier would silently stop finding files the
// user can name; not terminating at all turns `--source spec/**` on an ordinary
// tree with one self-referential link into a hang, with no output to explain it.
//
// The walk runs on its own goroutine so that a regression reports "did not
// terminate" here rather than killing the package with a panic from the go test
// timeout, several minutes later, naming every goroutine but not this rule.
func TestGlobTerminatesOnASymlinkLoop(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "spec")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(dir, "self")); err != nil {
		t.Fatalf("the loop needs a symlink: %v", err)
	}

	done := make(chan []string, 1)
	go func() { done <- ExpandFiles(root + "/spec/**/*.json") }()

	select {
	case matches := <-done:
		// Terminating is half the rule; the other half is that the loop was
		// really entered. A matcher that refused to follow the link would also
		// return promptly, and would satisfy a bare "it came back" assertion
		// while having stopped finding files the user can name.
		//
		// So: spec/a.json is reachable as itself AND through the link, at every
		// depth the OS still accepts, and more than one match is the evidence
		// the descent happened. The COUNT is not pinned — it is however many
		// levels fit in PATH_MAX under this tmpdir, which is a property of the
		// machine and not of the matcher.
		found := 0
		for _, match := range matches {
			if filepath.Base(match) == "a.json" {
				found++
			}
		}
		if found == 0 {
			t.Errorf("the walk terminated without finding spec/a.json: %v", matches)
		}
		if found == 1 {
			t.Error("the walk never entered the loop, so its termination proves nothing")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("ExpandFiles did not terminate on a symlink loop within 60s")
	}
}

// Sorting is ExpandFiles' job, and the report's reading order depends on it: a
// run over the same tree must list the same files in the same order twice.
func TestExpandFilesSorts(t *testing.T) {
	root := globTree(t)
	first := expandRelative(t, root, "spec/**/*.json")
	if !sort.StringsAreSorted(ExpandFiles(root + "/spec/**/*.json")) {
		t.Error("ExpandFiles did not return sorted matches")
	}
	second := expandRelative(t, root, "spec/**/*.json")
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Errorf("two identical expansions disagreed:\n %v\n %v", first, second)
	}
}

// osByteTree writes four files whose names differ only in one undecodable byte,
// and returns its root.
//
//	a\xe9.json
//	a\xfe.json
//	a\xff.json
//	ab.json     <- the ASCII control, so a class can be shown EXCLUDING a byte
//
// These names are legal on Linux and refused by filesystems that require valid
// UTF-8 (APFS, NTFS). The repo runs a genuine cross-platform matrix, so a
// filesystem that cannot host the input skips — asserting nothing there is
// correct, and asserting nothing on Linux is not, which is why the skip is per
// tree and not per case.
func osByteTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a\xe9.json", "a\xfe.json", "a\xff.json", "ab.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0o644); err != nil {
			t.Skipf("this filesystem will not host a non-UTF-8 filename (%q): %v", name, err)
		}
	}
	return root
}

// The matcher must keep two undecodable BYTES apart, not only the renderer.
//
// glob.go:pathRunes exists solely for this: plain []rune maps every byte of an
// ill-formed sequence to U+FFFD, so `a\xe9.json`, `a\xfe.json` and `a\xff.json`
// would compare equal to each other, and each undecodable byte becomes a
// distinct negative sentinel instead. render_test.go pins the DISPLAY half of
// that guarantee over these same three paths — a quiet merge into one `"file"`
// value. This is the PRODUCING half: which files are validated in the first
// place. Without it, `--source 'a[\xe9].json'` validates three files when the
// user named one, and the renderer faithfully reports all three under correctly
// distinguished names, so nothing downstream can notice the widening.
//
// MUTATION, run and recorded rather than asserted: replacing pathRunes' body
// with a bare `return []rune(s)` leaves the ENTIRE repo suite green — that is
// the gap this test closes. Every case below fails under that mutant, and each
// one records the count the mutant returns for it beside the case itself. That
// is deliberately not written here as one sequence: the discriminating cases
// span two test functions, so an ordered list has no unambiguous referent and
// drifts off its cases silently the moment one is reordered or added.
//
// The cases have to be CHARACTER CLASSES (or a `*` around a literal byte). The
// obvious alternatives are all vacuous here and were checked:
//
//   - a literal `a\xe9.json` never reaches the matcher at all — globPath
//     short-circuits on !hasMagic and resolves through lexists;
//   - `a*.json` and `a?.json` return 4 in both worlds, because every one of
//     these names has exactly one character in that position.
//
// Sets are asserted, not counts: a count of 1 does not say WHICH file, and the
// defect is a substitution as much as a widening.
func TestMatcherKeepsPathsWithOSBytesApart(t *testing.T) {
	root := osByteTree(t)

	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			// The headline. One byte named, one file matched. Collapsing
			// mutant: 3 — every undecodable byte answers to the class.
			name:    "a class naming one undecodable byte matches only that file",
			pattern: "a[\xe9].json",
			want:    []string{"a\xe9.json"},
		},
		{
			// Two members, two files — and NOT the third, which differs from
			// both only in that byte. Collapsing mutant: 3, the third included.
			name:    "a class naming two bytes matches exactly those two",
			pattern: "a[\xe9\xff].json",
			want:    []string{"a\xe9.json", "a\xff.json"},
		},
		{
			// The negated form, which fails in the opposite direction: a
			// collapsing matcher NARROWS here rather than widening — collapsing
			// mutant: 1, where every other DISCRIMINATING case returns 3 — so a
			// guard that only ever looked for over-matching would miss it. Being
			// the one case that pulls the other way, it is also why these counts
			// are recorded per case rather than as one sequence.
			// The ASCII name is in the expectation because a negated class
			// must admit everything it did not name, not merely the two other
			// byte-bearing names.
			name:    "a negated class excludes only the named byte",
			pattern: "a[!\xe9].json",
			// Byte order, not eye order: 'b' is 0x62, so ab.json sorts BEFORE
			// the two names carrying a 0xfe/0xff byte. expandRelative returns
			// sorted matches, so every want here is written pre-sorted.
			want: []string{"ab.json", "a\xfe.json", "a\xff.json"},
		},
		{
			// Not a class at all: a `*` on either side of a literal byte. The
			// byte still has to compare equal only to itself. Collapsing
			// mutant: 3, all three bytes being U+FFFD there.
			name:    "a `*` around a literal undecodable byte matches only the file holding it",
			pattern: "*\xe9*",
			want:    []string{"a\xe9.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandRelative(t, root, tc.pattern); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExpandFiles(%q):\n got  %q\n want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

// The documented range contract: a sentinel "can never ... fall inside a
// character-class range".
//
// `a[\xe9-\xff].json` matches NOTHING. The sentinels are negative, so the span
// runs from -0xe9 down to -0xff and is empty — there is no ordering in which a
// user's range over raw bytes silently selects files. Under the collapsing
// mutant the same pattern is a range over U+FFFD..U+FFFD and matches all three
// (collapsing mutant: 3, against a correct answer of 0).
//
// A correct answer of zero is the one shape that reads like a test which found
// nothing, so it carries its own positive control: the identical `a[x-y].json`
// shape over ASCII must still match, or the zero above proves only that ranges
// have stopped working.
func TestOSByteSentinelsNeverFallInsideACharacterClassRange(t *testing.T) {
	root := osByteTree(t)

	if got := expandRelative(t, root, "a[\xe9-\xff].json"); len(got) != 0 {
		t.Errorf("ExpandFiles(%q) = %q, want no matches: a range must not span undecodable bytes",
			"a[\xe9-\xff].json", got)
	}

	// The control. Same file set, same class-with-range shape, ASCII members.
	if got, want := expandRelative(t, root, "a[a-c].json"), []string{"ab.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the control ExpandFiles(%q) = %q, want %q — ranges are not working at all, "+
			"so the empty result above proves nothing", "a[a-c].json", got, want)
	}
}

// `?` counts an undecodable byte as exactly ONE character, which is the other
// half of pathRunes' contract and the reason a sentinel is a rune rather than
// an escape expanded into several.
//
// Stated honestly: this case does NOT discriminate the collapsing mutant — it
// returns 4 there too, because U+FFFD is also one character. It is here for the
// mutation that changes a byte's WIDTH, run and recorded rather than argued:
// appending the sentinel TWICE per undecodable byte fails this test, because
// `a?.json` then misses the three files whose one byte spends two characters.
//
// Not on that list, though it looks like it belongs: advancing by the
// DecodeRuneInString size instead of by one. Inside this branch the guard is
// `r == utf8.RuneError && size == 1`, so size IS one and `i += size` is a
// no-op refactor — applied, the suite stays green. No test can catch it, so it
// would overstate what this one is worth.
func TestOSByteCountsAsOneCharacter(t *testing.T) {
	root := osByteTree(t)
	want := []string{"ab.json", "a\xe9.json", "a\xfe.json", "a\xff.json"}
	if got := expandRelative(t, root, "a?.json"); !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandFiles(%q):\n got  %q\n want %q", "a?.json", got, want)
	}
}

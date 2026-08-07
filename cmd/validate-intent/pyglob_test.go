package main

// Unit coverage for the recursive `**` half of pyglob.go.
//
// WHY THIS FILE ASSERTS ON PATH LISTS AND NEVER ON COUNTS
// ======================================================
//
// Both obvious Go implementations of `**` were measured against the probe tree
// built below, and the failure mode is not "fewer results":
//
//	filepath.Glob("spec/**/*.json")   wrongly INCLUDES spec/.secret/f.json, and
//	                                  MISSES spec/a.json (`**` matching zero
//	                                  segments) and spec/requests/admin/d.json
//	                                  (depth 2).
//
//	filepath.WalkDir("spec") + a      returns the SAME NUMBER OF PATHS as
//	".json" suffix filter             Python on this tree, and a different set:
//	                                  it wrongly includes spec/.secret/f.json
//	                                  and misses spec/linked/b.json, because
//	                                  fs.WalkDir does not follow symlinks and
//	                                  Python does.
//
// A test that sanity-checks by counting therefore certifies a validator that
// silently skips every test file under a symlinked directory — a clean pass
// over a smaller set of files, which is the exact outcome the `**` refusal in
// slices 1-3 existed to prevent. So every case here pins the whole sorted list.
//
// The expectations are written out literally rather than being generated from
// python3, so they still assert something on a machine with no python3. The
// differential test at the bottom then re-derives all of them from the real
// glob module, and skips LOUDLY when the oracle is unavailable.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// buildGlobTree writes the probe tree the recursive cases are measured over and
// returns its root. Every shape in it is load-bearing:
//
//	spec/a.json               a match for `**` expanding to ZERO segments
//	spec/models/b.json        depth 1
//	spec/requests/admin/d.json depth 2
//	spec/real/b.json          the symlink target's real path
//	spec/linked -> real       a SYMLINKED directory: Python descends into it
//	spec/.secret/f.json       a HIDDEN directory: Python never descends into it
func buildGlobTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	for _, dir := range []string{"spec/models", "spec/requests/admin", "spec/real", "spec/.secret"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, file := range []string{
		"spec/a.json",
		"spec/models/b.json",
		"spec/requests/c.json",
		"spec/requests/admin/d.json",
		"spec/real/b.json",
		"spec/.secret/f.json",
	} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	if err := os.Symlink("real", filepath.Join(root, "spec/linked")); err != nil {
		t.Skipf("this filesystem does not support symlinks (%v) — the symlink half of the `**` contract cannot be verified here", err)
	}
	return root
}

// globCases are patterns and the exact sorted list `glob.glob(p, recursive=True)`
// answers with over buildGlobTree, relative to its root.
var globCases = []struct {
	pattern string
	want    []string
	why     string
}{
	{
		pattern: "spec/**/*.json",
		want: []string{
			"spec/a.json",
			"spec/linked/b.json",
			"spec/models/b.json",
			"spec/real/b.json",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		},
		why: "the whole contract in one pattern: zero segments, depth 1, depth 2, " +
			"through a symlink, and never into a hidden directory",
	},
	{
		pattern: "spec/**/a.json",
		want:    []string{"spec/a.json"},
		why:     "`**` matches ZERO segments — the case a hand-rolled recursion drops",
	},
	{
		pattern: "spec/re**s/*.json",
		want:    []string{"spec/requests/c.json"},
		why:     "`**` is recursive only as a WHOLE component; here it is an ordinary wildcard",
	},
	{
		pattern: "spec/**/",
		want: []string{
			"spec/",
			"spec/linked/",
			"spec/models/",
			"spec/real/",
			"spec/requests/",
			"spec/requests/admin/",
		},
		why: "a trailing separator matches directories only, each keeping its slash",
	},
	{
		pattern: "spec/**",
		want: []string{
			"spec/",
			"spec/a.json",
			"spec/linked",
			"spec/linked/b.json",
			"spec/models",
			"spec/models/b.json",
			"spec/real",
			"spec/real/b.json",
			"spec/requests",
			"spec/requests/admin",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		},
		why: "a trailing `**` yields the directory itself (zero segments) plus every descendant",
	},
	{
		pattern: "spec/**/.secret/*.json",
		want:    []string{"spec/.secret/f.json"},
		why: "the hidden rule belongs to the WILDCARD, not to the path: naming the " +
			"hidden component literally still reaches it",
	},
	{
		pattern: "spec/**/**/*.json",
		want: []string{
			"spec/a.json",
			"spec/linked/b.json",
			"spec/linked/b.json",
			"spec/models/b.json",
			"spec/models/b.json",
			"spec/real/b.json",
			"spec/real/b.json",
			"spec/requests/admin/d.json",
			"spec/requests/admin/d.json",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
			"spec/requests/c.json",
		},
		why: "two recursive components DUPLICATE matches in Python; de-duplicating " +
			"would be tidier than the oracle and therefore wrong",
	},
	{
		pattern: "nope/**/*.json",
		want:    nil,
		why: "a missing root is a no-match. NOTE: this case does NOT pin the " +
			"existence guard — it answers empty either way, because the trailing " +
			"`*.json` cannot match inside a directory that does not exist. The two " +
			"cases below are the ones that discriminate",
	},
	// The existence guard in globRecursive (`dir == "" || isDir(dir)`) is what
	// keeps the zero-segment match from being emitted under a path that is not a
	// directory. It is invisible at the CLI — ExpandFiles' isFile filter drops
	// `nope/` anyway — so it is only ever pinned here, and only by a pattern
	// whose LAST component is the recursive one. Delete the guard and both of
	// these turn red; every `nope/**/*.json`-shaped case stays green.
	{
		pattern: "nope/**",
		want:    nil,
		why: "a trailing `**` under a MISSING directory: the zero-segment match is " +
			"emitted only when the directory exists, so this stays empty rather " +
			"than becoming a match on `nope/`",
	},
	{
		pattern: "spec/a.json/**",
		want:    nil,
		why: "the same guard for the other reason isDir can be false — the root " +
			"EXISTS but is a regular file, so `**` has nothing to match, not even " +
			"zero segments",
	},
	{
		pattern: "spec/models/**/*.json",
		want:    []string{"spec/models/b.json"},
		why:     "a leaf directory still gets its zero-segment match",
	},
}

// pyGlobRelative runs PyGlob over an absolute pattern rooted at root and
// returns the sorted matches with the root prefix removed.
func pyGlobRelative(root, pattern string) []string {
	absolute := filepath.Join(root, pattern)
	if strings.HasSuffix(pattern, "/") {
		// filepath.Join eats a trailing separator, which is the whole meaning of
		// a directories-only pattern. Put it back.
		absolute += "/"
	}

	matches := PyGlob(absolute)
	sort.Strings(matches)

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, strings.TrimPrefix(match, root+"/"))
	}
	return out
}

func TestPyGlobRecursive(t *testing.T) {
	root := buildGlobTree(t)
	for _, tc := range globCases {
		got := pyGlobRelative(root, tc.pattern)
		if !equalStrings(got, tc.want) {
			t.Errorf("PyGlob(%q):\n  got  %q\n  want %q\n  (%s)", tc.pattern, got, tc.want, tc.why)
		}
	}
}

// ExpandFiles drops everything that is not a regular file, which is what makes
// `**/` a no-match at the CLI rather than a list of directories reported as
// unreadable files.
func TestExpandFilesRecursive(t *testing.T) {
	root := buildGlobTree(t)

	cases := []struct {
		pattern string
		want    []string
	}{
		{"spec/**/*.json", []string{
			"spec/a.json",
			"spec/linked/b.json",
			"spec/models/b.json",
			"spec/real/b.json",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		}},
		// Directories only -> nothing survives the isfile filter, so the caller
		// emits its "no file(s) match" diagnostic and exits 1.
		{"spec/**/", nil},
		{"spec/**", []string{
			"spec/a.json",
			"spec/linked/b.json",
			"spec/models/b.json",
			"spec/real/b.json",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		}},
	}
	for _, tc := range cases {
		pattern := filepath.Join(root, tc.pattern)
		if strings.HasSuffix(tc.pattern, "/") {
			pattern += "/"
		}
		var got []string
		for _, path := range ExpandFiles(pattern) {
			got = append(got, strings.TrimPrefix(path, root+"/"))
		}
		if !equalStrings(got, tc.want) {
			t.Errorf("ExpandFiles(%q):\n  got  %q\n  want %q", tc.pattern, got, tc.want)
		}
	}
}

// A bare `**` is the one pattern whose zero-segment match has no directory to
// join onto, so it surfaces as a literal "" that iglob drops. It only reads
// that way relative to the working directory, hence the chdir.
func TestPyGlobBareRecursiveDropsTheEmptyMatch(t *testing.T) {
	root := buildGlobTree(t)

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
	}()

	cases := []struct {
		pattern string
		want    []string
	}{
		{"**", []string{
			"spec",
			"spec/a.json",
			"spec/linked",
			"spec/linked/b.json",
			"spec/models",
			"spec/models/b.json",
			"spec/real",
			"spec/real/b.json",
			"spec/requests",
			"spec/requests/admin",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		}},
		{"**/*.json", []string{
			"spec/a.json",
			"spec/linked/b.json",
			"spec/models/b.json",
			"spec/real/b.json",
			"spec/requests/admin/d.json",
			"spec/requests/c.json",
		}},
		{"**/", []string{
			"spec/",
			"spec/linked/",
			"spec/models/",
			"spec/real/",
			"spec/requests/",
			"spec/requests/admin/",
		}},
	}
	for _, tc := range cases {
		got := PyGlob(tc.pattern)
		sort.Strings(got)
		if !equalStrings(got, tc.want) {
			t.Errorf("PyGlob(%q):\n  got  %q\n  want %q", tc.pattern, got, tc.want)
		}
		for _, match := range got {
			if match == "" {
				t.Errorf("PyGlob(%q) returned an empty match; iglob drops it", tc.pattern)
			}
		}
	}
}

// A self-referential symlink must terminate the way CPython's does: by running
// out of the kernel's symlink budget, not by a visited-set guard that would cut
// the walk off sooner than the oracle does. The test completing at all is the
// termination half; the depth assertion is what stops a port that quietly
// refused to follow the link from passing.
func TestPyGlobRecursiveSymlinkLoopTerminates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "loop"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "loop/x.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(".", filepath.Join(root, "loop/self")); err != nil {
		t.Skipf("this filesystem does not support symlinks (%v)", err)
	}

	got := pyGlobRelative(root, "loop/**/*.json")
	if len(got) == 0 {
		t.Fatal("a symlink loop must still yield the file it contains")
	}

	deepest := 0
	for _, match := range got {
		depth := strings.Count(match, "self/")
		if depth > deepest {
			deepest = depth
		}
		if match != "loop/"+strings.Repeat("self/", depth)+"x.json" {
			t.Errorf("unexpected match through the loop: %q", match)
		}
	}
	// The kernel's limit is ~40 links; the exact figure is not the contract, but
	// a port that declined to follow the symlink at all would stop at 0-1.
	if deepest < 5 {
		t.Errorf("deepest match went through %d loop levels — the walk is not following the symlink", deepest)
	}

	// Where the loop bottoms out IS a kernel property, so it is compared with
	// the oracle rather than named by a literal.
	if python, err := exec.LookPath("python3"); err == nil {
		if want := pythonGlob(t, python, root, "loop/**/*.json"); !equalStrings(got, want) {
			t.Errorf("symlink loop: the port bottoms out somewhere Python does not\n  go     %d matches, deepest %d levels\n  python %d matches",
				len(got), deepest, len(want))
		}
	} else {
		t.Log("python3 not on PATH — the loop's depth was not compared with the oracle")
	}
}

// --------------------------------------------------------------------------- #
// the differential half
// --------------------------------------------------------------------------- #

// The literal expectations above are re-derived from the real glob module, so a
// mistake shared between the port and the table is caught rather than pinned.
func TestPyGlobRecursiveMatchesPythonGlob(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — the differential oracle is unavailable, so this test verified nothing")
	}

	root := buildGlobTree(t)

	patterns := make([]string, 0, len(globCases)+6)
	for _, tc := range globCases {
		patterns = append(patterns, tc.pattern)
	}
	patterns = append(patterns,
		"spec/**/b.json",       // a literal basename under a recursive component
		"spec/**/[ab].json",    // a character class under one
		"spec/*/**/*.json",     // a wildcard directory above one
		"spec/.**/*.json",      // a hidden-looking NON-component `**`
		"spec/**/admin/*.json", // a literal component below one
		"spec/**/*",            // no extension: directories survive PyGlob
	)

	for _, pattern := range patterns {
		want := pythonGlob(t, python, root, pattern)
		got := pyGlobRelative(root, pattern)
		if !equalStrings(got, want) {
			t.Errorf("PyGlob(%q) disagrees with glob.glob(recursive=True):\n  go     %q\n  python %q",
				pattern, got, want)
		}
	}
}

// pythonGlob asks the oracle for sorted(glob.glob(pattern, recursive=True)),
// run from root so the results come back relative to it.
func pythonGlob(t *testing.T, python, root, pattern string) []string {
	t.Helper()
	const script = `
import glob, sys
sys.stdout.write("\x00".join(sorted(glob.glob(sys.argv[1], recursive=True))))
`
	cmd := exec.Command(python, "-c", script, pattern)
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("python glob oracle failed for %q: %v", pattern, err)
	}
	if len(out) == 0 {
		return nil
	}
	return strings.Split(string(out), "\x00")
}

func equalStrings(a, b []string) bool {
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

package main

// Coverage for the matcher's DIRECTORY WALK.
//
// port_test.go covers matchName, which decides one name against one pattern.
// Everything here is about the walk around it, and the two are worth separating
// because a matcher that is right about names can still be wrong about which
// names it is ever shown — and that failure is silent. A `**` that quietly
// skips a directory reports a clean pass over a smaller set of files, which
// looks exactly like a clean pass over all of them.
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

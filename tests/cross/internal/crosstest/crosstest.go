// Package crosstest holds test helpers shared by the tests/cross tools.
//
// It is a separate package from report, and deliberately so: this file imports
// "testing", and a normal (non-_test.go) file's imports are linked into every
// binary that imports the package. Keeping the helper here rather than in
// report means the shipped inspect-artifact and sha256sums binaries never pull
// the testing package in. It is a normal file rather than a _test.go one
// because Go does not share _test.go files across packages, and the two tools'
// tests are `package main` in different directories.
package crosstest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RequireProblem fails unless some problem contains want.
//
// Requiring the SPECIFIC diagnostic rather than merely "some problem" is the
// point: several distinct defects trip more than one assertion at once, so a
// test that only counted problems would still pass with the assertion it was
// written to protect deleted.
func RequireProblem(t *testing.T, problems []string, want string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem mentioned %q; got %#v", want, problems)
}

// RepoRoot is three levels up from the calling test's package directory, which
// is where `go test` runs it from.
//
// Every tests/cross tool package sits at tests/cross/<tool>, so the same
// resolution serves all of them. It is resolved from the CALLER's working
// directory, not from this file's location — this package sitting one level
// deeper than its callers does not enter into it.
func RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("could not resolve the repository root: %v", err)
	}
	return root
}

// SetDiff reports what is in want but not got, and what is in got but not want.
//
// It lives here rather than beside either caller for the same reason the
// agreement checks built on it exist at all: it was written out once per
// tests/cross tool, and the two copies had already drifted cosmetically before
// the second one was a day old. A helper duplicated across packages with
// nothing executing the agreement is exactly the shape those checks police.
func SetDiff(want, got []string) (missing, extra []string) {
	index := func(items []string) map[string]bool {
		m := make(map[string]bool, len(items))
		for _, item := range items {
			m[item] = true
		}
		return m
	}
	wantSet, gotSet := index(want), index(got)
	for _, item := range want {
		if !gotSet[item] {
			missing = append(missing, item)
		}
	}
	for _, item := range got {
		if !wantSet[item] {
			extra = append(extra, item)
		}
	}
	return missing, extra
}

// ShellTargets reads the `TARGETS=( ... )` block out of a shell script, given a
// path relative to the repository root, and returns the "goos/goarch" entries
// it names.
//
// It is ShellList pinned to the TARGETS block name; the fatal-not-skip contract
// described there is what makes the agreement checks built on it able to fail at
// all.
func ShellTargets(t *testing.T, rel string) []string {
	t.Helper()
	return ShellList(t, rel, "TARGETS")
}

// ShellList reads a `<name>=( ... )` array block out of a shell script, given a
// path relative to the repository root, and returns the double-quoted entries it
// names, in the order the script writes them.
//
// Every failure here is a t.Fatal rather than a skip or an empty result, because
// the whole value of the agreement checks built on it is that they FAIL when the
// lists diverge — and a parser that quietly returned nothing would make every
// empty list agree perfectly while the scripts disagreed. That applies to every
// way this can go wrong: the file cannot be read, the block is missing, there is
// more than one of it, it is empty, an entry is not a double-quoted value, or it
// is never closed. A "no block found" that skipped would read as agreement to
// anyone glancing at the run.
//
// Comments and blank lines inside the block are skipped rather than parsed, so a
// list may be annotated per entry without the annotation becoming an entry.
func ShellList(t *testing.T, rel, blockName string) []string {
	t.Helper()
	path := filepath.Join(RepoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}

	opener := blockName + "=("
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != opener {
			continue
		}
		if start != -1 {
			t.Fatalf("%s holds more than one `%s` block, so which one this test compares is a guess", rel, opener)
		}
		start = i
	}
	if start == -1 {
		t.Fatalf("%s holds no `%s` block. This test can no longer see the list it compares, which is not the same as the lists agreeing — re-point it rather than deleting it", rel, opener)
	}

	var entries []string
	for _, line := range lines[start+1:] {
		entry := strings.TrimSpace(line)
		if entry == ")" {
			if len(entries) == 0 {
				t.Fatalf("%s: the %s block is empty", rel, blockName)
			}
			return entries
		}
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if len(entry) < 3 || !strings.HasPrefix(entry, `"`) || !strings.HasSuffix(entry, `"`) {
			t.Fatalf("%s: cannot read %q as an entry of the %s block; this test parses one double-quoted value per line", rel, entry, blockName)
		}
		entries = append(entries, strings.Trim(entry, `"`))
	}
	t.Fatalf("%s: the `%s` block is never closed", rel, opener)
	return nil
}

// ShellScalar reads a `<name>="value"` assignment out of a shell script, given a
// path relative to the repository root, and returns the value.
//
// The sibling of ShellList for the lists that are one element long, and it
// carries the same fatal-not-skip contract for the same reason: a scalar this
// parser could not find would otherwise compare equal to another it also could
// not find. Only the double-quoted form is accepted — the scripts write it that
// way, and guessing at unquoted or single-quoted spellings would mean guessing
// at where the value ends.
func ShellScalar(t *testing.T, rel, name string) string {
	t.Helper()
	path := filepath.Join(RepoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}

	prefix := name + `="`
	value := ""
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		assignment := strings.TrimSpace(line)
		if !strings.HasPrefix(assignment, prefix) || !strings.HasSuffix(assignment, `"`) {
			continue
		}
		if found {
			t.Fatalf("%s assigns `%s` more than once, so which one this test compares is a guess", rel, name)
		}
		value, found = strings.TrimSuffix(strings.TrimPrefix(assignment, prefix), `"`), true
	}
	if !found {
		t.Fatalf("%s holds no `%s=\"...\"` assignment. This test can no longer see the value it compares, which is not the same as the values agreeing — re-point it rather than deleting it", rel, name)
	}
	if value == "" {
		t.Fatalf("%s: `%s` is assigned the empty string", rel, name)
	}
	return value
}

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

// ShellTargets reads the `TARGETS=( ... )` block out of a shell script, given a
// path relative to the repository root, and returns the "goos/goarch" entries
// it names.
//
// Every failure here is a t.Fatal rather than a skip or an empty result, because
// the whole value of the agreement checks built on it is that they FAIL when the
// lists diverge — and a parser that quietly returned nothing would make every
// empty list agree perfectly while the scripts disagreed.
func ShellTargets(t *testing.T, rel string) []string {
	t.Helper()
	path := filepath.Join(RepoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}

	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "TARGETS=(" {
			continue
		}
		if start != -1 {
			t.Fatalf("%s holds more than one `TARGETS=(` block, so which one describes the release is a guess", rel)
		}
		start = i
	}
	if start == -1 {
		t.Fatalf("%s holds no `TARGETS=(` block. This test can no longer see the list it compares, which is not the same as the lists agreeing — re-point it rather than deleting it", rel)
	}

	var targets []string
	for _, line := range lines[start+1:] {
		entry := strings.TrimSpace(line)
		if entry == ")" {
			if len(targets) == 0 {
				t.Fatalf("%s: the TARGETS block is empty", rel)
			}
			return targets
		}
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if len(entry) < 3 || !strings.HasPrefix(entry, `"`) || !strings.HasSuffix(entry, `"`) {
			t.Fatalf("%s: cannot read %q as a target entry; this test parses one quoted \"goos/goarch\" per line", rel, entry)
		}
		targets = append(targets, strings.Trim(entry, `"`))
	}
	t.Fatalf("%s: the `TARGETS=(` block is never closed", rel)
	return nil
}

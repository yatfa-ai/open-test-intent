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

package main

// SPGD-1576: an unknown flag in a pattern position is a usage error.
//
// SPGD-1398 closed the three spellings of this binary's own mode selectors. The
// wider class — `--verbose`, `--sourc`, `-x`, `-v`, a bare `--` — still
// answered exit 1 with a phantom no-match naming the flag (and, under --json, a
// whole document carrying a findings[] row whose `file` was the flag). These
// tests pin the existence-keyed refusal (exit 2, usage on stderr, stdout EMPTY)
// and every input it must leave alone.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownFlagInAPatternPositionIsAUsageError(t *testing.T) {
	cases := [][]string{
		{"--source", "a", "--verbose"},
		{"--source", "a", "-x"},
		{"--source", "a", "-v"},
		{"--source", "a", "--"}, // a bare end-of-options: no such grammar exists
		{"--source", "a", "-sA"},
		{"--sourc", "a"},      // adopter mode, position 0
		{"--source=A", "b"},   // attached form is not a flag spelling
		{"-x", "a/s_spec.rb"}, // adopter mode, position 0
		{"--json", "--source", "a", "--verbose"},
		{"--source", "a", "--verbose", "--json"},
		{"--json", "--sourc", "a"},
		{"--source", "--verbose"}, // first pattern, not only later ones
		// SETTLED DECISION (SPGD-1576 decision 3), not a regression: a
		// NONEXISTENT dash-prefixed literal filename used to exit 1 with a
		// no-match and now exits 2. The binary cannot tell a typo'd flag from a
		// typo'd dash-named file; the flag reading is the likelier one; both
		// codes fail CI, so only the diagnostic improves.
		{"--source", "-missing_spec.rb"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			workTree(t)
			code, stdout, stderr := captureRun(t, argv...)
			if code != 2 {
				t.Fatalf("run(%q) = %d, want 2 (a usage error, not a content verdict)", argv, code)
			}
			// Empty stdout forbids PASS lines and the --json document alike.
			if stdout != "" {
				t.Errorf("run(%q) wrote to stdout: %q — a refusal must not", argv, stdout)
			}
			if !strings.Contains(stderr, usage) {
				t.Errorf("run(%q) did not print the shared usage block: %q", argv, stderr)
			}
			if !strings.Contains(stderr, "is not a flag of this command") ||
				!strings.Contains(stderr, "no file or directory with that name exists") {
				t.Errorf("run(%q) did not name both readings: %q", argv, stderr)
			}
			if strings.Contains(stderr, "no file(s) match") {
				t.Errorf("run(%q) still reports a false no-match: %q", argv, stderr)
			}
			if strings.Contains(stderr, "could not read/parse JSON") {
				t.Errorf("run(%q) fabricated a parse-JSON failure: %q", argv, stderr)
			}
			if strings.Contains(stderr, helpTrailer) {
				t.Errorf("run(%q) printed the --help trailer on a refusal", argv)
			}
		})
	}
}

// The --sourc row, pinned on its own: it used to fall through to adopter mode
// and FAIL the valid file as JSON. The absence of that line is the point.
func TestMistypedSourceFlagDoesNotFallThroughToAdopterMode(t *testing.T) {
	workTree(t)
	code, stdout, stderr := captureRun(t, "--sourc", "a")
	if code != 2 || stdout != "" {
		t.Fatalf("run = %d, stdout %q; want 2 and empty stdout", code, stdout)
	}
	for _, fabricated := range []string{"FAIL", "could not read", "parse JSON", "no file(s) match"} {
		if strings.Contains(stderr, fabricated) {
			t.Errorf("stderr carries the fabricated %q: %q", fabricated, stderr)
		}
	}
	if !strings.Contains(stderr, "'--sourc'") {
		t.Errorf("stderr does not name the offending token: %q", stderr)
	}
}

// The 1398 gate keeps precedence: an argv with both a misplaced own selector and
// an unknown flag reports the own selector.
func TestOwnSelectorOutranksUnknownFlag(t *testing.T) {
	workTree(t)
	code, _, stderr := captureRun(t, "--source", "a", "--verbose", "--source")
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "'--source' is a flag of this command, not a file pattern") {
		t.Errorf("expected the own-selector message, got %q", stderr)
	}
}

// Decision 2's falsifier table: real dash-named files keep validating. The
// refusal is keyed on EXISTENCE, so none of these may be touched.
func TestRealDashNamedFilesStillValidateUnderTheUnknownFlagGate(t *testing.T) {
	cases := [][]string{
		{"--source", "-dash_spec.rb"},
		{"--source", "./-dash_spec.rb"},
		{"--source", "--weird_spec.rb"},
		{"--source", "./--weird_spec.rb"},
		{"--source", "-*_spec.rb"}, // flag-shaped glob over real files
		{"--source", "--*_spec.rb"},
		{"--source", "a", "./--source"}, // a file literally named --source
		{"--source", "-dashdir"},        // a dash-named directory, descended
		{"--source", "./-dashdir"},
		{"-dash_spec.rb"}, // adopter mode: position 0 exists, so it is a file
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			workTree(t)
			// workTree has already chdir'd, so copy an existing valid file.
			body, err := os.ReadFile("-dash_spec.rb")
			if err != nil {
				t.Fatal(err)
			}
			for _, rel := range []string{"--weird_spec.rb", filepath.Join("-dashdir", "d_spec.rb")} {
				if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(rel, body, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			code, stdout, stderr := captureRun(t, argv...)
			if argv[0] == "-dash_spec.rb" {
				// Adopter mode reads the spec file as JSON: a content verdict
				// (exit 1), never the usage refusal.
				if code == 2 {
					t.Fatalf("run(%q) was refused as a flag: %s", argv, stderr)
				}
				return
			}
			if code != 0 {
				t.Fatalf("run(%q) = %d, want 0\nstdout: %s\nstderr: %s", argv, code, stdout, stderr)
			}
			if stdout == "" {
				t.Errorf("run(%q) produced no output on stdout", argv)
			}
		})
	}
}

// A dash-named SYMLINK (even a dangling one) exists to lexists, so it is a file
// argument, not a flag.
func TestDanglingDashSymlinkIsNotAnUnknownFlag(t *testing.T) {
	workTree(t)
	if err := os.Symlink("nowhere", "-dangling"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if tok, bad := unknownFlagInPatterns([]string{"-dangling"}); bad {
		t.Errorf("a dangling symlink was refused as an unknown flag (%q)", tok)
	}
}

// Stdin mode reads no patterns: its extra arguments are ignored and stay so.
func TestStdinModeIsExemptFromTheUnknownFlagGate(t *testing.T) {
	var code int
	var stderr string
	withStdin(t, nil, func() {
		code, _, stderr = captureRun(t, "-", "--verbose")
	})
	if code == 2 && strings.Contains(stderr, "is not a flag of this command") {
		t.Errorf("stdin mode was refused by the unknown-flag gate: %q", stderr)
	}
}

// Predicate-level pins: each of the three conjuncts is load-bearing, so each
// gets a case only it can discriminate.
func TestUnknownFlagPredicateConjuncts(t *testing.T) {
	workTree(t)
	refused := []string{"--verbose", "-x", "--", "-sA", "--source=A", "-missing"}
	for _, tok := range refused {
		if got, bad := unknownFlagInPatterns([]string{"a", tok}); !bad || got != tok {
			t.Errorf("%q was not refused", tok)
		}
	}
	notRefused := []string{
		"a",          // no leading dash
		"",           // empty
		"-*.nomatch", // magic: content semantics stay (matches nothing, exit 1)
		"-?x",
		"-[x]",
		"-dash_spec.rb", // exists
		"./-nothere",    // not dash-led as typed
	}
	for _, tok := range notRefused {
		if got, bad := unknownFlagInPatterns([]string{tok}); bad {
			t.Errorf("%q was refused as %q", tok, got)
		}
	}
}

// A magic glob over nothing keeps exit 1 and the generic no-match sentence: the
// refusal must not widen to wildcards.
func TestDashGlobMatchingNothingStillExitsOne(t *testing.T) {
	workTree(t)
	code, _, stderr := captureRun(t, "--source", "-nomatch*")
	if code != 1 || !strings.Contains(stderr, "no file(s) match '-nomatch*'") {
		t.Errorf("code = %d, stderr %q; want exit 1 and the generic no-match", code, stderr)
	}
}

// The short-circuits answer in any position, before the gate.
func TestShortCircuitsStillWinOverUnknownFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"--verbose", "--version"},
		{"--version", "--verbose"},
		{"--verbose", "--help"},
		{"-x", "-h"},
		{"--verbose", "--schema-source"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			workTree(t)
			code, _, stderr := captureRun(t, argv...)
			if code == 2 && strings.Contains(stderr, "is not a flag of this command") {
				t.Errorf("run(%q) was refused by the gate: %q", argv, stderr)
			}
		})
	}
}

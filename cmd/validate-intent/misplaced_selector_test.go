package main

// SPGD-1398: a mode selector written in a pattern position is a usage error.
//
// Before the gate, `validate-intent --source A --source B` over two clean trees
// globbed the second `--source` as a filename, reported a false
// `no file(s) match '--source'` and exited 1 — the code reserved for "an
// annotation is invalid" — and under --json minted a findings[] row whose `file`
// was a flag name. These tests pin the refusal (exit 2, usage on stderr, stdout
// EMPTY) AND the inputs it must not touch: files whose names merely begin with a
// dash, and the documented invocations.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpecFile copies the shipped valid fixture to path. It must run before the
// test chdirs, because the fixture is located relatively.
func writeSpecFile(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "examples", "sources", "order_spec.rb"))
	if err != nil {
		t.Fatalf("reading the shipped valid fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// workTree builds a cwd holding two clean trees (a, b) plus files whose names
// begin with a dash, then chdirs into it for the test.
func workTree(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{
		"a/s_spec.rb", "b/s_spec.rb", "-dash_spec.rb", "--source", "plain/p_spec.rb",
	} {
		writeSpecFile(t, filepath.Join(root, rel))
	}
	t.Chdir(root)
}

func TestMisplacedModeSelectorIsAUsageError(t *testing.T) {
	cases := [][]string{
		{"--source", "a", "--source", "b"},
		{"-s", "a", "-s", "b"},
		{"--source", "a", "--source"}, // trailing bare flag
		{"--source", "a", "-s"},       // the alias as a later pattern
		{"--source", "a", "-"},        // the stdin marker as a later pattern
		{"a", "--source"},             // adopter mode, selector as a later pattern
		{"--source", "a", "--source", "b", "--json"},
		{"--json", "--source", "a", "--source", "b"},
		{"--source", "a", "--source", "b", "--version-not-a-flag-just-a-path"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			workTree(t)
			code, stdout, stderr := captureRun(t, argv...)
			if code != 2 {
				t.Fatalf("run(%q) = %d, want 2 (a usage error, not a content verdict)", argv, code)
			}
			// Empty stdout is what forbids both a PASS line and a --json
			// document carrying a phantom findings[] row.
			if stdout != "" {
				t.Errorf("run(%q) wrote to stdout: %q — a refusal must not", argv, stdout)
			}
			if !strings.Contains(stderr, usage) {
				t.Errorf("run(%q) did not print the shared usage block: %q", argv, stderr)
			}
			if strings.Contains(stderr, "no file(s) match") {
				t.Errorf("run(%q) still reports a false no-match: %q", argv, stderr)
			}
			if strings.Contains(stderr, helpTrailer) {
				t.Errorf("run(%q) printed the --help trailer on a refusal", argv)
			}
		})
	}
}

// The falsifier for any leading-dash heuristic, and the raw-token keying
// decision: only an EXACT bare spelling is refused.
func TestDashLedFilenamesStillValidate(t *testing.T) {
	cases := [][]string{
		{"--source", "./-dash_spec.rb"},
		{"--source", "-dash_spec.rb"},
		{"--source", "-*_spec.rb"},       // flag-shaped glob matching a real file
		{"--source", "a", "./--source"},  // a file literally named --source, qualified
		{"--source", "a", "b", "plain"},  // the documented multi-path form
		{"-s", "a", "b"},                 // the alias
		{"--json", "--source", "a", "b"}, // --json anywhere
		{"--source", "a", "--json", "b"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			workTree(t)
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Fatalf("run(%q) = %d, want 0\nstdout: %s\nstderr: %s", argv, code, stdout, stderr)
			}
			if stdout == "" {
				t.Errorf("run(%q) produced no output on stdout", argv)
			}
		})
	}
}

// Stdin mode reads no patterns; its handling of extra arguments is unchanged.
func TestStdinModeIsExemptFromTheSelectorGate(t *testing.T) {
	var code int
	var stderr string
	withStdin(t, nil, func() {
		code, _, stderr = captureRun(t, "-", "-")
	})
	if code == 2 && strings.Contains(stderr, "is a flag of this command") {
		t.Errorf("stdin mode was refused by the selector gate: %q", stderr)
	}
}

// The gate is the closed set, compared on the raw token — pin the predicate
// directly so a widening to a prefix rule fails here, not only through run().
func TestMisplacedModeSelectorIsAClosedSet(t *testing.T) {
	for _, bad := range []string{"--source", "-s", "-"} {
		if tok, ok := misplacedModeSelector([]string{"x", bad}); !ok || tok != bad {
			t.Errorf("%q in a pattern position was not refused", bad)
		}
	}
	// Not in the set: dash-led names, qualified spellings, and the unknown-flag
	// class (unknownFlagInPatterns, unknown_flag_test.go), none of which this
	// gate may claim.
	for _, ok := range []string{
		"./--source", "-dash_spec.rb", "--sourc", "--verbose", "-x", "--", "a.json", "",
	} {
		if tok, bad := misplacedModeSelector([]string{"--source", ok}); bad {
			t.Errorf("%q was refused as %q; the gate is exact-match on three spellings", ok, tok)
		}
	}
	// positional[0] is the selector's own slot and is never inspected.
	if _, bad := misplacedModeSelector([]string{"--source"}); bad {
		t.Error("positional[0] was inspected")
	}
}

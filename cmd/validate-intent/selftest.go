package main

// Self-test mode — the port of `run_self_test` and `_self_test_source_fixture`
// (bin/validate-intent:585-710).
//
// This is the in-repo fixture harness, not an adopter surface: it loads the
// shipped schema and checks every example fixture against its EXPECTED outcome
// (every examples/*.json must validate, every examples/invalid/*.json must be
// rejected, and the same both ways for the in-source fixtures). It is what
// `validate-intent` with no arguments does.

import (
	"fmt"
	"os"
	"path/filepath"
)

// RepoRoot is the reference's REPO_ROOT (bin/validate-intent:76): the parent of
// the directory holding the executable. Deriving it from os.Executable() the way
// SchemaPath does is what makes a Go binary built into bin/ resolve the same
// fixture globs the Python script does — and therefore what lets the self-test
// output be compared byte for byte.
func RepoRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// selfTestSourceFixture is the port of `_self_test_source_fixture`
// (bin/validate-intent:585-618): check one source fixture end-to-end
// (extraction -> normalization -> validation).
//
// Returns true when the fixture matched its expectation. A fixture with ZERO
// extractable annotations is a mismatch either way: that is what a
// silently-broken extractor looks like, and it must not read as green.
func selfTestSourceFixture(path, root string, schema *Schema, expectValid bool) bool {
	rel := relTo(root, path)
	findings, readError := CheckSourceFile(path, schema)
	if readError != "" {
		fmt.Printf("FAIL  %s — %s\n", rel, readError)
		return false
	}
	if len(findings) == 0 {
		fmt.Printf("FAIL  %s — no @intent annotations extracted\n", rel)
		return false
	}

	matched := true
	for _, finding := range findings {
		where := fmt.Sprintf("%s:%d", rel, finding.Line)
		if finding.Valid == expectValid {
			if expectValid {
				fmt.Printf("PASS  %s\n", where)
				continue
			}
			// The reference's `problem or (errors[0] if errors else "invalid")`.
			detail := finding.Problem
			if detail == "" {
				if len(finding.Errors) > 0 {
					detail = finding.Errors[0]
				} else {
					detail = "invalid"
				}
			}
			fmt.Printf("PASS  %s (correctly rejected — %s)\n", where, detail)
			continue
		}
		matched = false
		if expectValid {
			suffix := ""
			if finding.Problem != "" {
				suffix = " (" + finding.Problem + ")"
			}
			fmt.Printf("FAIL  %s — unexpectedly invalid%s\n", where, suffix)
			for _, err := range finding.Errors {
				fmt.Printf("        -> %s\n", err)
			}
		} else {
			fmt.Printf("FAIL  %s — unexpectedly valid\n", where)
		}
	}
	return matched
}

// RunSelfTest is the port of `run_self_test` (bin/validate-intent:621-710).
//
// On the arithmetic in the final line: a run over the shipped corpus prints 24
// PASS lines and then "12/12 fixtures matched expectation." Those numbers count
// DIFFERENT THINGS and are supposed to disagree — `checked` counts FIXTURES
// (8 JSON files + 4 source files), while the source fixtures print one line per
// ANNOTATION, of which there are more. Do not "fix" it; reproduce it.
func RunSelfTest(schema *Schema) int {
	root, err := RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not locate the repo root: %s\n", err)
		return 2
	}

	validExamplesGlob := filepath.Join(root, "examples", "*.json")
	invalidExamplesGlob := filepath.Join(root, "examples", "invalid", "*.json")
	validSourcesGlob := filepath.Join(root, "examples", "sources", "*")
	invalidSourcesGlob := filepath.Join(root, "examples", "sources", "invalid", "*")

	validFiles := ExpandFiles(validExamplesGlob)
	invalidFiles := ExpandFiles(invalidExamplesGlob)
	invalidSources := ExpandFiles(invalidSourcesGlob)
	// `sources/*` yields the invalid/ directory entry, not its contents, and
	// ExpandFiles drops directories — so the two sets are already disjoint. The
	// subtraction keeps them disjoint if the glob is ever widened to `**`.
	validSources := []string{}
	for _, path := range ExpandFiles(validSourcesGlob) {
		if !contains(invalidSources, path) {
			validSources = append(validSources, path)
		}
	}

	// A fixture set that matched NOTHING is a failure, not a vacuous pass.
	// Dropping examples/invalid/ alone would turn 12/12 into a *greener*-reading
	// 8/8, exit 0, with the validator's ability to reject a bad annotation now
	// wholly unexercised. Each set is guarded independently — a `checked == 0`
	// check would sail straight past that case, which is both the likeliest
	// shape and the most misleading one. Every empty set is reported, so one run
	// names everything missing, and the run fails BEFORE checking anything: an
	// incomplete fixture inventory invalidates the whole run, so there is no
	// conformance verdict left to report.
	type fixtureSet struct {
		pattern  string
		verifies string
		files    []string
	}
	empty := []fixtureSet{}
	for _, set := range []fixtureSet{
		{validExamplesGlob, "acceptance", validFiles},
		{invalidExamplesGlob, "rejection", invalidFiles},
		{validSourcesGlob, "in-source acceptance", validSources},
		{invalidSourcesGlob, "in-source rejection", invalidSources},
	} {
		if len(set.files) == 0 {
			empty = append(empty, set)
		}
	}
	if len(empty) > 0 {
		for _, set := range empty {
			fmt.Fprintf(os.Stderr,
				"error: no fixtures match %s — self-test cannot verify %s\n",
				PyReprString(relTo(root, set.pattern)), set.verifies)
		}
		return 1
	}

	mismatches := 0
	checked := 0

	// expected-valid fixtures
	for _, path := range validFiles {
		checked++
		rel := relTo(root, path)
		valid, errs, parseError, _ := CheckFile(path, schema)
		switch {
		case parseError != "":
			fmt.Printf("FAIL  %s — unexpectedly invalid (%s)\n", rel, parseError)
			mismatches++
		case valid:
			fmt.Printf("PASS  %s\n", rel)
		default:
			fmt.Printf("FAIL  %s — unexpectedly invalid\n", rel)
			for _, err := range errs {
				fmt.Printf("        -> %s\n", err)
			}
			mismatches++
		}
	}

	// expected-invalid fixtures
	for _, path := range invalidFiles {
		checked++
		rel := relTo(root, path)
		valid, _, parseError, _ := CheckFile(path, schema)
		switch {
		case parseError != "":
			// Malformed JSON is a rejection too — but flag it so it's visible.
			fmt.Printf("PASS  %s (correctly rejected — %s)\n", rel, parseError)
		case valid:
			fmt.Printf("FAIL  %s — unexpectedly valid\n", rel)
			mismatches++
		default:
			fmt.Printf("PASS  %s (correctly invalid)\n", rel)
		}
	}

	// in-source fixtures — extraction + normalization + validation end-to-end
	for _, path := range validSources {
		checked++
		if !selfTestSourceFixture(path, root, schema, true) {
			mismatches++
		}
	}
	for _, path := range invalidSources {
		checked++
		if !selfTestSourceFixture(path, root, schema, false) {
			mismatches++
		}
	}

	fmt.Printf("\n%d/%d fixtures matched expectation.\n", checked-mismatches, checked)
	if mismatches == 0 {
		return 0
	}
	return 1
}

// relTo is os.path.relpath(path, root).
//
// It falls back to the absolute path when no relative one exists (a different
// volume, say), which is what os.path.relpath would raise on — a fallback rather
// than a crash, because a self-test that aborts here has reported nothing at all
// about the fixtures.
func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func contains(haystack []string, needle string) bool {
	for _, entry := range haystack {
		if entry == needle {
			return true
		}
	}
	return false
}

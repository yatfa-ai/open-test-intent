package main

// Self-test mode.
//
// This is the in-repo fixture harness, not an adopter surface: it loads the
// shipped schema and checks every example fixture against its EXPECTED outcome
// (every examples/*.json must validate, every examples/invalid/*.json must be
// rejected, and the same both ways for the in-source fixtures). It is what
// `validate-intent` with no arguments does.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// The four fixture sets, spelled as the "no fixtures match ..." diagnostic
// prints them.
//
// Held slash-separated and relative to the repo root, because that is the one
// spelling both consumers can use: the on-disk expansion joins the root onto
// them, the embedded expansion matches them against a corpus whose separator is
// always "/", and the diagnostic quotes them verbatim.
//
// Together they ARE the conformance instrument. Every examples/*.json must
// accept, every examples/invalid/*.json must reject, and the same both ways
// in-source — PROTOCOL.md and schemas/open-test-intent.v1.json say what valid
// means, and this is where the binary is held to it.
const (
	validExamplesGlob   = "examples/*.json"
	invalidExamplesGlob = "examples/invalid/*.json"
	validSourcesGlob    = "examples/sources/*"
	invalidSourcesGlob  = "examples/sources/invalid/*"
)

// RepoRoot is the parent of the directory holding the executable. Deriving it
// from os.Executable() the way SchemaPath does is what makes a binary built into
// bin/ find the fixture tree beside it with no configuration.
func RepoRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// --------------------------------------------------------------------------- //
// where a self-test's fixtures come from
// --------------------------------------------------------------------------- //

// fixtureSource is the corpus one self-test run reads: either the examples/
// tree beside the executable, or the copy compiled into the binary.
//
// # Why there is a fallback at all
//
// RepoRoot is executable-relative, which is right in a checkout and wrong for a
// distributable binary. Installed at /usr/local/bin/validate-intent it resolved
// /usr/local/examples/*.json,
// found nothing, and exited 1 with four errors — while the schema, since
// SPGD-131, loaded perfectly well. This is the other half of that same defect:
// --help's first line advertises the bare invocation as the thing to run, and
// on the host an adopter actually installs to, --help is the entire
// documentation set.
//
// # Why disk still wins
//
// The same reason LoadSchema lets it (fileio.go): selftest_embed_test.go plants
// synthetic corpora and expects the binary to read what is in them. A pure embed
// would ignore every one of those while the tests reported green. In a checkout
// the self-test therefore grades the binary against the fixtures a reviewer can
// see and edit.
//
// # ⭐ Why the decision is per TREE and never per GLOB
//
// This is the load-bearing design point, and the tempting alternative is the
// wrong one. A per-glob fallback — "use the embedded copy for whichever of the
// four sets is empty" — would take a checkout whose examples/invalid/ someone
// deleted and quietly heal it from the binary. The run would go green having
// tested the deleted coverage against a copy of itself, which turns SPGD-56's
// empty-fixture guard into a no-op: coverage deleted without a single case
// going red, this project's house defect wearing a new hat.
//
// So ONE decision is taken, once, before anything is expanded: is there an
// examples/ tree here? If there is, every glob resolves against it and an
// incomplete one still fails loudly. If there is not, all four come from the
// binary. There is no mixed state.
type fixtureSource struct {
	// root is the repo root fixture paths are named relative to. It is joined
	// onto a pattern for the on-disk expansion, and — either way — it is what
	// the printed paths are relative TO, which is why an embedded run prints
	// the same "examples/unit-order-total.json" a checkout does.
	root string

	// corpus is non-nil only when the embedded copy won, so it doubles as the
	// record of which way the one decision went.
	corpus embeddedCorpus
}

// embeddedCorpus is the compiled-in corpus, narrowed to the two operations the
// self-test performs on it. opentestintent.ExamplesFS() satisfies it; so does a
// stand-in, which is what lets selftest_embed_test.go stage the one case that
// cannot be staged from outside the process — an embedded corpus that matches
// nothing — without shipping a broken binary to do it.
//
// Two methods and not fs.FS: see ExamplesFS's own comment for the size argument,
// and expandEmbedded below for why fs.Glob would have been the wrong matcher
// here even if it were free.
type embeddedCorpus interface {
	ReadDir(name string) ([]fs.DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// newFixtureSource takes the per-tree decision described above.
//
// The absent/present rule is LoadSchema's, deliberately: a tree that is THERE
// is a deliberate statement about which fixtures this run should use, and
// substituting past a broken one would answer the user's own mistake with a
// confident green report. Only an absent tree is an absence of intent.
//
// So anything other than "not there" — a permissions failure, a plain file
// sitting on the name — keeps the on-disk path, where it produces exactly the
// loud "no fixtures match ..." failure it produces today. This is the branch
// where falling back would be most convenient and least honest.
func newFixtureSource(root string) fixtureSource {
	if _, err := os.Stat(filepath.Join(root, "examples")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fixtureSource{root: root, corpus: opentestintent.ExamplesFS()}
		}
	}
	return fixtureSource{root: root}
}

// expand returns the fixtures matching one of the four patterns, as paths
// relative to root.
//
// It returns RELATIVE paths in both modes because those are what gets printed,
// and because the embedded corpus has no absolute paths to offer. On disk the
// sort still happens over absolute paths inside ExpandFiles; every entry shares
// the `root + separator` prefix, so relativising afterwards cannot reorder them.
func (s fixtureSource) expand(pattern string) []string {
	if s.corpus != nil {
		return s.expandEmbedded(pattern)
	}
	matches := ExpandFiles(filepath.Join(s.root, pattern))
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		files = append(files, relTo(s.root, match))
	}
	return files
}

// expandEmbedded is ExpandFiles over the compiled-in corpus.
//
// It is built out of THIS package's matcher rather than the standard library's,
// and that is the whole point rather than an optimisation. The self-test's
// stdout is a function of the expansion, not of the file set: two corpora
// holding identical bytes but expanded by matchers that disagree still print
// different output, which is the one thing the fallback promises not to do.
// fs.Glob would have been a second matcher to keep in agreement with glob.go
// forever — its `*` matches dotfiles and this one's does not, to name only the
// difference that bites first. Reusing matchName, splitPath and joinPath means
// there is nothing to keep in agreement.
//
// What it reproduces, and where each rule comes from:
//
//   - globInDir's dotfile rule — a pattern not itself starting with `.` never
//     matches one. The embed carries dotfiles (`all:` is what makes it a mirror
//     of the tree), so this is the layer that has to drop them, exactly as on
//     disk.
//   - ExpandFiles' isFile filter, so `examples/sources/*` yields the four
//     source files and not the `invalid` directory entry.
//   - ExpandFiles' sort, over the same names.
//
// Only the ONE-directory shapes the four patterns use are handled — no `**`,
// no magic in the directory part. A pattern needing more would silently match
// nothing here while working on disk, which is why the four are compile-time
// constants at the top of this file and nothing builds one at run time.
func (s fixtureSource) expandEmbedded(pattern string) []string {
	dir, base := splitPath(pattern)
	entries, err := s.corpus.ReadDir(dir)
	if err != nil {
		// A directory the corpus does not have. Nothing matched is the honest
		// answer, and the empty-fixture guard below turns it into a loud
		// failure rather than a quiet 8/8.
		return nil
	}
	allowHidden := isHidden(base)
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !allowHidden && isHidden(name) {
			continue
		}
		if !matchName(name, base) {
			continue
		}
		files = append(files, joinPath(dir, name))
	}
	sort.Strings(files)
	return files
}

// checkJSON is CheckFile addressed by a path relative to root.
func (s fixtureSource) checkJSON(rel string, schema *Schema) (valid bool, errs []string, parseError string) {
	if s.corpus == nil {
		valid, errs, parseError, _ = CheckFile(filepath.Join(s.root, rel), schema)
		return valid, errs, parseError
	}
	data, err := s.corpus.ReadFile(rel)
	if err != nil {
		// Unreachable while rel came from expand, which only names files the
		// same corpus just listed. Reported rather than ignored: a corpus that
		// cannot be read is a self-test that verified nothing.
		return false, nil, "could not read/parse JSON: " + err.Error()
	}
	valid, errs, parseError, _ = CheckJSONBytes(data, schema)
	return valid, errs, parseError
}

// checkSource is CheckSourceFile addressed by a path relative to root.
func (s fixtureSource) checkSource(rel string, schema *Schema) (findings []SourceFinding, readError string) {
	if s.corpus == nil {
		return CheckSourceFile(filepath.Join(s.root, rel), schema)
	}
	data, err := s.corpus.ReadFile(rel)
	if err != nil {
		return nil, "could not read file: " + err.Error()
	}
	text, decodeError := decodeSourceText(data)
	if decodeError != "" {
		return nil, decodeError
	}
	return CheckSourceText(text, schema), ""
}

// selfTestSourceFixture checks one source fixture end-to-end
// (extraction -> normalization -> validation).
//
// Returns true when the fixture matched its expectation. A fixture with ZERO
// extractable annotations is a mismatch either way: that is what a
// silently-broken extractor looks like, and it must not read as green.
func selfTestSourceFixture(src fixtureSource, rel string, schema *Schema, expectValid bool) bool {
	findings, readError := src.checkSource(rel, schema)
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

// RunSelfTest grades this binary against the shipped fixture corpus.
//
// On the arithmetic in the final line: a run prints more PASS lines than
// "N/N fixtures matched expectation." accounts for. Those numbers count
// DIFFERENT THINGS and are supposed to disagree — `checked` counts FIXTURES,
// while the source fixtures print one line per ANNOTATION, of which there are
// more. Do not "fix" it by making them agree: a fixture is the unit of
// expectation and an annotation is the unit of reporting.
func RunSelfTest(schema *Schema) int {
	root, err := RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not locate the repo root: %s\n", err)
		return 2
	}
	return runSelfTest(schema, newFixtureSource(root))
}

// runSelfTest is RunSelfTest with the corpus injected, so both halves of the
// fallback can be asserted directly rather than only through a harness that has
// to build a tree to provoke each one — loadSchemaFrom's argument (fileio.go),
// and it applies with more force here: the case that matters most is "the
// embedded corpus somehow matched nothing", which cannot be staged from the
// outside at all, because staging it means shipping a broken binary.
func runSelfTest(schema *Schema, src fixtureSource) int {
	validFiles := src.expand(validExamplesGlob)
	invalidFiles := src.expand(invalidExamplesGlob)
	invalidSources := src.expand(invalidSourcesGlob)
	// `sources/*` yields the invalid/ directory entry, not its contents, and
	// the expansion drops directories — so the two sets are already disjoint.
	// The subtraction keeps them disjoint if the glob is ever widened to `**`.
	validSources := []string{}
	for _, path := range src.expand(validSourcesGlob) {
		if !contains(invalidSources, path) {
			validSources = append(validSources, path)
		}
	}

	// A fixture set that matched NOTHING is a failure, not a vacuous pass.
	// Dropping examples/invalid/ alone would turn 15/15 into a *greener*-reading
	// 4/4, exit 0, with the validator's ability to reject a bad annotation now
	// wholly unexercised. Each set is guarded independently — a `checked == 0`
	// check would sail straight past that case, which is both the likeliest
	// shape and the most misleading one. Every empty set is reported, so one run
	// names everything missing, and the run fails BEFORE checking anything: an
	// incomplete fixture inventory invalidates the whole run, so there is no
	// conformance verdict left to report.
	//
	// This applies to the EMBEDDED corpus exactly as it applies to a checkout,
	// and that is not a formality. A directive re-pointed or narrowed until it
	// matched nothing would otherwise turn a released binary's self-test into
	// the greenest possible lie — a check that verified nothing, on the one host
	// where nobody can look at the corpus to find out. Nothing below is reached
	// until all four sets exist.
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
				Quote(set.pattern), set.verifies)
		}
		return 1
	}

	mismatches := 0
	checked := 0

	// expected-valid fixtures
	for _, rel := range validFiles {
		checked++
		valid, errs, parseError := src.checkJSON(rel, schema)
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
	for _, rel := range invalidFiles {
		checked++
		valid, _, parseError := src.checkJSON(rel, schema)
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
	for _, rel := range validSources {
		checked++
		if !selfTestSourceFixture(src, rel, schema, true) {
			mismatches++
		}
	}
	for _, rel := range invalidSources {
		checked++
		if !selfTestSourceFixture(src, rel, schema, false) {
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

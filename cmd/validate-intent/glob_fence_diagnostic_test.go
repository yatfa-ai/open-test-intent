package main

// What the descent SAYS about its dependency/build fence.
//
// glob_fence_test.go pins which files the fence selects; this file pins what
// the tool tells the user about having applied it, which is a separate claim
// and was the defect. The fence shipped silent in both directions:
//
//   - a directory whose readable part the fence emptied printed the EMPTY
//     DIRECTORY's sentence, byte for byte. That sentence was not merely
//     unhelpful, it was UNTRUE — it blamed the tree for a silence the tool's
//     own walk produced, and it pointed the reader at the one place there was
//     nothing to find;
//   - a run that skipped a fenced directory and still had work to do printed
//     output byte-identical to a run over a tree that never held one, so a
//     narrowed read set was indistinguishable from an unnarrowed one.
//
// Both arms are settled semantics in this product rather than inventions here:
// `specguard-rspec`'s `all_fenced_reason` and `selection_line` already answer
// the same two questions for the Ruby client, and this binary saying what its
// own client says is the point. The one place the twin is NOT copied is its
// COUNT: that selector counts what it rejected, while this walk declines a
// directory at the prune and never enters it — so no count is produced, none is
// printed, and nothing here asserts one.
//
// The diagnostic's own discrimination — an all-fenced directory told apart from
// an empty one and from a nonexistent path, on both renderers — is pinned with
// its two siblings in glob_dirarg_test.go, where the situations it must differ
// from already live.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// allFencedTree writes a tree whose every annotated file sits inside a fenced
// directory, and returns its root.
//
// Two fenced directories rather than one, because a single `node_modules` would
// leave a rule about that one name indistinguishable from the fence.
func allFencedTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"node_modules/pkg/a.js": passingAnnotation,
		"dist/b.js":             passingAnnotation,
	})
}

// partialTree writes the COMMONER shape: the user's own file beside a fenced
// one, so the run succeeds and the fence still narrowed what it read.
func partialTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"src/real.js":             passingAnnotation,
		"node_modules/pkg/dep.js": passingAnnotation,
	})
}

// unfencedTwin writes partialTree's tree with the fenced directory absent: the
// same user-visible work, nothing for the fence to do.
func unfencedTwin(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"src/real.js": passingAnnotation,
	})
}

// --------------------------------------------------------------------------- //
// the partial arm: a SUCCESSFUL run that read less than its argument names
// --------------------------------------------------------------------------- //

// A run the fence narrowed says so, and a run it did not narrow is unchanged.
//
// The two halves are one test because either alone is satisfied by the wrong
// implementation: a disclosure that always fires passes the first check and
// turns every clean run into noise, and silence passes the second while leaving
// the narrowing invisible. Together they are the count-gating the Ruby twin
// does — its clause appears only when `skipped.positive?` — in this repo's
// spelling, where the gate is that the fence FIRED rather than a figure.
//
// The unnarrowed half is asserted as the EXACT stderr bytes rather than as "no
// fence clause", because an absence check is also satisfied by a run that broke
// some other way and printed a different diagnostic instead.
//
// Both renderers, because the disclosure is provenance about the selection and
// not a finding: it belongs on stderr in both, and a `--json` consumer parsing
// the document whole must not find a note wedged into it.
func TestRunSource_aFenceThatNarrowedTheRunSaysSo(t *testing.T) {
	narrowed := partialTree(t)
	untouched := unfencedTwin(t)

	_, narrowedOut, narrowedErr := captureRun(t, "--source", narrowed)
	_, untouchedOut, untouchedErr := captureRun(t, "--source", untouched)

	if untouchedErr != "" {
		t.Errorf("a run with nothing to fence must print exactly what it printed before the fence "+
			"existed; stderr = %q", untouchedErr)
	}
	if !strings.Contains(narrowedErr, "dependency or build directories") {
		t.Errorf("a run the fence narrowed must name the narrowing; stderr = %q", narrowedErr)
	}
	// The REPORT is untouched by the disclosure: the two runs read the same one
	// user file, so their stdout must agree once each tree's own root is
	// substituted out. This is what makes the note provenance rather than a
	// finding, and it is asserted rather than argued because writing the note
	// to stdout would pass every check above.
	narrowedReport := strings.ReplaceAll(narrowedOut, narrowed, "<ROOT>")
	untouchedReport := strings.ReplaceAll(untouchedOut, untouched, "<ROOT>")
	if narrowedReport != untouchedReport {
		t.Errorf("the fence disclosure reached the report:\n narrowed  %q\n untouched %q",
			narrowedReport, untouchedReport)
	}
	// The positive control. Two EMPTY reports would satisfy the equality above
	// while proving the run had stopped reading anything.
	if !strings.Contains(narrowedReport, "PASS  ") {
		t.Errorf("the user's own file must still be reported:\n%s", narrowedReport)
	}
}

// The machine channel's half: the disclosure is on stderr, and the DOCUMENT is
// byte-identical to the unnarrowed run's.
//
// A `--json` consumer parses stdout whole, so a note written there is a parse
// error or — worse — a silently extra finding. The document equality is the
// strongest available statement that the narrowing changed nothing a consumer
// reads, and the stderr check is what keeps that from meaning the disclosure
// was simply dropped under `--json`.
func TestRunSourceJSON_theFenceDisclosureIsProvenanceNotAFinding(t *testing.T) {
	narrowed := partialTree(t)
	untouched := unfencedTwin(t)

	_, narrowedOut, narrowedErr := captureRun(t, "--source", "--json", narrowed)
	_, untouchedOut, _ := captureRun(t, "--source", "--json", untouched)

	if !strings.Contains(narrowedErr, "dependency or build directories") {
		t.Errorf("--json must disclose the narrowing on stderr too; stderr = %q", narrowedErr)
	}
	narrowedDoc := strings.ReplaceAll(narrowedOut, narrowed, "<ROOT>")
	untouchedDoc := strings.ReplaceAll(untouchedOut, untouched, "<ROOT>")
	if narrowedDoc != untouchedDoc {
		t.Errorf("the fence disclosure reached the --json document:\n narrowed:\n%s\n untouched:\n%s",
			narrowedDoc, untouchedDoc)
	}
	if !strings.Contains(narrowedDoc, `"ok": true`) {
		t.Errorf("the narrowed run must still pass; document:\n%s", narrowedDoc)
	}
}

// The disclosure is PER PATTERN, not per run.
//
// `runOverPatterns` loops, and the fence fact is produced inside one expansion,
// so a fact that outlived its pattern would attach the previous argument's
// fence to the next argument's tree. The shape that catches it is the fenced
// argument FIRST and the clean one second: a leaked fact discloses twice, and
// an argument whose tree holds nothing to fence would be described as narrowed.
func TestRunSource_theFenceFactDoesNotLeakBetweenPatterns(t *testing.T) {
	narrowed := partialTree(t)
	untouched := unfencedTwin(t)

	_, _, stderr := captureRun(t, "--source", narrowed, untouched)

	if got, want := strings.Count(stderr, "dependency or build directories"), 1; got != want {
		t.Errorf("the fence fact leaked across the pattern loop; stderr = %q", stderr)
	}
}

// --------------------------------------------------------------------------- //
// how the fact is produced
// --------------------------------------------------------------------------- //

// The fact costs no step into the fenced tree.
//
// This is the property the whole mechanism turns on: the fence exists so the
// walk never enters a dependency tree, and a disclosure bought by walking one
// to size it would have undone the feature it describes. So the fact is
// recorded AT THE PRUNE — before the descent is declined — and the evidence for
// that is behavioural rather than a comment.
//
// TWO DIFFERENT PROPERTIES ARE PINNED HERE, and only the second one is about
// where the fact comes from.
//
// The shallow/deep pair pins SELECTION-INVARIANCE: two trees identical in the
// part the tool reads and differing only in what sits behind the fence must
// report the fence fired and select the IDENTICAL set, so producing the fact
// changed no file. That is worth pinning and it is NOT evidence about the
// prune: an implementation that learns the fact by walking the refused tree
// and discarding what it finds answers the same on both of these trees, since
// both hold files behind the fence and neither descent reaches the selection.
//
// The EMPTY fenced directory is what discriminates the prune. There is nothing
// behind that fence to find, so an implementation that looks inside to decide
// has nothing to decide FROM and answers "not fenced" — while still answering
// correctly on every other tree in this file. It is also the tree the feature's
// own wording rests on: noMatchDetail says "no file to read OUTSIDE" rather
// than the Ruby twin's "files found, all in" precisely BECAUSE a fenced
// directory can be empty, and a cleaned `dist/` or a bare `node_modules/` is
// the ordinary shape rather than a contrived one. Left uncovered, it is the
// tree that puts the false sentence back.
//
// No count is taken and none is asserted, here or anywhere in this file: a
// count of what a fence refused is exactly the figure that cannot be had
// without entering the refused tree.
func TestExpandFiles_theFenceFactIsProducedAtThePruneNotByDescending(t *testing.T) {
	shallow := writeTree(t, map[string]string{
		"src/a.ts":                  passingAnnotation,
		"node_modules/pkg/index.js": passingAnnotation,
	})
	deep := writeTree(t, map[string]string{
		"src/a.ts":                           passingAnnotation,
		"node_modules/pkg/index.js":          passingAnnotation,
		"node_modules/pkg/lib/b.js":          passingAnnotation,
		"node_modules/pkg/lib/deep/c.js":     passingAnnotation,
		"node_modules/other/d.js":            passingAnnotation,
		"node_modules/other/nested/e.js":     passingAnnotation,
		"node_modules/other/nested/far/f.js": passingAnnotation,
	})

	shallowFiles, shallowFenced := expandFilesFenced(shallow)
	deepFiles, deepFenced := expandFilesFenced(deep)

	if !shallowFenced || !deepFenced {
		t.Fatalf("the fence fired on neither or only one tree (shallow %v, deep %v); the comparison "+
			"below would be vacuous", shallowFenced, deepFenced)
	}

	want := []string{"src/a.ts"}
	if got := relativise(t, shallow, shallowFiles); !reflect.DeepEqual(got, want) {
		t.Errorf("the shallow tree's selection changed:\n got  %q\n want %q", got, want)
	}
	if got := relativise(t, deep, deepFiles); !reflect.DeepEqual(got, want) {
		t.Errorf("a bigger tree BEHIND the fence changed the selection:\n got  %q\n want %q",
			got, want)
	}

	// An EMPTY fenced directory. Nothing behind the fence at all, so an
	// implementation that learns the fact by LOOKING INSIDE answers "not
	// fenced" here while answering correctly on every other tree.
	empty := writeTree(t, map[string]string{"src/a.ts": passingAnnotation})
	if err := os.MkdirAll(filepath.Join(empty, "dist"), 0o755); err != nil {
		t.Fatalf("could not create the empty fenced directory: %v", err)
	}
	if _, emptyFenced := expandFilesFenced(empty); !emptyFenced {
		t.Error("an EMPTY fenced directory must still report the fence: the prune declined it " +
			"without entering it, so its contents cannot be what the fact is made of")
	}
}

// A fence that did not fire reports that it did not.
//
// The negative half of the flag, without which "the fence fired" is satisfied
// by a constant. It also re-states the count-gate at the layer the fact is
// produced in, rather than only at the layer it is printed in.
func TestExpandFiles_aTreeWithNothingToFenceReportsNoFence(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.ts":                   passingAnnotation,
		"src/vendor_helpers/h.ts":    passingAnnotation,
		"spec/.cache/hidden_spec.rb": passingAnnotation,
	})

	files, fenced := expandFilesFenced(root)

	if fenced {
		t.Errorf("nothing in this tree is fenced, but the fence reported firing; selected %q",
			relativise(t, root, files))
	}
	// A hidden directory is skipped by the rule the fence sits BESIDE, and must
	// not be reported as a fence: the two rules refuse different things and a
	// user told "dependency or build directories" about their `.cache` would be
	// told the wrong reason.
	want := []string{"src/a.ts", "src/vendor_helpers/h.ts"}
	if got := relativise(t, root, files); !reflect.DeepEqual(got, want) {
		t.Errorf("the selection is wrong, so the flag above proves nothing:\n got  %q\n want %q",
			got, want)
	}
}

// The fact travels through the DIR/** spelling too.
//
// The fence lives in the shared descent precisely so `DIR` and `DIR/**` stay
// the identical set, and the fact it produces has to follow the same rule — a
// mechanism wired into the bare-directory rewrite alone would disclose on one
// spelling and stay silent on the other, and the sugar's advertised equivalence
// would quietly stop holding for what the tool SAYS.
func TestExpandFiles_bothSpellingsOfTheDescentProduceTheFence(t *testing.T) {
	root := partialTree(t)

	bare, bareFenced := expandFilesFenced(root)
	explicit, explicitFenced := expandFilesFenced(root + "/**")

	if bareFenced != explicitFenced {
		t.Errorf("DIR reported fenced=%v and DIR/** reported fenced=%v", bareFenced, explicitFenced)
	}
	if !bareFenced {
		t.Error("the fence did not fire on either spelling; the equality above proves nothing")
	}
	if !reflect.DeepEqual(bare, explicit) {
		t.Errorf("the two spellings selected different files:\n bare     %q\n explicit %q", bare, explicit)
	}
}

// The fence SENTENCE follows the sugar's other half too.
//
// The pin above settles that both spellings PRODUCE the fence fact; this one
// settles that both spellings SAY it. They did not used to: the sentence layer
// gated every clause on the bare spelling's rewrite — a test a magic-carrying
// pattern can never satisfy — so an all-fenced tree got the fence clause
// through `DIR` and the generic bytes through `DIR/**` (SPGD-1386). The same
// silence was the tool's own doing in one breath and unexplained in the other,
// and the sugar's advertised equivalence stopped holding for what the tool
// SAYS while still holding for what it reads.
//
// The cause is asserted, not merely a difference from the generic line: the
// generic bytes also differ from the bare spelling's, and difference was never
// the claim. Both renderers, because noMatchDetail is worn by both.
func TestRunSource_theAllFencedSentenceFollowsTheExplicitSpelling(t *testing.T) {
	root := allFencedTree(t)
	explicit := root + "/**"

	_, _, stderr := captureRun(t, "--source", explicit)
	if !strings.Contains(stderr, "dependency or build directories") {
		t.Errorf("an all-fenced tree must name the fence through the explicit spelling too; "+
			"stderr = %q", stderr)
	}

	schema := repoSchema(t)
	document, code := runSourceJSON(t, []string{explicit}, schema)
	if code != 1 {
		t.Errorf("--source --json on an all-fenced tree exited %d, want 1; document:\n%s", code, document)
	}
	if !strings.Contains(errorsBlock(t, document), "dependency or build directories") {
		t.Errorf("the fence cause must reach errors[] through the explicit spelling too; document:\n%s",
			document)
	}
}

// The fact accumulates from ANY DEPTH.
//
// `descendants` recurses, and the fact is produced inside that recursion, so a
// mechanism that records the prune into a value local to one level answers
// truthfully for a fence at the top of the tree and silently drops every deeper
// one. That is not a corner: a fenced directory is far more often `src/ui/
// node_modules` or `app/tmp` than a top-level one, and every other case in this
// file fences at the top — so this mutation survives all of them.
//
// Both halves of the tree are asserted: the nested fence fires, and the
// selection is unchanged by the fact having been produced.
func TestExpandFiles_theFenceFactAccumulatesFromAnyDepth(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.ts":                            passingAnnotation,
		"src/ui/b.ts":                         passingAnnotation,
		"src/ui/node_modules/pkg/dep.js":      passingAnnotation,
		"src/ui/widgets/deep/dist/emitted.js": passingAnnotation,
	})

	files, fenced := expandFilesFenced(root)

	if !fenced {
		t.Error("a fence below the top level must still be reported; the descent recursed past it " +
			"and dropped the fact")
	}
	want := []string{"src/a.ts", "src/ui/b.ts"}
	if got := relativise(t, root, files); !reflect.DeepEqual(got, want) {
		t.Errorf("the nested fences did not select the user's own files:\n got  %q\n want %q", got, want)
	}
}

// relativise renders absolute matches as sorted root-relative slash paths, so
// an expectation reads as the path list it is about.
//
// A sibling of glob_fence_test.go's relativeMatches, which expands a pattern of
// its own; this one takes the matches a caller already has, because the callers
// here are asserting on the SECOND return value and cannot re-expand without
// discarding it.
func relativise(t *testing.T, root string, matches []string) []string {
	t.Helper()
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			t.Fatalf("%q is not under the tree root %q: %v", match, root, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

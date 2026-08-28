package main

// The expected-invalid SOURCE corpus, graded per annotation.
//
// examples/sources/invalid/broken_intent_spec.rb opens by stating its own
// contract: "every @intent annotation in this file MUST be rejected, each
// reported at its own file:line." The self-test cannot grade that. It walks the
// findings asserting one bit each (`finding.Valid == expectValid`, in
// selfTestSourceFixture, selftest.go) and guards structure only with
// `len(findings) == 0` — so the COUNT is unpinned and so are Line and Kind.
//
// WHY ONE BIT IS NOT ENOUGH, in the same terms conformance_test.go states for
// adopter mode. If extraction regressed so that the two malformed annotations
// were silently swallowed, the fixture would yield 3 findings instead of 5,
// every survivor would still be invalid, the list would still be non-empty, and
// the self-test would print PASS three times and exit green. The swallowed pair
// is exactly the pair the schema can never catch on its own: a malformed
// annotation never reaches validation, so KindExtraction is only ever produced
// by extraction actually noticing. A silent skip is therefore invisible to
// every check that exists today.
//
// So the assertions here are the ones a verdict cannot carry: how MANY
// annotations the fixture yields, WHERE each one is, and by WHICH KIND it was
// refused — the same "the kind of failure and the diagnostic is what separates
// them" reasoning as conformance_test.go, applied to the other mode.
//
// The fixture is read off disk rather than restated here, and a missing file is
// a failure rather than a skip: the fixture is the artifact an adopter copies,
// so the assertion must be about the fixture and not about a copy that drifts.

import "testing"

// TestInvalidSourceCorpusIsGradedPerAnnotation pins the shipped expected-invalid
// source fixture: its annotation count, each annotation's line, and each
// annotation's failure kind.
//
// Delete an @intent annotation from the fixture, move one, or let one be
// swallowed by the extractor, and this test goes red. The self-test stays green
// through all three.
func TestInvalidSourceCorpusIsGradedPerAnnotation(t *testing.T) {
	const rel = "examples/sources/invalid/broken_intent_spec.rb"

	schema := conformanceSchema(t)
	text := readRepoFile(t, rel)
	findings := CheckSourceText(text, schema)

	// Every annotation in the fixture, in source order. The kinds are not
	// decoration: the two KindExtraction rows are the ones no schema rule can
	// reproduce, and they are the rows a silent skip removes.
	want := []struct {
		line int
		kind string
		why  string
	}{
		{9, KindSchema, "typo'd `entiity` — caught by additionalProperties"},
		{15, KindSchema, "`layer` outside the enum"},
		{21, KindSchema, "missing required `layer`, and `behavior` under minLength"},
		{28, KindExtraction, "unterminated object literal"},
		{34, KindExtraction, "an @intent: token with no payload at all"},
	}

	// THE COUNT IS AN ASSERTION, not a bounds check before the loop. `len(x) ==
	// 0` — the only structural guard the self-test has — cannot tell a corpus
	// that lost two annotations from one that never had them.
	if len(findings) != len(want) {
		t.Fatalf("%s yielded %d annotations, want %d — an annotation was added, "+
			"removed, or silently swallowed by extraction",
			rel, len(findings), len(want))
	}

	for i, tc := range want {
		finding := findings[i]

		if finding.Line != tc.line {
			t.Errorf("annotation %d is reported at %s:%d, want line %d (%s) — the "+
				"fixture declares that each annotation is reported at its own file:line",
				i, rel, finding.Line, tc.line, tc.why)
		}

		// Every annotation in this fixture must be REJECTED. This is the one bit
		// the self-test already grades; it is restated here so a failure in this
		// file is self-contained rather than only meaningful next to the harness.
		if finding.Valid {
			t.Errorf("%s:%d validated; the fixture requires every annotation in it "+
				"to be rejected (%s)", rel, tc.line, tc.why)
			continue
		}

		// THE LOAD-BEARING ASSERTION, for the same reason conformance_test.go
		// labels its own that way. Rejection alone passes whether the annotation
		// was refused by extraction or by the schema, and those are different
		// claims about different code. A malformed annotation graded `schema`
		// means it reached validation, which it must never do.
		if finding.Kind != tc.kind {
			t.Errorf("%s:%d failed with kind %q, want %q (%s) — it was refused by "+
				"something other than the stage that should have refused it",
				rel, tc.line, finding.Kind, tc.kind, tc.why)
		}

		// And it must say WHY. A rejection with no diagnostic is a verdict an
		// adopter cannot act on, and the kinds carry it in different fields:
		// the pre-validation refusals (KindExtraction, KindParse) in Problem,
		// a schema refusal in Errors.
		//
		// THE `default` IS PART OF THE POINT, not defensive boilerplate. This
		// file exists because a grading path that can silently skip is a check
		// that verifies nothing; a switch with no default is that same shape,
		// one level up. Source mode can produce a third rejection kind here —
		// KindParse, produced in CheckSourceText (source.go), reachable from
		// this very fixture with a malformed payload — so without a default,
		// adding such a row to broken_intent_spec.rb would silently buy no
		// diagnostic assertion at all. Fatalf rather than Errorf: an ungraded
		// kind means the grader is incomplete, which is a defect in this test,
		// not in the fixture.
		switch tc.kind {
		case KindExtraction, KindParse:
			if finding.Problem == "" {
				t.Errorf("%s:%d was rejected with kind %q but an empty Problem (%s)",
					rel, tc.line, tc.kind, tc.why)
			}
		case KindSchema:
			if len(finding.Errors) == 0 {
				t.Errorf("%s:%d was rejected by the schema with no errors listed (%s)",
					rel, tc.line, tc.why)
			}
		default:
			t.Fatalf("%s:%d expects kind %q, which this test has no diagnostic "+
				"assertion for — extend the switch so the new kind is graded on "+
				"more than its name (%s)", rel, tc.line, tc.kind, tc.why)
		}
	}
}

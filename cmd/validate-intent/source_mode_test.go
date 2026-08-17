package main

// The `--source` mode, driven through its own entry points.
//
// Three of the validator's four modes had a test that actually RAN them —
// stdin_mode_test.go reaches RunStdinJSON and RunAdopterJSON,
// selftest_embed_test.go reaches RunSelfTest. `--source` did not. The four
// argv cases naming `--source` elsewhere in this tree all short-circuit before
// main.go dispatches (a bare `--source` is a usage error; `--version` and
// `--schema-source` are handled ahead of the mode), and source_test.go /
// source_corpus_test.go exercise the layer BELOW the mode — CheckSourceText,
// never CheckSourceFile and never RunSource.
//
// An earlier revision of this paragraph credited stdin_mode_test.go with
// reaching RunStdin as well. It did not: no test named that function, and the
// 37.5% the coverage tool reported for it was an argv row in version_test.go
// riding the empty stdin `go test` supplies. Both TEXT renderers were unpinned
// for the same reason `--source` was, one mode over — SPGD-683 closed that half
// and stdin_mode_test.go now drives RunStdin and RunAdopter directly.
//
// The sharpest consequence was in render_test.go, which builds a
// `&JSONReport{Mode: "source", Files: 2, Annotations: 3}` LITERAL and checks
// how Emit renders it. The renderer was pinned against hand-written counters
// while the code that DERIVES those counters was unpinned: RunSourceJSON could
// have counted files as annotations, or counted an unreadable file as a site,
// and every test in the package stayed green.
//
// So these drive the two entry points and assert what only they can produce:
// the line vocabulary text mode prints, the `----` rule the mode exists in its
// own shape for, the counters RunSourceJSON derives, and that the two renderers
// cannot disagree about whether a run passed.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// emDash is the U+2014 the text renderer puts between a location and its
// problem. RunSource (source_mode.go) calls it "load-bearing output, not
// decoration": it is what a consumer splits a FAIL line on, so a hyphen or an
// en dash in that position is a format change wearing a typo's clothes. Written as an
// escape rather than pasted, because the two neighbouring characters are
// indistinguishable in most editors — which is exactly why nothing caught it
// before.
const emDash = "\u2014"

// continuation is the prefix on each schema error under a FAIL headline
// (RunSource, source_mode.go): eight spaces, an arrow, one space.
const continuation = "        -> "

// sourceFixture resolves a shipped fixture, repo-relative.
//
// A missing fixture is a FAILURE, not a skip, for the reason
// source_corpus_test.go states: the fixture is the artifact an adopter copies,
// so a corpus that lost one must not read as green.
func sourceFixture(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", filepath.FromSlash(rel))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shipped fixture %s is missing: %v", rel, err)
	}
	return path
}

// writeTemp writes body to a fresh temp file and returns its path.
func writeTemp(t *testing.T, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func runSourceText(t *testing.T, patterns []string, schema *Schema) (string, int) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = RunSource(patterns, schema) })
	return out, code
}

func runSourceJSON(t *testing.T, patterns []string, schema *Schema) (string, int) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = RunSourceJSON(patterns, schema) })
	return out, code
}

// assertJSONOrder checks that every wanted fragment appears, in this order.
//
// Order of APPEARANCE in the raw text, not membership: a findings array that
// emitted the right rows in the wrong order — or attached line 28's kind to
// line 9 — contains every fragment below and is still wrong.
func assertJSONOrder(t *testing.T, out string, want []string) {
	t.Helper()
	cursor := 0
	for _, fragment := range want {
		idx := strings.Index(out[cursor:], fragment)
		if idx < 0 {
			t.Fatalf("%s missing or out of order; document was:\n%s", fragment, out)
		}
		cursor += idx + len(fragment)
	}
}

// TestRunSource_passLinesAreTheWholeOutputOfACleanRun drives RunSource over the
// three shipped valid source fixtures.
//
// The assertion is whole-output equality rather than a set of Contains checks,
// because the things most likely to rot here are the things Contains cannot
// see: the TWO spaces after PASS, one line per ANNOTATION rather than per file,
// source order within a file, argument order across files, and the absence of
// any summary line. Every one of those survives a substring assertion.
func TestRunSource_passLinesAreTheWholeOutputOfACleanRun(t *testing.T) {
	schema := repoSchema(t)
	ruby := sourceFixture(t, "examples/sources/order_spec.rb")
	js := sourceFixture(t, "examples/sources/checkout_flow.spec.js")
	py := sourceFixture(t, "examples/sources/checkout_service_test.py")

	out, code := runSourceText(t, []string{ruby, js, py}, schema)
	if code != 0 {
		t.Fatalf("the valid source fixtures exited %d, want 0; output was:\n%s", code, out)
	}

	var want strings.Builder
	pass := func(path string, lines ...int) {
		for _, line := range lines {
			want.WriteString("PASS  " + path + ":" + strconv.Itoa(line) + "\n")
		}
	}
	pass(ruby, 10, 17, 24, 32, 41, 49, 57)
	pass(js, 7)
	pass(py, 12, 18)

	if out != want.String() {
		t.Errorf("--source output\n got:\n%s\nwant:\n%s", out, want.String())
	}
}

// TestRunSource_failLinesCarryTheEmDashAndContinuationPrefix pins the failure
// half of the vocabulary over the shipped expected-invalid fixture.
//
// The fixture's five annotations fail in two DIFFERENT SHAPES, and the shapes
// are the point: a schema failure prints a bare `FAIL file:line` headline with
// one indented `-> ` line per error, while an extraction failure prints its
// problem inline after the em dash and has no continuation lines at all
// (RunSource, source_mode.go). Collapsing the two — always inlining, or always
// indenting — is a change no exit-code assertion can see.
//
// The diagnostics themselves are deliberately NOT pinned here (their wording is
// graded per annotation by source_corpus_test.go). This test is about the
// SHAPE: prefix, separator, indent.
func TestRunSource_failLinesCarryTheEmDashAndContinuationPrefix(t *testing.T) {
	schema := repoSchema(t)
	invalid := sourceFixture(t, "examples/sources/invalid/broken_intent_spec.rb")

	out, code := runSourceText(t, []string{invalid}, schema)
	if code != 1 {
		t.Fatalf("the expected-invalid fixture exited %d, want 1; output was:\n%s", code, out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

	// One row per annotation, in source order — the same five source_corpus_test.go
	// grades, seen from the renderer's side.
	cases := []struct {
		line       int
		inline     bool // the problem follows the em dash on the headline
		errorLines int  // indented continuation lines beneath it
	}{
		{line: 9, inline: false, errorLines: 2},
		{line: 15, inline: false, errorLines: 1},
		{line: 21, inline: false, errorLines: 2},
		{line: 28, inline: true, errorLines: 0},
		{line: 34, inline: true, errorLines: 0},
	}

	cursor := 0
	next := func(what string) string {
		if cursor >= len(lines) {
			t.Fatalf("output ended before %s; output was:\n%s", what, out)
		}
		line := lines[cursor]
		cursor++
		return line
	}

	for _, tc := range cases {
		where := "FAIL  " + invalid + ":" + strconv.Itoa(tc.line)
		headline := next(where)

		if tc.inline {
			prefix := where + " " + emDash + " "
			switch {
			case strings.HasPrefix(headline, prefix):
				if strings.TrimPrefix(headline, prefix) == "" {
					t.Errorf("%s carries the em dash and then no problem text", where)
				}
			case strings.HasPrefix(headline, where+" - "), strings.HasPrefix(headline, where+" \u2013 "):
				t.Errorf("the separator on %q must be an em dash (U+2014); it is a hyphen or en dash", headline)
			default:
				t.Errorf("headline %q, want prefix %q", headline, prefix)
			}
		} else if headline != where {
			t.Errorf("headline %q, want exactly %q — a schema failure's errors belong on "+
				"continuation lines, not inlined after an em dash", headline, where)
		}

		for i := 0; i < tc.errorLines; i++ {
			errLine := next("continuation line " + strconv.Itoa(i+1) + " of " + where)
			if !strings.HasPrefix(errLine, continuation) {
				t.Errorf("continuation line %q, want prefix %q", errLine, continuation)
			}
			if strings.TrimSpace(strings.TrimPrefix(errLine, continuation)) == "" {
				t.Errorf("continuation line %q carries no diagnostic", errLine)
			}
		}
	}

	if cursor != len(lines) {
		t.Errorf("unexpected trailing output: %q", lines[cursor:])
	}
}

// TestRunSource_fileWithNoAnnotationsIsReportedAndDoesNotFail is the rule the
// mode exists in its own shape for, and nothing asserted either half of it.
//
// RunSource's doc comment states it twice over: a file with no annotations is
// "reported and skipped, not failed", and the `----` line is "the absence of
// anything to report, not a result" — the reason `--source` cannot be collapsed
// into adopter mode's one-finding-per-file shape. Both halves are asserted
// here: the line VERBATIM (the whole string is vocabulary, including the em
// dash and the four leading dashes), and that the run still exits 0.
func TestRunSource_fileWithNoAnnotationsIsReportedAndDoesNotFail(t *testing.T) {
	schema := repoSchema(t)
	plain := writeTemp(t, "plain_spec.rb", []byte(
		"describe Order do\n  it \"charges the card\" do\n    expect(order).to be_paid\n  end\nend\n"))

	t.Run("alone", func(t *testing.T) {
		out, code := runSourceText(t, []string{plain}, schema)
		if code != 0 {
			t.Errorf("a file with no @intent annotations exited %d, want 0 — "+
				"the protocol allows unannotated tests", code)
		}
		want := "----  " + plain + " " + emDash + " no @intent annotations\n"
		if out != want {
			t.Errorf("output %q, want %q", out, want)
		}
	})

	// ...and it must not drag down a batch that otherwise passes, which is the
	// form an adopter actually meets it in.
	t.Run("beside an annotated file", func(t *testing.T) {
		js := sourceFixture(t, "examples/sources/checkout_flow.spec.js")
		out, code := runSourceText(t, []string{js, plain}, schema)
		if code != 0 {
			t.Errorf("exit %d, want 0; output was:\n%s", code, out)
		}
		want := "PASS  " + js + ":7\n" +
			"----  " + plain + " " + emDash + " no @intent annotations\n"
		if out != want {
			t.Errorf("output\n got:%q\nwant:%q", out, want)
		}
	})
}

// TestRunSource_unreadableFileFailsTheRun is the other side of the same rule:
// nothing to report exits 0, but nothing READABLE is a failure
// (RunSource, source_mode.go).
//
// The unreadable file here is one holding undecodable bytes rather than one
// chmod'd to 000. That is not a stylistic choice: the suite routinely runs as
// root, where chmod 000 does not make a file unreadable at all — the adopter
// equivalent in stdin_mode_test.go has to SKIP itself there, leaving the rule
// unexercised on exactly the machines CI uses. decodeSourceText (fileio.go)
// refuses undecodable bytes for every user, so this reaches the same branch
// unconditionally.
func TestRunSource_unreadableFileFailsTheRun(t *testing.T) {
	schema := repoSchema(t)
	// The first line is a well-formed annotation. It must NOT be reported: the
	// file is refused before the extractor sees it, so a run reporting a PASS
	// here would be reporting an annotation from text it never validated.
	unreadable := writeTemp(t, "bad_bytes_spec.rb", []byte("# @intent: {}\n\xff\xfe\n"))

	out, code := runSourceText(t, []string{unreadable}, schema)
	if code != 1 {
		t.Errorf("an unreadable file exited %d, want 1", code)
	}
	want := "FAIL  " + unreadable + " " + emDash + " could not read file: " + errNotUTF8 + "\n"
	if out != want {
		t.Errorf("output\n got:%q\nwant:%q", out, want)
	}
}

// TestRunSourceJSON_derivesFilesAndAnnotationSites drives the --json renderer
// over a batch where the three summary numbers deliberately disagree.
//
// render_test.go pins how a JSONReport RENDERS, from a literal it fills in
// itself. Nothing pinned how RunSourceJSON FILLS ONE IN, which is where the
// counting rules live: `files` counts every file the patterns matched, readable
// or not; `annotations` counts annotation SITES EXAMINED, so a file with none
// and a file that could not be read both contribute zero; `failed` counts
// failing findings, including the read failure that contributed no site.
// Four files, six sites, six failures — no two of them equal, so a renderer
// that reaches for one counter and reuses it cannot produce this document.
func TestRunSourceJSON_derivesFilesAndAnnotationSites(t *testing.T) {
	schema := repoSchema(t)
	js := sourceFixture(t, "examples/sources/checkout_flow.spec.js")
	invalid := sourceFixture(t, "examples/sources/invalid/broken_intent_spec.rb")
	plain := writeTemp(t, "plain_spec.rb", []byte("# no annotations in here\n"))
	unreadable := writeTemp(t, "bad_bytes_spec.rb", []byte("\xff\xfe\n"))

	out, code := runSourceJSON(t, []string{js, invalid, plain, unreadable}, schema)
	if code != 1 {
		t.Fatalf("exit %d, want 1; document was:\n%s", code, out)
	}

	assertJSONOrder(t, out, []string{
		`"mode": "source"`,
		`"ok": false`,
		`"files": 4`,       // every matched file, readable or not
		`"annotations": 6`, // 1 + 5; the empty and the unreadable file contribute none
		`"failed": 6`,      // 5 bad annotations + the read failure

		// One finding per ANNOTATION, each at its own line, with `kind` naming
		// why it failed and null where it did not.
		`"file": ` + EncodeJSONPath(js), `"line": 7`, `"ok": true`, `"kind": null`,
		`"file": ` + EncodeJSONPath(invalid), `"line": 9`, `"ok": false`, `"kind": "schema"`,
		`"line": 15`, `"ok": false`, `"kind": "schema"`,
		`"line": 21`, `"ok": false`, `"kind": "schema"`,
		`"line": 28`, `"ok": false`, `"kind": "extraction"`,
		`"line": 34`, `"ok": false`, `"kind": "extraction"`,

		// A file that could not be read is one finding with no line at all.
		`"file": ` + EncodeJSONPath(unreadable), `"line": null`, `"ok": false`, `"kind": "read"`,
	})

	// The file with no annotations contributes to `files` and to NOTHING else.
	// Text mode's `----` line is the absence of a result; emitting it as a
	// finding would hand consumers a row to filter back out (RunSourceJSON,
	// report.go).
	if strings.Contains(out, plain) {
		t.Errorf("the file with no annotations must not appear in findings:\n%s", out)
	}

	// Re-run without the unreadable file. `files` drops by one and `failed` by
	// one, and `annotations` DOES NOT MOVE — which is the claim "an unreadable
	// file contributes no annotation sites" stated as a difference rather than
	// as a number that happens to look right.
	readable, code := runSourceJSON(t, []string{js, invalid, plain}, schema)
	if code != 1 {
		t.Fatalf("exit %d, want 1; document was:\n%s", code, readable)
	}
	for _, want := range []string{`"files": 3`, `"annotations": 6`, `"failed": 5`} {
		if !strings.Contains(readable, want) {
			t.Errorf("missing %s in:\n%s", want, readable)
		}
	}
}

// TestRunSourceJSON_normalizesProblemIntoTheErrorsList pins RunSourceJSON's
// Problem/Errors normalization (report.go).
//
// A SourceFinding carries its diagnostic in one of two fields — `Problem` for
// an extraction or parse failure, `Errors` for a schema one — and text mode
// renders them differently on purpose. The JSON document must expose ONE shape,
// so `Problem` is folded into the `errors` list and never surfaces as a key of
// its own.
//
// The expected strings are taken from the TEXT renderer's own output rather
// than restated, so the two cannot drift apart while both stay green.
func TestRunSourceJSON_normalizesProblemIntoTheErrorsList(t *testing.T) {
	schema := repoSchema(t)
	invalid := sourceFixture(t, "examples/sources/invalid/broken_intent_spec.rb")

	text, _ := runSourceText(t, []string{invalid}, schema)
	document, _ := runSourceJSON(t, []string{invalid}, schema)

	problems := 0
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "FAIL  ") {
			continue
		}
		parts := strings.SplitN(line, " "+emDash+" ", 2)
		if len(parts) != 2 {
			continue // a schema failure: its errors are on continuation lines
		}
		problems++
		if !strings.Contains(document, EncodeJSONString(parts[1])) {
			t.Errorf("text mode reported the problem %q, which is absent from the "+
				"JSON errors list:\n%s", parts[1], document)
		}
	}
	if problems != 2 {
		t.Fatalf("expected the fixture to yield 2 problem-carrying findings, got %d;"+
			" text output was:\n%s", problems, text)
	}

	if strings.Contains(document, `"problem"`) {
		t.Errorf("`problem` is normalized into `errors`; it must never be a key of "+
			"its own:\n%s", document)
	}
}

// TestRunSource_renderersAgreeOnTheExitCode is the single highest-value
// assertion available here, and it rested on nothing.
//
// The two renderers are meant to be the SAME checks behind a different
// presentation — Emit takes the exit code the text path would have produced
// rather than recomputing one from the findings, precisely so they cannot
// disagree (JSONReport.Emit, report.go). Nothing tested that for `--source`,
// and a consumer that switches to `--json` for machine readability inherits the
// verdict, not just the format. The `ok` field is checked alongside the code
// because a document that says `"ok": true` while exiting 1 is worse than
// either being wrong on its own.
//
// The no-match case writes its text-mode diagnostic to STDERR
// (runOverPatterns, main.go), so that one line appears in the test log; the
// assertion is on the exit code and on the JSON document, both of which are
// captured.
func TestRunSource_renderersAgreeOnTheExitCode(t *testing.T) {
	schema := repoSchema(t)
	valid := sourceFixture(t, "examples/sources/order_spec.rb")
	invalid := sourceFixture(t, "examples/sources/invalid/broken_intent_spec.rb")
	plain := writeTemp(t, "plain_spec.rb", []byte("# no annotations in here\n"))
	unreadable := writeTemp(t, "bad_bytes_spec.rb", []byte("\xff\xfe\n"))
	noMatch := filepath.Join(t.TempDir(), "nope-*.rb")

	cases := []struct {
		name     string
		patterns []string
		want     int
	}{
		{"valid annotations", []string{valid}, 0},
		{"no annotations at all", []string{plain}, 0},
		{"valid beside unannotated", []string{valid, plain}, 0},
		{"schema and extraction failures", []string{invalid}, 1},
		{"unreadable input", []string{unreadable}, 1},
		{"a pattern matching nothing", []string{noMatch}, 1},
		{"all of it at once", []string{valid, plain, invalid, unreadable, noMatch}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, textCode := runSourceText(t, tc.patterns, schema)
			document, jsonCode := runSourceJSON(t, tc.patterns, schema)

			if textCode != tc.want {
				t.Errorf("RunSource exited %d, want %d", textCode, tc.want)
			}
			if jsonCode != textCode {
				t.Errorf("RunSourceJSON exited %d but RunSource exited %d — the two "+
					"renderers must not disagree about whether the run passed", jsonCode, textCode)
			}
			wantOK := `"ok": ` + jsonBool(tc.want == 0)
			if !strings.Contains(document, wantOK) {
				t.Errorf("document should carry %s alongside exit %d:\n%s", wantOK, jsonCode, document)
			}
		})
	}
}

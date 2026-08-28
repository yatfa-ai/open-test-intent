package main

// Tests for the two modes slice 3 added — stdin and adopter — pinned at the
// level the differential harness cannot isolate. Both renderers of each mode
// are driven here: `--json` and the TEXT default an adopter actually sees.
//
// The self-test does not reach stdin mode at all, which is
// the real acceptance test. What it cannot do is say WHICH property broke when
// a document changes: a whole-document diff reports "stdout differs" whether the
// cause is a miscount, a reordered key or a lost newline. These name the
// properties one at a time, so a failure points at the rule rather than at the
// output.
//
// The text half arrived late (SPGD-683). Until then RunStdin sat at 37.5% and
// RunAdopter at 15.4%, and the only reason either was non-zero was an argv table
// in version_test.go whose `-` row worked because `go test` happens to hand the
// process an empty stdin — so the only exercised path through each was the one
// where there is nothing to report. Nothing here relies on that ambient stdin:
// every case sets its bytes through withStdin.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
// The port writes through pyWriteStdout, which resolves os.Stdout per call, so
// swapping the variable is enough.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := read.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()

	fn()
	os.Stdout = saved
	write.Close()
	out := <-done
	read.Close()
	return out
}

// withStdin points os.Stdin at a file holding exactly these bytes.
func withStdin(t *testing.T, data []byte, fn func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing stdin fixture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening stdin fixture: %v", err)
	}
	defer file.Close()
	saved := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = saved }()
	fn()
}

func repoSchema(t *testing.T) *Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "open-test-intent.v1.json"))
	if err != nil {
		t.Fatalf("reading the shipped schema: %v", err)
	}
	root, err := DecodeJSON(raw)
	if err != nil {
		t.Fatalf("decoding the shipped schema: %v", err)
	}
	schema, err := CompileSchema(root)
	if err != nil {
		t.Fatalf("compiling the shipped schema: %v", err)
	}
	return schema
}

// runStdinText drives RunStdin over exactly these bytes and returns what the
// text renderer wrote together with the exit code it chose.
//
// The bytes are always set explicitly. RunStdin reads os.Stdin, so a test that
// omits the input still "passes" against whatever `go test` left on the stream —
// which is how this entry point stayed at 37.5% while looking exercised.
func runStdinText(t *testing.T, input []byte, schema *Schema) (string, int) {
	t.Helper()
	var out string
	var code int
	withStdin(t, input, func() {
		out = captureStdout(t, func() { code = RunStdin(schema) })
	})
	return out, code
}

// TestRunStdin_verdictLinesAreTheWholeOutput drives the TEXT renderer of the
// mode stdin_mode.go's file header calls the one "where a wrong answer costs
// the most: the caller has no filename to sanity-check the verdict against".
//
// Whole-output equality rather than Contains, because what rots here is what a
// substring assertion cannot see: a stray summary line beneath PASS, a verdict
// printed to stderr as well, a lost or doubled newline. And the exit code is
// asserted beside the text on every row — the contract stdin mode makes is the
// PAIR (what it printed, what it returned), and the two can drift apart one at
// a time.
//
// The read and parse failures share one prose line on purpose (RunStdin,
// stdin_mode.go): text mode has nowhere to put the `kind` its --json twin
// carries, so the distinction TestRunStdinJSON_readParseSplit pins is
// deliberately invisible here. That is why both rows below want the same
// prefix.
func TestRunStdin_verdictLinesAreTheWholeOutput(t *testing.T) {
	schema := repoSchema(t)
	valid, err := os.ReadFile(filepath.Join("..", "..", "examples", "unit-order-total.json"))
	if err != nil {
		t.Fatalf("reading a valid fixture: %v", err)
	}

	failPrefix := "FAIL " + emDash + " could not read/parse JSON: "

	cases := []struct {
		name  string
		input []byte
		want  string // exact whole output, when the wording is ours to pin
		// ...or a prefix, when the tail is a decoder diagnostic graded elsewhere
		wantPrefix string
		wantCode   int
	}{
		{
			// The single most important promise the mode makes, and the one the
			// old suite left unpinned: a schema-valid document exits 0 and says
			// so in one word.
			name: "schema-valid document", input: valid,
			want: "PASS\n", wantCode: 0,
		},
		{
			// Bytes that never decoded. PROTOCOL.md §1.1 makes UTF-8 part of what
			// a JSON text IS, so this is a READ failure — and in text mode it has
			// to be indistinguishable from the parse failure below.
			name: "not well-formed UTF-8", input: []byte("\xff\xfe{}"),
			want: failPrefix + errNotUTF8 + "\n", wantCode: 1,
		},
		{
			name: "decoded, then rejected by the parser", input: []byte(`{"broken"`),
			wantPrefix: failPrefix, wantCode: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runStdinText(t, tc.input, schema)
			if code != tc.wantCode {
				t.Errorf("exit %d, want %d; output was:\n%s", code, tc.wantCode, out)
			}
			switch {
			case tc.want != "":
				if out != tc.want {
					t.Errorf("output %q, want exactly %q", out, tc.want)
				}
			default:
				if !strings.HasPrefix(out, tc.wantPrefix) {
					t.Errorf("output %q, want prefix %q", out, tc.wantPrefix)
				}
				if strings.TrimSpace(strings.TrimPrefix(out, tc.wantPrefix)) == "" {
					t.Errorf("output %q carries the prefix and then no diagnostic", out)
				}
				if lines := strings.Count(out, "\n"); lines != 1 {
					t.Errorf("a read/parse failure is ONE line; got %d in:\n%s", lines, out)
				}
			}
		})
	}
}

// TestRunStdin_schemaViolationHeadlineIsBareAndErrorsAreIndented is the shape
// half of the failure vocabulary, kept apart from the case above because the
// two failures print DIFFERENTLY and the difference is the assertion.
//
// A read/parse failure inlines its prose after the em dash on the headline; a
// schema violation prints a bare `FAIL` and puts one indented `-> ` line under
// it per error (RunStdin, stdin_mode.go). Collapsing the two — always
// inlining, or always indenting — is a change no exit-code assertion can see,
// and it is the same distinction source_mode_test.go pins for `--source`.
//
// The diagnostics' wording is deliberately not pinned; validator_test.go grades
// that. This is about prefix, headline and indent.
func TestRunStdin_schemaViolationHeadlineIsBareAndErrorsAreIndented(t *testing.T) {
	schema := repoSchema(t)

	// Decodes cleanly, so it reaches Validate — and then fails it several times
	// over: `layer` is not a valid value AND the required siblings are absent.
	out, code := runStdinText(t, []byte(`{"layer":"e2e"}`), schema)
	if code != 1 {
		t.Fatalf("a schema-invalid document exited %d, want 1; output was:\n%s", code, out)
	}

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if lines[0] != "FAIL" {
		t.Errorf("headline %q, want exactly %q — a schema violation has no filename and no "+
			"inline problem, so nothing may follow the verdict", lines[0], "FAIL")
	}
	if len(lines) < 2 {
		t.Fatalf("a schema violation must print its errors, not just the headline; output was:\n%s", out)
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, continuation) {
			t.Errorf("error line %q, want prefix %q", line, continuation)
		}
		if strings.TrimSpace(strings.TrimPrefix(line, continuation)) == "" {
			t.Errorf("error line %q carries no diagnostic", line)
		}
	}
}

// TestRunStdin_streamIOFailureIsAFindingNotAPanic covers the branch readStdin
// exists for: io.ReadAll failing on the stream ITSELF, as opposed to returning
// bytes that then fail to decode.
//
// readStdin (stdin_mode.go) states the rule — "a caller piping into this mode
// gets a finding, not a stack". Nothing exercised it, so the recovery was as
// unpinned as the panic it prevents. An already-closed descriptor is the
// cheapest real I/O error available; withStdin cannot supply one, since it
// hands over an open file by construction.
func TestRunStdin_streamIOFailureIsAFindingNotAPanic(t *testing.T) {
	schema := repoSchema(t)

	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("writing stdin fixture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening stdin fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing stdin fixture: %v", err)
	}

	saved := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = saved }()

	var code int
	out := captureStdout(t, func() { code = RunStdin(schema) })

	if code != 1 {
		t.Errorf("exit %d, want 1 — an unreadable stream is a failure, never a silent pass", code)
	}
	prefix := "FAIL " + emDash + " could not read/parse JSON: "
	if !strings.HasPrefix(out, prefix) {
		t.Errorf("output %q, want prefix %q", out, prefix)
	}
	if strings.TrimSpace(strings.TrimPrefix(out, prefix)) == "" {
		t.Errorf("output %q reports a read failure and then names nothing", out)
	}
}

// TestRunStdinJSON_documentShape pins the four emission properties that a
// JSON-equality assertion cannot see, because every one of them parses to the
// same document as its wrong answer:
//
//	key order        positional, not alphabetical
//	"errors": []     never null, even on a passing finding
//	"line" / "kind"  null, not 0 and not ""
//	trailing newline exactly one
func TestRunStdinJSON_documentShape(t *testing.T) {
	schema := repoSchema(t)
	valid, err := os.ReadFile(filepath.Join("..", "..", "examples", "unit-order-total.json"))
	if err != nil {
		t.Fatalf("reading a valid fixture: %v", err)
	}

	var out string
	var code int
	withStdin(t, valid, func() {
		out = captureStdout(t, func() { code = RunStdinJSON(schema) })
	})
	if code != 0 {
		t.Fatalf("a valid payload exited %d, want 0", code)
	}

	// Key order, top level and per finding. Asserted as ORDER OF APPEARANCE in
	// the raw text: Go sorts map keys alphabetically, which would give
	// annotations/failed/files and errors/file/kind/line/ok — both of which
	// decode to an identical document.
	wantOrder := []string{
		`"schema"`, `"mode"`, `"ok"`, `"summary"`,
		`"files"`, `"annotations"`, `"failed"`,
		`"findings"`,
		`"file"`, `"line"`, `"ok"`, `"kind"`, `"errors"`,
	}
	cursor := 0
	for _, key := range wantOrder {
		idx := strings.Index(out[cursor:], key)
		if idx < 0 {
			t.Fatalf("key %s missing or out of order; document was:\n%s", key, out)
		}
		cursor += idx + len(key)
	}

	for _, want := range []string{
		`"mode": "stdin"`,
		`"files": 0`,       // stdin is not a file
		`"annotations": 1`, // ...but it is one annotation site
		`"failed": 0`,
		`"line": null`, // not 0
		`"kind": null`, // not ""
		`"errors": []`, // not null
		`"file": "-"`,  // the sentinel the caller passed
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}

	if !strings.HasSuffix(out, "}\n") || strings.HasSuffix(out, "}\n\n") {
		t.Errorf("want exactly one trailing newline, got %q", out[max(0, len(out)-4):])
	}
}

// TestRunStdinJSON_readParseSplit is the distinction run_stdin_json's hoisted
// read exists to preserve, and the one a tidier port collapses.
func TestRunStdinJSON_readParseSplit(t *testing.T) {
	schema := repoSchema(t)
	cases := []struct {
		name            string
		env             string
		input           []byte
		wantKind        string
		wantAnnotations string
	}{
		{
			// Decoded, then rejected by the parser: the site existed and its
			// payload was bad, so it counts.
			name: "malformed but decodable", env: "", input: []byte(`{"broken"`),
			wantKind: `"kind": "parse"`, wantAnnotations: `"annotations": 1`,
		},
		{
			// Never decoded: PROTOCOL.md §1.1 makes UTF-8 part of what a JSON
			// text IS, so there was no site to examine and it does not count —
			// the same rule an unreadable file follows.
			name: "not well-formed UTF-8", env: "", input: []byte("\xff\xfe{}"),
			wantKind: `"kind": "read"`, wantAnnotations: `"annotations": 0`,
		},
		{
			// The distinction has to survive the payload being ALMOST right: a
			// stream that decodes and then fails on a §1.1 rule is a parse
			// failure with a site, not a read failure.
			name: "decodable, refused by §1.1(a)", env: "", input: []byte(`{"a":"\ud800"}`),
			wantKind: `"kind": "parse"`, wantAnnotations: `"annotations": 1`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out string
			var code int
			withStdin(t, tc.input, func() {
				out = captureStdout(t, func() { code = RunStdinJSON(schema) })
			})
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			for _, want := range []string{tc.wantKind, tc.wantAnnotations} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %s in:\n%s", want, out)
				}
			}
		})
	}
}

// TestRunAdopterJSON_threeCountingRules is the headline of this slice: `files`,
// `annotations` and `failed` are three different counts, and the batch below is
// arranged so no two of them agree. A port that reaches for one counter and
// reuses it produces 4/4/4, 4/4/5 or 5/4/5 — all plausible, all wrong.
func TestRunAdopterJSON_threeCountingRules(t *testing.T) {
	schema := repoSchema(t)
	dir := t.TempDir()

	valid, err := os.ReadFile(filepath.Join("..", "..", "examples", "unit-order-total.json"))
	if err != nil {
		t.Fatalf("reading a valid fixture: %v", err)
	}
	write := func(name string, body []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}
	ok := write("ok.json", valid)
	bad := write("bad.json", []byte(`{"broken"`))
	schemafail := write("schemafail.json", []byte(`{"layer":"e2e"}`))
	unread := write("unread.json", []byte(`{}`))

	if err := os.Chmod(unread, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(unread, 0o644)
	if file, err := os.Open(unread); err == nil {
		file.Close()
		t.Skip("chmod 000 did not make the file unreadable (running as root?) — " +
			"the unreadable-file rule cannot be exercised here")
	}

	noMatch := filepath.Join(dir, "nope*.json")
	var out string
	var code int
	out = captureStdout(t, func() {
		code = RunAdopterJSON([]string{ok, bad, schemafail, unread, noMatch}, schema)
	})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}

	for _, want := range []string{
		`"files": 4`,       // four files; the no-match PATTERN is not one
		`"annotations": 3`, // the malformed file counts, the unreadable does not
		`"failed": 4`,      // ...and the no-match, which neither of the above counted
		`"kind": "parse"`,
		`"kind": "read"`,
		`"kind": "schema"`,
		`"kind": "no-match"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}

	// The no-match message is deliberately NOT the text path's repr'd form.
	// PyReprString is right there and is the natural wrong reach.
	if !strings.Contains(out, `"no file(s) match `+noMatch+`"`) {
		t.Errorf("no-match message should be unquoted (%%s, not %%r); got:\n%s", out)
	}
	if strings.Contains(out, `no file(s) match '`) {
		t.Errorf("no-match message was repr'd — that is the TEXT renderer's form:\n%s", out)
	}

	// A passing file still carries kind null and errors [].
	if !strings.Contains(out, `"kind": null`) || !strings.Contains(out, `"errors": []`) {
		t.Errorf("the passing finding should carry kind null and errors []:\n%s", out)
	}
}

// TestRunAdopter_rowVocabulary drives the DEFAULT renderer of the mode an
// adopter runs without any flag at all — `validate-intent FILE...`.
//
// Its --json twin above sat at 100% while this one sat at 15.4%, which is the
// precise condition under which two renderers of one result drift apart with a
// green suite. So the three row shapes are pinned as whole lines, in argument
// order, with nothing else in the output:
//
//	PASS  <path>
//	FAIL  <path>              + one indented `-> ` line per schema error
//	FAIL  <path> — <problem>  the parse failure, inlined, no continuation lines
//
// The batch mixes all three deliberately. A renderer that reached for one row
// shape and reused it still prints three lines and still exits 1.
func TestRunAdopter_rowVocabulary(t *testing.T) {
	schema := repoSchema(t)
	dir := t.TempDir()

	valid, err := os.ReadFile(filepath.Join("..", "..", "examples", "unit-order-total.json"))
	if err != nil {
		t.Fatalf("reading a valid fixture: %v", err)
	}
	write := func(name string, body []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}
	ok := write("ok.json", valid)
	schemafail := write("schemafail.json", []byte(`{"layer":"e2e"}`))
	unparseable := write("bad.json", []byte(`{"broken"`))

	var code int
	out := captureStdout(t, func() {
		code = RunAdopter([]string{ok, schemafail, unparseable}, schema)
	})
	if code != 1 {
		t.Fatalf("a batch containing failures exited %d, want 1; output was:\n%s", code, out)
	}

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	cursor := 0
	next := func(what string) string {
		if cursor >= len(lines) {
			t.Fatalf("output ended before %s; output was:\n%s", what, out)
		}
		line := lines[cursor]
		cursor++
		return line
	}

	// Argument order, one row per file. TWO spaces after the verdict: the
	// column is what a consumer splits on, and a single space survives Contains.
	if got, want := next("the PASS row"), "PASS  "+ok; got != want {
		t.Errorf("row %q, want exactly %q", got, want)
	}

	if got, want := next("the schema-failure headline"), "FAIL  "+schemafail; got != want {
		t.Errorf("row %q, want exactly %q — a schema failure's errors belong on continuation "+
			"lines, not inlined after an em dash", got, want)
	}
	errorLines := 0
	for cursor < len(lines) && strings.HasPrefix(lines[cursor], continuation) {
		if strings.TrimSpace(strings.TrimPrefix(lines[cursor], continuation)) == "" {
			t.Errorf("continuation line %q carries no diagnostic", lines[cursor])
		}
		errorLines++
		cursor++
	}
	if errorLines == 0 {
		t.Errorf("the schema failure printed a headline and no %q lines; output was:\n%s",
			continuation, out)
	}

	// The parse failure inlines its problem after the em dash instead, and gets
	// no continuation lines at all.
	parse := next("the parse-failure row")
	prefix := "FAIL  " + unparseable + " " + emDash + " could not read/parse JSON: "
	switch {
	case strings.HasPrefix(parse, prefix):
		if strings.TrimSpace(strings.TrimPrefix(parse, prefix)) == "" {
			t.Errorf("row %q carries the em dash and then no problem text", parse)
		}
	case strings.HasPrefix(parse, "FAIL  "+unparseable+" - "),
		strings.HasPrefix(parse, "FAIL  "+unparseable+" – "):
		t.Errorf("the separator on %q must be an em dash (U+2014); it is a hyphen or en dash", parse)
	default:
		t.Errorf("row %q, want prefix %q", parse, prefix)
	}

	if cursor != len(lines) {
		t.Errorf("unexpected trailing output: %q", lines[cursor:])
	}
}

// TestRunAdopter_eachRowShapeDrivesTheExitCodeAlone pins what the mixed batch
// above structurally cannot.
//
// In that batch the schema failure and the parse failure BOTH raise the exit
// code, so either one may stop contributing and the aggregate stays 1 — a
// checkOne branch that renders the right row and then returns false is
// invisible there. It is also the branch most easily lost, since the two
// failure shapes return true from different places inside RunAdopter
// (main.go) while the pass returns false between them.
//
// One file per run is the only arrangement in which each shape's contribution
// is observable on its own. The passing case doubles as the proof that 0 is
// still reachable at all: every other assertion in this file wants 1.
func TestRunAdopter_eachRowShapeDrivesTheExitCodeAlone(t *testing.T) {
	schema := repoSchema(t)

	fixture := filepath.Join("..", "..", "examples", "unit-order-total.json")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("shipped fixture %s is missing: %v", fixture, err)
	}
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}

	cases := []struct {
		name     string
		path     string
		wantCode int
		wantOut  string // exact whole output, when it is a single line we own
	}{
		{
			name: "valid file", path: fixture,
			wantCode: 0, wantOut: "PASS  " + fixture + "\n",
		},
		{
			name: "schema-failing file", path: write("schemafail.json", []byte(`{"layer":"e2e"}`)),
			wantCode: 1,
		},
		{
			name: "unparseable file", path: write("bad.json", []byte(`{"broken"`)),
			wantCode: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := captureStdout(t, func() { code = RunAdopter([]string{tc.path}, schema) })
			if code != tc.wantCode {
				t.Errorf("exit %d, want %d; output was:\n%s", code, tc.wantCode, out)
			}
			if tc.wantOut != "" && out != tc.wantOut {
				t.Errorf("output %q, want exactly %q", out, tc.wantOut)
			}
		})
	}
}

package main

// Tests for the two modes slice 3 added — stdin and adopter --json — pinned at
// the level the differential harness cannot isolate.
//
// tests/parity/run_parity.sh proves these byte-identical to python3, which is
// the real acceptance test. What it cannot do is say WHICH property broke when
// a document changes: a whole-document diff reports "stdout differs" whether the
// cause is a miscount, a reordered key or a lost newline. These name the
// properties one at a time, so a failure points at the rule rather than at the
// output.

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
	root, err := DecodeOrdered(raw)
	if err != nil {
		t.Fatalf("decoding the shipped schema: %v", err)
	}
	schema, err := CompileSchema(root)
	if err != nil {
		t.Fatalf("compiling the shipped schema: %v", err)
	}
	return schema
}

// TestRunStdinJSON_documentShape pins the five emission properties that a
// JSON-equality assertion cannot see, because every one of them parses to the
// same document as its wrong answer:
//
//	key order        positional, not alphabetical
//	"errors": []     never null, even on a passing finding
//	"line" / "kind"  null, not 0 and not ""
//	"intent"         present in stdin mode too, not only under --source
//	trailing newline exactly one
func TestRunStdinJSON_documentShape(t *testing.T) {
	t.Setenv("PYTHONIOENCODING", "")
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
		`"file"`, `"line"`, `"ok"`, `"kind"`, `"errors"`, `"intent"`,
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
			// Never decoded: there was no site to examine, so it does not
			// count — the same rule an unreadable file follows.
			name: "undecodable under strict", env: "utf-8", input: []byte("\xff\xfe{}"),
			wantKind: `"kind": "read"`, wantAnnotations: `"annotations": 0`,
		},
		{
			// The very same bytes under the default handler decode into lone
			// surrogates rather than failing, so they reach the parser and are
			// rejected THERE — a different kind and a different annotation count
			// for one unchanged input. That is why the handler is reproduced
			// rather than assumed.
			name: "the same bytes under surrogateescape", env: "", input: []byte("\xff\xfe{}"),
			wantKind: `"kind": "parse"`, wantAnnotations: `"annotations": 1`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PYTHONIOENCODING", tc.env)
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

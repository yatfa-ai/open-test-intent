package main

// Tests for the `files_read` key — the list of files a --json run READ.
//
// WHAT THE KEY ANSWERS. summary.files counts every file the run read, but a
// file read successfully that carries no `@intent` annotation produces no
// finding, so before this key the document could not NAME it: the text renderer
// prints `----  path — no @intent annotations`, the machine renderer printed
// nothing. Subtracting finding paths from a caller's own argument list does not
// recover them either — a directory argument is ONE string that expands to many
// files — so the list has to come from the run.
//
// EVERY TREE HERE CONTAINS A BARE FILE, because that is the one population a
// list reconstructed from findings[] would silently drop. A test over a tree
// where every file carries a finding passes against the wrong implementation.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// filesReadDoc is the subset of the document these tests read.
type filesReadDoc struct {
	OK      bool `json:"ok"`
	Summary struct {
		Files       int `json:"files"`
		Annotations int `json:"annotations"`
		Failed      int `json:"failed"`
	} `json:"summary"`
	Findings []struct {
		File string  `json:"file"`
		Kind *string `json:"kind"`
	} `json:"findings"`
	FilesRead *[]string `json:"files_read"`
}

func parseFilesReadDoc(t *testing.T, out string) filesReadDoc {
	t.Helper()
	var doc filesReadDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the --json document does not parse: %v\n%s", err, out)
	}
	return doc
}

func findingFiles(doc filesReadDoc) map[string]bool {
	seen := map[string]bool{}
	for _, f := range doc.Findings {
		seen[f.File] = true
	}
	return seen
}

// mixedTree writes passing, schema-failing, unreadable, bare files into one
// directory and returns the directory plus each file's path.
func mixedTree(t *testing.T) (dir string, files map[string]string) {
	t.Helper()
	dir = t.TempDir()
	valid, err := os.ReadFile(filepath.Join("..", "..", "examples", "sources", "order_spec.rb"))
	if err != nil {
		t.Fatalf("reading a valid source fixture: %v", err)
	}
	invalid, err := os.ReadFile(filepath.Join("..", "..", "examples", "sources", "invalid", "broken_intent_spec.rb"))
	if err != nil {
		t.Fatalf("reading an invalid source fixture: %v", err)
	}
	files = map[string]string{
		"passing":    "a_passing_spec.rb",
		"failing":    "b_failing_spec.rb",
		"bare":       "c_bare_spec.rb",
		"bare2":      "d_bare_spec.rb",
		"unreadable": "e_unreadable_spec.rb",
	}
	bodies := map[string][]byte{
		"passing":    valid,
		"failing":    invalid,
		"bare":       []byte("# nothing annotated here\n"),
		"bare2":      []byte("describe 'x' do\nend\n"),
		"unreadable": []byte("\xff\xfe\n"),
	}
	for k, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), bodies[k], 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		files[k] = filepath.Join(dir, name)
	}
	return dir, files
}

// The wire literal is a contract: pin it so a rename is a deliberate act.
func TestFilesReadKeyLiteralIsPinned(t *testing.T) {
	if filesReadKey != "files_read" {
		t.Fatalf("the wire key is %q; it is a contract and the README names `files_read`", filesReadKey)
	}
	schema := repoSchema(t)
	plain := writeTemp(t, "plain_spec.rb", []byte("# bare\n"))
	out, _ := runSourceJSON(t, []string{plain}, schema)
	if !strings.Contains(out, "\n  \"files_read\": [") {
		t.Errorf("document lacks the top-level `files_read` key:\n%s", out)
	}
}

// AC1/AC2: source mode, a directory ONE argument, bare files named; length
// equals summary.files, and the list names files findings[] cannot.
func TestSourceJSON_filesReadNamesBareFilesUnderOneDirectoryArgument(t *testing.T) {
	schema := repoSchema(t)
	dir, files := mixedTree(t)

	out, _ := runSourceJSON(t, []string{dir}, schema)
	doc := parseFilesReadDoc(t, out)

	if doc.FilesRead == nil {
		t.Fatalf("files_read missing:\n%s", out)
	}
	got := *doc.FilesRead
	if len(got) != doc.Summary.Files {
		t.Errorf("len(files_read) = %d, summary.files = %d — they must agree", len(got), doc.Summary.Files)
	}
	if doc.Summary.Files != len(files) {
		t.Fatalf("summary.files = %d, want %d", doc.Summary.Files, len(files))
	}
	inFindings := findingFiles(doc)
	for _, key := range []string{"bare", "bare2"} {
		p := files[key]
		if inFindings[p] {
			t.Errorf("bare file %s has a finding; the report.go asymmetry says it must not", p)
		}
		found := false
		for _, g := range got {
			if g == p {
				found = true
			}
		}
		if !found {
			t.Errorf("bare file %s is not named in files_read: %v", p, got)
		}
	}
	// A list built from findings would be strictly shorter on this tree.
	if len(inFindings) >= len(got) {
		t.Errorf("tree must hold files findings[] cannot name (findings paths %d, files_read %d)", len(inFindings), len(got))
	}
	// Same deterministic order the findings use: the walk's order, sorted here.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("files_read is not in the run's deterministic order: %v", got)
			break
		}
	}
}

// AC3/AC4: the mixed tree's findings and counters are what they always were —
// a bare file adds NO finding, and nothing else moved.
func TestSourceJSON_mixedTreeFindingsAndSummaryUnchanged(t *testing.T) {
	schema := repoSchema(t)
	dir, files := mixedTree(t)
	noMatch := filepath.Join(t.TempDir(), "nope-*.rb")

	out, code := runSourceJSON(t, []string{dir, noMatch}, schema)
	doc := parseFilesReadDoc(t, out)

	if code != 1 || doc.OK {
		t.Errorf("exit/ok = %d/%v, want 1/false", code, doc.OK)
	}
	if doc.Summary.Files != 5 {
		t.Errorf("summary.files = %d, want 5 (the no-match pattern is not a file)", doc.Summary.Files)
	}
	// 1 passing annotation site + failing file's sites; the unreadable and bare
	// files contribute none. Derived from the findings so the test states the
	// rule, not a magic number: every non-read, non-no-match finding is a site.
	sites := 0
	failed := 0
	kinds := map[string]int{}
	for _, f := range doc.Findings {
		if f.Kind == nil {
			kinds["pass"]++
			sites++
			continue
		}
		kinds[*f.Kind]++
		if *f.Kind != KindRead && *f.Kind != KindNoMatch {
			sites++
		}
	}
	for _, f := range doc.Findings {
		if f.Kind != nil {
			failed++
		}
	}
	if doc.Summary.Annotations != sites {
		t.Errorf("summary.annotations = %d, want %d", doc.Summary.Annotations, sites)
	}
	if kinds[KindRead] != 1 || kinds[KindNoMatch] != 1 {
		t.Errorf("kinds = %v, want exactly one read and one no-match", kinds)
	}
	if doc.Summary.Failed < failed {
		t.Errorf("summary.failed = %d below the %d failing findings", doc.Summary.Failed, failed)
	}
	inFindings := findingFiles(doc)
	if inFindings[files["bare"]] || inFindings[files["bare2"]] {
		t.Errorf("a bare file produced a finding: %v", inFindings)
	}
}

// AC6: an unreadable file was read-attempted and summary.files counts it, so it
// is in the list — and it also carries its `read` finding.
func TestSourceJSON_unreadableNamedFileIsInFilesRead(t *testing.T) {
	schema := repoSchema(t)
	unreadable := writeTemp(t, "bad_bytes_spec.rb", []byte("\xff\xfe\n"))
	bare := writeTemp(t, "bare_spec.rb", []byte("# bare\n"))

	out, _ := runSourceJSON(t, []string{unreadable, bare}, schema)
	doc := parseFilesReadDoc(t, out)

	want := []string{unreadable, bare}
	if doc.FilesRead == nil || strings.Join(*doc.FilesRead, "|") != strings.Join(want, "|") {
		t.Fatalf("files_read = %v, want %v", doc.FilesRead, want)
	}
	if len(*doc.FilesRead) != doc.Summary.Files {
		t.Errorf("len(files_read) %d != summary.files %d", len(*doc.FilesRead), doc.Summary.Files)
	}
	if !findingFiles(doc)[unreadable] {
		t.Errorf("the unreadable file should ALSO carry a read finding:\n%s", out)
	}
}

// AC7: a pattern that matches nothing contributes no entry, and the list stays
// `[]` (never null) when the run read no file.
func TestSourceJSON_noMatchPatternIsNotInFilesRead(t *testing.T) {
	schema := repoSchema(t)
	noMatch := filepath.Join(t.TempDir(), "nope-*.rb")
	bare := writeTemp(t, "bare_spec.rb", []byte("# bare\n"))

	out, _ := runSourceJSON(t, []string{noMatch, bare}, schema)
	doc := parseFilesReadDoc(t, out)
	if doc.FilesRead == nil || len(*doc.FilesRead) != 1 || (*doc.FilesRead)[0] != bare {
		t.Errorf("files_read = %v, want exactly [%s]", doc.FilesRead, bare)
	}
	if doc.Summary.Files != 1 {
		t.Errorf("summary.files = %d, want 1", doc.Summary.Files)
	}

	only, _ := runSourceJSON(t, []string{noMatch}, schema)
	if !strings.Contains(only, "\"files_read\": []") {
		t.Errorf("a run that read no file must render `files_read: []`:\n%s", only)
	}
}

// AC8: adopter mode emits the key with the same length invariant.
func TestAdopterJSON_filesReadMatchesSummaryFiles(t *testing.T) {
	schema := repoSchema(t)
	valid := filepath.Join("..", "..", "examples", "unit-order-total.json")
	bad := writeTemp(t, "bad.json", []byte("{not json"))
	unreadable := writeTemp(t, "bad_bytes.json", []byte("\xff\xfe"))
	missing := filepath.Join(t.TempDir(), "gone.json") // matches nothing: a no-match, not a file
	noMatch := filepath.Join(t.TempDir(), "nope-*.json")

	var code int
	out := captureStdout(t, func() {
		code = RunAdopterJSON([]string{valid, bad, unreadable, missing, noMatch}, schema)
	})
	doc := parseFilesReadDoc(t, out)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if doc.FilesRead == nil {
		t.Fatalf("files_read missing in adopter mode:\n%s", out)
	}
	want := []string{valid, bad, unreadable}
	if strings.Join(*doc.FilesRead, "|") != strings.Join(want, "|") {
		t.Errorf("files_read = %v, want %v", *doc.FilesRead, want)
	}
	if len(*doc.FilesRead) != doc.Summary.Files {
		t.Errorf("len(files_read) %d != summary.files %d", len(*doc.FilesRead), doc.Summary.Files)
	}
}

// Stdin is not a file: files stays 0 and the list is empty — no new semantics.
func TestStdinJSON_filesReadIsEmpty(t *testing.T) {
	schema := repoSchema(t)
	var out string
	withStdin(t, []byte(`{}`), func() {
		out = captureStdout(t, func() { RunStdinJSON(schema) })
	})
	doc := parseFilesReadDoc(t, out)
	if doc.Summary.Files != 0 || doc.FilesRead == nil || len(*doc.FilesRead) != 0 {
		t.Errorf("stdin: files=%d files_read=%v, want 0 and []", doc.Summary.Files, doc.FilesRead)
	}
}

// Paths use the same injective encoding `file` does, and the key sits AFTER
// `findings` so the existing order-of-appearance pins stay green unedited.
func TestFilesRead_encodingAndPlacement(t *testing.T) {
	report := &JSONReport{Mode: "source"}
	report.noteFileRead("spec/a<b>&\"q\".rb")
	report.noteFileRead("spec/a\xe9.rb")
	out := captureStdout(t, func() { report.Emit(0) })

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("document does not parse: %v\n%s", err, out)
	}
	if strings.Index(out, `"files_read"`) < strings.Index(out, `"findings"`) {
		t.Errorf("files_read must follow findings:\n%s", out)
	}
	if !strings.Contains(out, EncodeJSONPath("spec/a\xe9.rb")) || !strings.Contains(out, "<b>&") {
		t.Errorf("paths are not rendered through EncodeJSONPath / unescaped:\n%s", out)
	}
	if report.Files != 2 {
		t.Errorf("noteFileRead must advance Files; got %d", report.Files)
	}
}

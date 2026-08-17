package opentestintent

// The guard that keeps in-comment source citations from rotting silently.

// This file is corpus_test.go's argument applied to prose instead of bytes: a
// comment is an assertion about the code, nothing recompiles when it stops
// being true, and a wrong one is worse than none because it is followed.
//
// The comments in this repo are long and they cite. Most citations name a
// FUNCTION — `newFixtureSource (cmd/validate-intent/selftest.go)` — and a name
// survives every edit that does not rename the thing it names, which is the
// edit a reader would notice anyway. A minority bolted a LINE COORDINATE on
// instead, and a coordinate is invalidated by any insertion above it, in a file
// the citing file usually does not even live in. Nothing in the toolchain reads
// a comment, so those went stale in the ordinary course of unrelated work and
// stayed stale: at the time this file was written, the repo carried six
// citations pointing at code that had moved out from under them, three of them
// past the end of the file they named.
//
// The fix for that is the name referent, and it has been applied — see the
// commit that introduced this file. This test is what stops the class from
// coming back, and it lives here rather than behind new tooling because
// .agents/ci.yml has exactly two gates and `go test ./...` is one of them. A
// new workflow step, a linter, a script — each would be a second thing to keep
// alive, and the existing root-package pins (corpus_test.go, schema_test.go)
// already establish that cross-artifact consistency checks that walk the
// filesystem belong in this package.
//
// # What is checked
//
// Every `<path>.<ext>:<line>` (and `<path>.<ext>:<line>-<line>`) appearing in a
// COMMENT in a `.go` or `.sh` file must name a file that exists and lines that
// are within that file's length. Both endpoints of a range are checked, and a
// range that runs backwards is a failure too.
//
// Shell files are scanned as well as Go ones, and not as an afterthought: two
// of the six rotted citations lived in scripts/install.sh, where nothing at all
// was watching.
//
// The set of extensions a citation may POINT AT is wider than the set of files
// scanned FOR citations: a comment may cite a .md, .json or .yml, but only .go
// and .sh files are read looking for one. A rotted coordinate inside a README
// is therefore invisible to this test. The asymmetry is deliberate — this
// repo's prose lives in code comments — but it is the kind of thing that
// should be stated outright rather than left to be inferred from a switch.
//
// # Keeping the guard itself honest
//
// A scanner that silently stops scanning reports zero failures, which looks
// exactly like clean prose. That is the vacuous-green shape this repo keeps
// writing files to argue against, and this file has to survive its own
// argument, so the liveness check is made in two places — because one was not
// enough.
//
// The obvious canary is "at least one citation was checked". It is a canary
// over the SUM of the two scanners, and the sum is dominated by Go. The
// migration this file ships with left NO .sh file carrying a citation at all,
// so the entire shell scanner could be replaced with `return nil, nil` and the
// suite would still report the same three Go citations and pass. That is not a
// suspicion: it was tried during review, and it passed.
//
// So the liveness canary is over COMMENTS FOUND, per language, rather than
// citations checked. Every .sh file here has comments, so asserting the shell
// scanner returns some is an assertion about the SCANNER, and it stays true
// however tidy the prose gets. Asserting shell CITATIONS instead would amount
// to asserting that the rot is never cleaned up — the opposite of the point.
//
// And because the shell half is the one with a hand-rolled quoting rule and now
// no live input to exercise it, TestTrailingCommentIndex and TestShellComments
// pin that rule directly against literal lines. The rule is stated in prose
// below; those tests are what make it executable.
//
// # What is NOT checked, and why saying so matters
//
// THIS TEST CATCHES EXISTENCE AND RANGE ROT ONLY. It cannot catch the failure
// mode that motivated the migration, and pretending otherwise would make it the
// kind of check this repo keeps writing files to argue against.
//
// A citation whose target file still exists and whose line number is still
// within it passes here regardless of what now lives on that line. The worst
// real example found in this repo was exactly that shape: a coordinate
// describing where a schema path is built, still comfortably in range, by then
// pointing into the middle of a doc comment on an unrelated struct. It was
// green by every mechanical measure available and wrong to every reader.
//
// So this is the ratchet, not the fix. The fix is the name referent; this file
// exists to make the cheap half of the regression impossible rather than to
// claim the expensive half is handled. A citation that must carry a coordinate
// — into code this repo does not control — should be VERSION-PINNED in the
// prose instead ("rspec-core 3.13.6, example.rb line 117"), which also keeps it
// out of this test's way, since the target is not a file here.
//
// Two narrower limits, stated so nobody has to rediscover them:
//
//   - A continuation coordinate that names no file (the second half of a
//     citation written as "<file>.go:208 and :218") is not matched. Matching a
//     bare colon-number would mean guessing which earlier path it attaches to,
//     and a guess is what this file exists to remove.
//
//   - In shell, a comment is recognised as a whole-line `#` or as a trailing
//     ` #` on a line whose quoting is balanced up to that point. A `#` inside a
//     quoted string is therefore skipped, and so is `${var#prefix}`, which has
//     no space before it. The cost is that a trailing comment on an unbalanced
//     line goes unscanned — a false negative, chosen deliberately over the
//     false positives a hand-rolled model of shell quoting would produce.

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// citation matches a path with a known source extension followed by a line or a
// line range. The extension list is what this repo actually cites into; adding
// one is a one-word change and widens the guard.
var citation = regexp.MustCompile(
	`([A-Za-z0-9_][A-Za-z0-9_./-]*\.(?:go|sh|rb|json|md|yml|yaml|txt)):([0-9]+)(?:-([0-9]+))?`,
)

// skippedDirs are trees whose contents are not this repo's prose.
var skippedDirs = map[string]bool{".git": true, "dist": true}

func TestInCommentCitationsResolve(t *testing.T) {
	lineCounts := map[string]int{}
	// countLines returns the number of lines in path, or -1 if it is not a
	// readable file. Cached because a popular target is cited many times.
	countLines := func(path string) int {
		if n, seen := lineCounts[path]; seen {
			return n
		}
		n := -1
		if data, err := os.ReadFile(path); err == nil {
			n = strings.Count(string(data), "\n")
			if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
				n++
			}
		}
		lineCounts[path] = n
		return n
	}

	checked := 0
	// commentsByExt counts what each scanner actually returned, which is what
	// the liveness canary below is asserted over. See the header: counting
	// citations instead would let the shell scanner die unnoticed.
	commentsByExt := map[string]int{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		var comments []commentLine
		ext := filepath.Ext(path)
		switch ext {
		case ".go":
			comments, err = goComments(path)
			if err != nil {
				return err
			}
		case ".sh":
			comments, err = shellComments(path)
			if err != nil {
				return err
			}
		default:
			return nil
		}
		commentsByExt[ext] += len(comments)

		for _, c := range comments {
			for _, m := range citation.FindAllStringSubmatch(c.text, -1) {
				checked++
				cited, first, last := m[1], m[2], m[3]

				// A citation is written either repo-relative or relative to the
				// directory of the file doing the citing. Both are in use and
				// both are legible, so both resolve.
				target := cited
				if countLines(target) < 0 {
					target = filepath.Join(filepath.Dir(path), cited)
				}
				length := countLines(target)
				if length < 0 {
					t.Errorf("%s:%d cites %q, which is not a file — "+
						"re-point it at the name of what it means rather than deleting it",
						path, c.line, m[0])
					continue
				}

				start, _ := strconv.Atoi(first)
				end := start
				if last != "" {
					end, _ = strconv.Atoi(last)
				}
				switch {
				case start < 1 || end < start:
					t.Errorf("%s:%d cites %q, which is not a line range", path, c.line, m[0])
				case end > length:
					t.Errorf("%s:%d cites %q, but %s is %d lines long — "+
						"the code moved; cite it by name instead",
						path, c.line, m[0], target, length)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	// Liveness, asserted of each scanner separately. A walk that silently
	// stopped matching would report zero failures, which is indistinguishable
	// from a clean tree — and a canary over the two scanners' SUM cannot tell
	// which one went quiet. Every .go and .sh file in this repo carries
	// comments, so a zero here means that scanner broke, and it stays a valid
	// assertion even when a language's prose carries no citations at all,
	// which is exactly the state shell is in today.
	for _, ext := range []string{".go", ".sh"} {
		if commentsByExt[ext] == 0 {
			t.Errorf("no comments were found in any %s file — the %s scanner is not scanning", ext, ext)
		}
	}
	if checked == 0 {
		t.Fatal("no in-comment citations were found at all — the scanner is not scanning")
	}
	t.Logf("checked %d in-comment citation(s); scanned %d Go and %d shell comment(s)",
		checked, commentsByExt[".go"], commentsByExt[".sh"])
}

// commentLine is one comment and the line it starts on, so a failure names
// where to go and not merely what is wrong.
type commentLine struct {
	line int
	text string
}

// goComments returns the comments of a Go file.
//
// go/parser does this exactly, which is the whole reason to use it: a
// hand-rolled scan has to model string literals, raw strings and runes to know
// whether a `//` is a comment, and this repo has 692 lines carrying backticks
// for it to get wrong. A file that does not parse is a compile failure the
// suite will report far more clearly than this test could, so it is skipped
// rather than reported here.
func goComments(path string) ([]commentLine, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil
	}
	out := []commentLine{}
	for _, group := range file.Comments {
		for _, c := range group.List {
			out = append(out, commentLine{line: fset.Position(c.Pos()).Line, text: c.Text})
		}
	}
	return out, nil
}

// shellComments returns the comment text of a shell file, under the rule stated
// in this file's header: a whole-line `#`, or a trailing ` #` on a line whose
// quoting is balanced up to that point.
func shellComments(path string) ([]commentLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := []commentLine{}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, commentLine{line: i + 1, text: trimmed})
			continue
		}
		if idx := trailingCommentIndex(line); idx >= 0 {
			out = append(out, commentLine{line: i + 1, text: line[idx:]})
		}
	}
	return out, nil
}

// trailingCommentIndex returns the index of the `#` starting a trailing
// comment, or -1. It scans quotes rather than searching for the last `#`,
// because the `#` this must not find is the one inside a quoted string.
func trailingCommentIndex(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && quote != '\'':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && i > 0 && (line[i-1] == ' ' || line[i-1] == '\t'):
			return i
		}
	}
	return -1
}

// TestTrailingCommentIndex makes the shell quoting rule executable.
//
// The Go scanner is exercised by this repo's own prose on every run. The shell
// scanner is not: after the migration no .sh file here carries a citation, so
// this rule — the one piece of hand-rolled language modelling in the file —
// could be broken, or deleted outright, with nothing to notice. These cases are
// its input. They are literal lines rather than lines taken from the tree
// precisely so that tidying the prose cannot quietly retire them.
//
// Each case is a line and the comment text expected on it, "" meaning none.
// Asserting the TEXT rather than the index keeps the case table readable as the
// rule it encodes, and a shifted index still fails it.
func TestTrailingCommentIndex(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{{
		name: "trailing comment after balanced double quotes",
		line: `run "$x"   # see foo.sh:12`,
		want: `# see foo.sh:12`,
	}, {
		name: "a tab separates a trailing comment too",
		line: "echo hi\t# see foo.sh:12",
		want: `# see foo.sh:12`,
	}, {
		name: "hash inside a double-quoted string is not a comment",
		line: `echo "a # b"`,
		want: ``,
	}, {
		name: "hash inside a single-quoted string is not a comment",
		line: `echo 'a # b'`,
		want: ``,
	}, {
		name: "parameter expansion has no space before its hash",
		line: `echo ${var#prefix}`,
		want: ``,
	}, {
		name: "an escaped hash is not a comment",
		line: `echo \# not-a-comment`,
		want: ``,
	}, {
		name: "a backslash inside single quotes is literal, so the quote still closes",
		line: `echo 'a\' # see foo.sh:12`,
		want: `# see foo.sh:12`,
	}, {
		name: "quotes may close and reopen before the comment",
		line: `echo 'it'\''s' # see foo.sh:12`,
		want: `# see foo.sh:12`,
	}, {
		// The deliberate false negative named in this file's header. Recorded
		// as a test so that it stays a decision rather than becoming a
		// surprise: a line whose quoting never closes is skipped, because the
		// alternative is a hand-rolled shell parser flagging live code.
		name: "an unbalanced line is skipped rather than guessed at",
		line: `echo "unbalanced # see foo.sh:12`,
		want: ``,
	}, {
		// Whole-line comments are shellComments' job, not this function's —
		// it requires a separator BEFORE the hash, and column zero has none.
		name: "a whole-line comment is not this function's to find",
		line: `# see foo.sh:12`,
		want: ``,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			if idx := trailingCommentIndex(tc.line); idx >= 0 {
				got = tc.line[idx:]
			}
			if got != tc.want {
				t.Errorf("trailingCommentIndex(%q)\n produced %q\n wanted   %q", tc.line, got, tc.want)
			}
		})
	}
}

// TestShellComments pins the whole-file half of the shell scanner: that
// whole-line and indented comments are found, that a quoted hash still is not,
// and that the reported line numbers are the ones a failure message will send a
// reader to. A scanner that finds the right text at the wrong line sends people
// hunting, which is the failure this file exists to stop.
func TestShellComments(t *testing.T) {
	const script = `#!/bin/sh
# a whole-line comment citing a.go:1
echo "a # b"
	# an indented comment citing b.go:2
run "$x"  # a trailing comment citing c.go:3
echo ${v#p}
`
	path := filepath.Join(t.TempDir(), "sample.sh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("writing the sample script: %v", err)
	}

	got, err := shellComments(path)
	if err != nil {
		t.Fatalf("scanning the sample script: %v", err)
	}
	want := []commentLine{
		{line: 1, text: `#!/bin/sh`},
		{line: 2, text: `# a whole-line comment citing a.go:1`},
		{line: 4, text: `# an indented comment citing b.go:2`},
		{line: 5, text: `# a trailing comment citing c.go:3`},
	}
	if len(got) != len(want) {
		t.Fatalf("scanned %d comment(s), wanted %d:\n got %q\n want %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment %d:\n got  line %d %q\n want line %d %q",
				i, got[i].line, got[i].text, want[i].line, want[i].text)
		}
	}
}

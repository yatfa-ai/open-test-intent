package main

// The argv/filesystem channel — pinned against CPython's own os.fsdecode /
// os.fsencode rather than against this port's idea of them.
//
// The defect these exist for: `os.Args` and every name out of a directory
// listing were taken as raw Go strings, so a byte that is not valid UTF-8
// reached the renderers as U+FFFD instead of U+DC00+byte. Three files whose
// names differed by one such byte collapsed onto ONE `file` key in --json while
// `summary.files` still said three.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fsByteCases are the byte strings the channel has to survive. Each is a
// filename shape that actually occurs: a stray high byte, a truncated
// multi-byte character, a lead byte with no continuation, a byte that is not a
// lead byte at all, and — the control — names that decode perfectly well and
// must therefore come through untouched.
var fsByteCases = [][]byte{
	[]byte("plain.json"),
	[]byte("café.json"),
	[]byte("日本語.json"),
	{'x', 0xe9, '.', 'j', 's', 'o', 'n'},
	{'x', 0xfe, '.', 'j', 's', 'o', 'n'},
	{'x', 0xff, '.', 'j', 's', 'o', 'n'},
	{'a', 0xe2, 0x82, 'b'},          // truncated three-byte sequence
	{0xf0, 'p', 'l', 'a', 'i', 'n'}, // lead byte of a four-byte sequence, alone
	{0x80, 0x81, 0x82},              // continuation bytes with no lead
	{0xc3, 0xa9, 0xc3},              // valid é followed by a dangling lead byte
	{0xed, 0xa0, 0x80},              // the WTF-8 encoding of U+D800 — bytes, not a character
}

// TestPyFSDecode_matchesCPython is the differential that matters: CPython's own
// os.fsdecode is the definition, and its repr() is compared rather than the
// string itself, because a lone surrogate cannot be written to a pipe under
// every handler while its repr can. repr(), NOT ascii(): PyReprString
// reproduces repr, which keeps printable non-ASCII literal — ascii() escapes it
// and the two disagree on every ordinary accented filename.
func TestPyFSDecode_matchesCPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	const script = `
import os, sys
data = sys.stdin.buffer.read()
os.write(1, repr(os.fsdecode(data)).encode('utf-8'))
`
	for _, data := range fsByteCases {
		t.Run(fmt.Sprintf("%q", data), func(t *testing.T) {
			cmd := exec.Command(python, "-c", script)
			cmd.Stdin = strings.NewReader(string(data))
			cmd.Stderr = os.Stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("os.fsdecode oracle failed: %v", err)
			}
			// PyReprString over the port's answer is the same repr() CPython
			// just produced, so the two are directly comparable.
			if got := PyReprString(pyFSDecode(data)); got != string(out) {
				t.Errorf("pyFSDecode(%q):\n  go     %s\n  python %s", data, got, out)
			}
		})
	}
}

// TestPyFSEncode_roundTripsEveryDecode pins the property the whole design rests
// on: a decoded path has to encode back to the bytes it came from, or the port
// would name a badly-named file correctly and then fail to open it.
func TestPyFSEncode_roundTripsEveryDecode(t *testing.T) {
	for _, data := range fsByteCases {
		if got := pyFSEncode(pyFSDecode(data)); got != string(data) {
			t.Errorf("round trip of %q gave %q", data, got)
		}
	}
}

// TestPyFSEncode_matchesCPython checks the other direction against os.fsencode,
// so "round-trips" cannot be satisfied by two mistakes that cancel.
func TestPyFSEncode_matchesCPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	const script = `
import os, sys
os.write(1, os.fsencode(os.fsdecode(sys.stdin.buffer.read())))
`
	for _, data := range fsByteCases {
		cmd := exec.Command(python, "-c", script)
		cmd.Stdin = strings.NewReader(string(data))
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("os.fsencode oracle failed for %q: %v", data, err)
		}
		if got := pyFSEncode(pyFSDecode(data)); got != string(out) {
			t.Errorf("pyFSEncode(pyFSDecode(%q)): go %q, python %q", data, got, out)
		}
	}
}

// TestPyFSDecodeArgs_leavesValidArgvAlone is the boring half, and it is the one
// that would catch a decode applied too enthusiastically: every ordinary
// argument must come through byte-identical.
func TestPyFSDecodeArgs_leavesValidArgvAlone(t *testing.T) {
	argv := []string{"--json", "-", "--source", "examples/*.json", "café.json", ""}
	got := pyFSDecodeArgs(argv)
	for i := range argv {
		if got[i] != argv[i] {
			t.Errorf("pyFSDecodeArgs changed %q to %q", argv[i], got[i])
		}
	}
}

// TestFnmatch_tellsSurrogatesApart is the matcher half of the same defect.
// []rune() collapses every WTF-8 surrogate to U+FFFD, which makes two DIFFERENT
// bad-byte names compare equal — the `file`-key collision moved into the glob.
func TestFnmatch_tellsSurrogatesApart(t *testing.T) {
	e9 := pyFSDecode([]byte{'x', 0xe9, '.', 'j', 's', 'o', 'n'})
	ff := pyFSDecode([]byte{'x', 0xff, '.', 'j', 's', 'o', 'n'})

	if !fnmatch(e9, "x?.json") {
		t.Errorf("fnmatch(%q, \"x?.json\") = false, want true — one escaped byte is ONE character", PyReprString(e9))
	}
	if fnmatch(ff, e9) {
		t.Errorf("fnmatch(%q, %q) = true — two distinct bad bytes matched each other",
			PyReprString(ff), PyReprString(e9))
	}
	if !fnmatch(e9, e9) {
		t.Errorf("fnmatch(%q, itself) = false", PyReprString(e9))
	}
}

// TestExpandFiles_keepsBadlyNamedFilesDistinct is the end-to-end shape the
// reviewer measured: real files whose names differ only in undecodable bytes,
// one ordinary ASCII glob, one distinct answer each — in the SAME ORDER python3
// sorts them, which is why the sort has to happen AFTER the decode.
//
// The fixture set is chosen so those two orders actually differ, and the test
// fails if it ever stops being. Every escaped byte becomes U+DC80-U+DCFF, whose
// WTF-8 lead byte is 0xED — so a name containing a real U+FF21 (lead 0xEF)
// sorts AFTER all of them decoded and BETWEEN them raw. Without that one
// character the two orders agree on every high byte and the comparison below
// would pass for an implementation that sorted the raw bytes.
func TestExpandFiles_keepsBadlyNamedFilesDistinct(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	root := t.TempDir()
	names := [][]byte{
		{'x', 0xe9, '.', 'j', 's', 'o', 'n'},
		{'x', 0xee, '.', 'j', 's', 'o', 'n'},
		{'x', 0xf0, '.', 'j', 's', 'o', 'n'},
		{'x', 0xff, '.', 'j', 's', 'o', 'n'},
		[]byte("xé.json"), // decodes cleanly, lead byte below every escape
		[]byte("xＡ.json"), // decodes cleanly, lead byte 0xEF — above them
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, string(name)), []byte("{}"), 0o644); err != nil {
			t.Fatalf("writing fixture %q: %v", name, err)
		}
	}

	got := ExpandFiles(filepath.Join(root, "*.json"))
	if len(got) != len(names) {
		t.Fatalf("ExpandFiles returned %d paths, want %d", len(got), len(names))
	}
	distinct := map[string]bool{}
	for _, path := range got {
		distinct[path] = true
	}
	if len(distinct) != len(names) {
		t.Errorf("ExpandFiles collapsed %d files onto %d distinct paths", len(names), len(distinct))
	}

	// The oracle, ordering included.
	const script = `
import glob, os, sys
os.write(1, "\x00".join(sorted(glob.glob(sys.argv[1]))).encode('utf-8', 'surrogateescape'))
`
	cmd := exec.Command(python, "-c", script, filepath.Join(root, "*.json"))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("glob oracle failed: %v", err)
	}
	want := strings.Split(pyFSDecodeString(string(out)), "\x00")
	if !equalStrings(got, want) {
		t.Errorf("ExpandFiles disagrees with sorted(glob.glob(...)):\n  go     %s\n  python %s",
			reprAll(got), reprAll(want))
	}

	// And the sort is genuinely the decoded one: sorting the raw bytes would
	// have produced a different order for this name set, so an implementation
	// that sorted before decoding cannot pass the comparison above by accident.
	rawOrder := make([]string, len(got))
	for i, path := range got {
		rawOrder[i] = pyFSEncode(path)
	}
	sort.Strings(rawOrder)
	same := true
	for i := range rawOrder {
		if rawOrder[i] != pyFSEncode(got[i]) {
			same = false
		}
	}
	if same {
		t.Errorf("this fixture set no longer distinguishes byte order from code point order — " +
			"pick names that do, or the ordering assertion above is vacuous")
	}
}

func reprAll(paths []string) string {
	parts := make([]string, len(paths))
	for i, p := range paths {
		parts[i] = PyReprString(filepath.Base(p))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

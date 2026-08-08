package main

// Tests for pyutf8.go and pystdout.go: CPython's UTF-8 decoder, both of its
// answers, and the encoder that puts the result back.
//
// These are differential, not table-driven-by-hand. The decoder's error SPAN is
// the thing a port gets subtly wrong — it blames the partial sequence it had
// accepted, not the byte it choked on, and switches message templates when that
// span is wider than one byte — so the assertion is against CPython itself over
// every one- and two-byte sequence that exists, plus a spread of three- and
// four-byte ones. A list typed from documentation would encode the same
// misreading twice.
//
// Skipped, loudly, when python3 is unavailable: an oracle-less differential test
// has verified nothing and must not read as a pass.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// utf8Probe asks CPython, for each hex-encoded byte string, what
// bytes.decode("utf-8") raises and what decode(..., "surrogateescape") yields.
//
// Both answers come from one probe run on one input list so the two can never
// drift out of step, and so the ~66k cases below cost a single subprocess.
const utf8Probe = `
import sys
for line in open(sys.argv[1], "r"):
    line = line.strip()
    if not line:
        continue
    raw = bytes.fromhex(line)
    try:
        raw.decode("utf-8")
        head = "OK"
    except UnicodeDecodeError as exc:
        head = "ERR\t%s" % exc
    escaped = raw.decode("utf-8", "surrogateescape")
    print("%s\t%s" % (head, ",".join(str(ord(ch)) for ch in escaped)))
`

// runUTF8Probe returns one CPython answer per input sequence, in order.
func runUTF8Probe(t *testing.T, sequences [][]byte) []string {
	t.Helper()
	var input strings.Builder
	for _, seq := range sequences {
		input.WriteString(hex.EncodeToString(seq))
		input.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "sequences.txt")
	if err := os.WriteFile(path, []byte(input.String()), 0o644); err != nil {
		t.Fatalf("writing probe input: %v", err)
	}
	out := runPython(t, fmt.Sprintf("import sys\nsys.argv = ['probe', %q]\n%s", path, utf8Probe))
	lines := strings.Split(out, "\n")
	if len(lines) != len(sequences) {
		t.Fatalf("probe returned %d lines for %d sequences", len(lines), len(sequences))
	}
	return lines
}

// utf8Corpus is every sequence the tests below run through both implementations.
//
// Exhaustive where exhaustive is affordable — all 256 single bytes and all 65536
// two-byte sequences — because that is where the lead-byte classification and
// the narrowed second-byte ranges for 0xe0/0xed/0xf0/0xf4 live, and an
// off-by-one in any of those bounds is a whole class of documents classified
// wrongly. Sampled above that, on the boundary values that matter, plus a
// trailing/leading context byte so a non-zero error POSITION is exercised too.
func utf8Corpus() [][]byte {
	var seqs [][]byte
	for b := 0; b < 256; b++ {
		seqs = append(seqs, []byte{byte(b)})
	}
	for hi := 0; hi < 256; hi++ {
		for lo := 0; lo < 256; lo++ {
			seqs = append(seqs, []byte{byte(hi), byte(lo)})
		}
	}
	leads := []byte{0xc1, 0xc2, 0xdf, 0xe0, 0xe1, 0xec, 0xed, 0xee, 0xef, 0xf0, 0xf1, 0xf4, 0xf5, 0xff}
	edges := []byte{0x00, 0x41, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0, 0xff}
	for _, lead := range leads {
		for _, b1 := range edges {
			for _, b2 := range edges {
				seqs = append(seqs, []byte{lead, b1, b2})
				for _, b3 := range edges {
					seqs = append(seqs, []byte{lead, b1, b2, b3})
				}
			}
		}
	}
	// The same hazards with context on both sides, so `position` is not always
	// 0 and a decoder that resumes at the wrong offset is caught.
	withContext := [][]byte{
		[]byte(`{"layer":"` + "\xe2\x82" + `"}`),
		[]byte(`{"layer":"unit` + "\xe2\x82"),
		[]byte("\xff\xfe" + `{"layer":"unit"}`),
		[]byte("caf\xc3\xa9 \xf0\x9f\x98\x80 \xed\xa0\x80 tail"),
		[]byte("ok\xe2\x82\"x"),
		[]byte("a\xc2\xa9b\xc2"),
	}
	return append(seqs, withContext...)
}

func TestUTF8Decode_matchesCPython(t *testing.T) {
	sequences := utf8Corpus()
	answers := runUTF8Probe(t, sequences)

	var checkedErrors, checkedEscapes int
	for i, seq := range sequences {
		fields := strings.Split(answers[i], "\t")
		wantEscaped := fields[len(fields)-1]

		// --- the strict handler: does it fail, and with exactly what prose? ---
		_, _, _, goFailed := utf8DecodeFailure(seq)
		pyFailed := fields[0] == "ERR"
		if goFailed != pyFailed {
			t.Fatalf("%x: python failed=%v, go failed=%v", seq, pyFailed, goFailed)
		}
		if pyFailed {
			checkedErrors++
			if got, want := pyUnicodeDecodeError(seq), fields[1]; got != want {
				t.Fatalf("%x:\n  python: %s\n  go:     %s", seq, want, got)
			}
			// The classifier the callers actually branch on must agree with the
			// one generating the prose, or a file could be reported as a read
			// failure with a message saying it decoded fine.
			if utf8.Valid(seq) {
				t.Fatalf("%x: utf8.Valid says yes but CPython raised %s", seq, fields[1])
			}
		} else if !utf8.Valid(seq) {
			t.Fatalf("%x: utf8.Valid says no but CPython decoded it", seq)
		}

		// --- the surrogateescape handler: the same code points? ---
		checkedEscapes++
		if got := codePointsOf(pySurrogateEscape(seq)); got != wantEscaped {
			t.Fatalf("%x surrogateescape:\n  python: %s\n  go:     %s", seq, wantEscaped, got)
		}
	}

	// A run that compared nothing is not a pass — and with a corpus this large,
	// a probe that silently returned empty answers is a real way to get there.
	if checkedErrors < 1000 || checkedEscapes < len(sequences) {
		t.Fatalf("suspiciously little was checked: %d decode errors, %d escapes over %d sequences",
			checkedErrors, checkedEscapes, len(sequences))
	}
	t.Logf("%d sequences: %d decode failures compared, %d surrogateescape decodes compared",
		len(sequences), checkedErrors, checkedEscapes)
}

// codePointsOf renders a WTF-8 string the way the probe renders a Python str,
// so the two are directly comparable.
func codePointsOf(s string) string {
	runes := pyRunes(s)
	parts := make([]string, 0, len(runes))
	for _, r := range runes {
		parts = append(parts, strconv.Itoa(int(r)))
	}
	return strings.Join(parts, ",")
}

// TestPyUnicodeDecodeError_bothTemplates pins the one thing the differential
// test above cannot state out loud: CPython uses TWO message templates, and the
// plural one is the one a port forgets. Spelled out so a reader sees the
// contract without running python3.
func TestPyUnicodeDecodeError_bothTemplates(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"singular, bad lead byte", []byte{0xff},
			"'utf-8' codec can't decode byte 0xff in position 0: invalid start byte"},
		{"singular, truncated two-byte", []byte{0xc2},
			"'utf-8' codec can't decode byte 0xc2 in position 0: unexpected end of data"},
		{"singular, second byte out of the narrowed range", []byte{0xe0, 0x80, 0x80},
			"'utf-8' codec can't decode byte 0xe0 in position 0: invalid continuation byte"},
		{"plural, truncated three-byte at EOF", []byte{0xe2, 0x82},
			"'utf-8' codec can't decode bytes in position 0-1: unexpected end of data"},
		{"plural, three-byte with a bad third byte", []byte(`{"layer":"` + "\xe2\x82" + `"}`),
			"'utf-8' codec can't decode bytes in position 10-11: invalid continuation byte"},
		{"plural, four-byte with a bad fourth byte", []byte{0xf0, 0x90, 0x80, 0x41},
			"'utf-8' codec can't decode bytes in position 0-2: invalid continuation byte"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pyUnicodeDecodeError(tc.data); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestPySurrogateEscape_roundTrips is the property that makes the WTF-8
// representation safe: whatever bytes go in come back out unchanged through the
// encoder in pystdout.go. If it did not hold, stdin mode would silently rewrite
// the very bytes it was asked to report on.
func TestPySurrogateEscape_roundTrips(t *testing.T) {
	inputs := [][]byte{
		[]byte(""),
		[]byte("plain ascii"),
		[]byte("caf\xc3\xa9"),
		[]byte("\xff"),
		[]byte("\xff\xfe\xfd"),
		[]byte(`{"la` + "\xe2\x82" + `yer":"unit"}`),
		[]byte("\xed\xa0\x80"), // WTF-8 for U+D800: three separately-escaped bytes
		[]byte("mixed \xf0\x9f\x98\x80 emoji \x80 and junk \xc2"),
	}
	for _, in := range inputs {
		decoded := pySurrogateEscape(in)
		encoded, bad, ok := pyEncodeSurrogates(decoded, "surrogateescape")
		if !ok {
			t.Errorf("%x: round trip refused at U+%04X", in, bad)
			continue
		}
		if encoded != string(in) {
			t.Errorf("%x: round-tripped to %x", in, encoded)
		}
	}
}

// TestPyEncodeSurrogates_refusesOutsideTheEscapeRange pins the boundary. Only
// U+DC80-U+DCFF are bytes surrogateescape produced; anything else is a surrogate
// that came from an explicit \uXXXX escape, which CPython refuses to encode —
// so the port must refuse it too rather than invent bytes for it.
func TestPyEncodeSurrogates_refusesOutsideTheEscapeRange(t *testing.T) {
	encodable := []rune{0xdc80, 0xdcab, 0xdcff}
	refused := []rune{0xd800, 0xdbff, 0xdc00, 0xdc7f, 0xdfff}

	for _, r := range encodable {
		var b strings.Builder
		writeCodePoint(&b, r)
		got, bad, ok := pyEncodeSurrogates(b.String(), "surrogateescape")
		if !ok {
			t.Errorf("U+%04X should encode, refused at U+%04X", r, bad)
			continue
		}
		if want := string([]byte{byte(r & 0xff)}); got != want {
			t.Errorf("U+%04X encoded to %x, want %x", r, got, want)
		}
	}
	for _, r := range refused {
		var b strings.Builder
		writeCodePoint(&b, r)
		if _, bad, ok := pyEncodeSurrogates(b.String(), "surrogateescape"); ok {
			t.Errorf("U+%04X should be refused, was encoded", r)
		} else if bad != r {
			t.Errorf("U+%04X refused but reported U+%04X", r, bad)
		}
	}

	// The fast path must not swallow anything: a plain ASCII string is returned
	// untouched, and a string with no surrogates at all is valid UTF-8.
	if got, _, ok := pyEncodeSurrogates("caf\u00e9 <root> &", "surrogateescape"); !ok || got != "caf\u00e9 <root> &" {
		t.Errorf("valid UTF-8 was altered: %q ok=%v", got, ok)
	}
}

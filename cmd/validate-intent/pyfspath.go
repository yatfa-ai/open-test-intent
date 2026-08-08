package main

// The ARGV and FILESYSTEM channel, decoded the way CPython decodes it.
//
// WHY THIS EXISTS — and why `pySurrogateEscape` alone was not enough.
//
// CPython decodes three byte-shaped inputs into `str` before this tool's code
// ever sees them, and it decodes ALL THREE with surrogateescape:
//
//	sys.stdin.read()   the document              -> stdin_mode.go (was already done)
//	sys.argv           the patterns/paths typed  -> here
//	os.listdir/glob    the names found on disk   -> here
//
// Only the first was wired up. `os.Args` and the glob's results were taken as
// raw Go strings, and every renderer downstream iterates them as CODE POINTS —
// so a byte that is not valid UTF-8 decoded to U+FFFD REPLACEMENT CHARACTER
// where the reference produces U+DC00+byte.
//
// It does not take exotic argv to reach. Three real files whose names differ by
// one undecodable byte, matched by an ordinary ASCII glob:
//
//	$ ls -b /tmp/collide            # three DISTINCT files
//	x\351.json   x\376.json   x\377.json
//
//	$ python3 bin/validate-intent --json '/tmp/collide/*.json' | grep '"file"'
//	      "file": "/tmp/collide/x\udce9.json",
//	      "file": "/tmp/collide/x\udcfe.json",
//	      "file": "/tmp/collide/x\udcff.json",
//
//	before this file, the port answered:
//	      "file": "/tmp/collide/x�.json",      (three times — ONE key)
//
// U+FFFD is lossy, so three findings collapse onto one `file` value while
// `summary` still says three. Both documents are 1445 bytes with the same exit
// code and the same structure, which is exactly the shape of divergence that
// survives every test that parses JSON before comparing. `file` is the field
// JSONFinding's own doc calls the consumer's key.
//
// THE ROUND-TRIP IS THE POINT. A path decoded here is later handed BACK to the
// OS by os.ReadFile/os.Stat/os.ReadDir, so it has to encode to the original
// bytes or the port would name a file correctly and then fail to open it. Every
// syscall in this port therefore goes through pyFSEncode, and every name coming
// out of one goes through pyFSDecode. The two are exact inverses over the
// values that can occur — see the round-trip note on pyFSEncode.
//
// SCOPE: the CODEC is utf-8 and the HANDLER is surrogateescape, hard-coded, and
// that is guarded rather than assumed. CPython's filesystem encoding is
// locale-dependent (`PYTHONUTF8=0 LC_ALL=C` makes it `ascii`, which escapes
// every byte >= 0x80 including ones that are perfectly good UTF-8), so
// pylocale.go refuses the whole run in any environment where CPython's default
// codec is not UTF-8. Nothing here has to second-guess that.

import (
	"unicode/utf8"
)

// pyFSDecode is os.fsdecode over bytes: `bytes.decode(sys.getfilesystemencoding(),
// sys.getfilesystemencodeerrors())`, which on this port's supported
// environments is utf-8/surrogateescape.
//
// It is deliberately the SAME function stdin uses (pySurrogateEscape, pyutf8.go)
// rather than a second escaper. A surrogate that arrived through a filename, one
// that arrived through stdin, and one that arrived through a `"\udc82"` JSON
// escape are then the same value downstream, and pyRunes / PyReprString /
// pyJSONDumpsString / pyEncodeSurrogates already handle that value correctly —
// which is precisely why stdin was right while this channel was wrong.
func pyFSDecode(data []byte) string {
	return pySurrogateEscape(data)
}

// pyFSDecodeString is pyFSDecode for a string Go already holds the raw bytes in
// — os.Args entries, os.DirEntry.Name(), os.Executable(). Go performs no
// decoding on any of these: the bytes are simply carried in a string, so
// converting to []byte is free of information loss.
func pyFSDecodeString(s string) string {
	// Fast path: valid UTF-8 decodes to itself under any handler, and this is
	// every path on a normally-named filesystem.
	if utf8.ValidString(s) {
		return s
	}
	return pyFSDecode([]byte(s))
}

// pyFSDecodeArgs is os.fsdecode over sys.argv. main() calls it once, so nothing
// below it ever sees an undecoded argument.
func pyFSDecodeArgs(argv []string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		out[i] = pyFSDecodeString(arg)
	}
	return out
}

// pyFSEncode is os.fsencode: the inverse of pyFSDecode, applied at every point
// a decoded path is handed back to the operating system.
//
// ROUND-TRIP. pyFSDecode maps each byte the UTF-8 decoder rejected to
// U+DC80-U+DCFF (a rejected byte is always >= 0x80, so U+DC00+byte lands in that
// range) and leaves everything else alone; pyFSEncode maps that range back to
// the single byte and re-encodes the rest as UTF-8. The composition is the
// identity in both directions over real path bytes: bytes that DID decode are
// never escaped, so re-encoding them cannot produce a different sequence, and
// bytes that did NOT decode cannot be reconstructed into a valid sequence by
// re-encoding — if they could, they would have decoded.
//
// THE FAILURE BRANCH IS UNREACHABLE, and is written out anyway rather than
// silently dropped. pyEncodeSurrogates refuses a lone surrogate OUTSIDE
// U+DC80-U+DCFF, which surrogateescape never generates — so the only way to
// reach it would be a path string that did not come from this channel. There is
// no such caller. Returning the string unchanged makes the syscall fail with
// ENOENT and report a normal "could not read" finding, rather than naming some
// other file; CPython raises UnicodeEncodeError from os.fsencode on the same
// input, which run_adopter does not catch either.
func pyFSEncode(path string) string {
	encoded, _, ok := pyEncodeSurrogates(path, "surrogateescape")
	if !ok {
		return path
	}
	return encoded
}

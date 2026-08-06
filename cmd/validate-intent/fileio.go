package main

// File reading, schema loading, and the Python-flavoured error prose that goes
// with them.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

// CheckFile is the port of `check_file` (bin/validate-intent:219-247).
//
// The 4-tuple shape is kept deliberately. `kind` is unused by adopter text mode
// — it renders a parse failure and a read failure as the same prose — but a
// consumer of the later --json slice needs to tell them apart, and once the
// result has been flattened to text the distinction is gone for good.
func CheckFile(path string, schema *Schema) (valid bool, errs []string, parseError string, kind string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil, "could not read/parse JSON: " + pyOSError(err), KindRead
	}
	// Python opens with encoding="utf-8", so undecodable bytes raise
	// UnicodeDecodeError during the read and never reach the parser. Go would
	// silently substitute U+FFFD instead, turning a read failure into a
	// successful parse of different text — check explicitly.
	if !utf8.Valid(data) {
		return false, nil, "could not read/parse JSON: " + pyUnicodeDecodeError(data), KindRead
	}
	instance, err := DecodeOrdered(data)
	if err != nil {
		return false, nil, "could not read/parse JSON: " + err.Error(), KindParse
	}
	errs = schema.Validate(instance)
	return len(errs) == 0, errs, "", ""
}

// readSourceText reads a test source file the way check_source_file does
// (bin/validate-intent:434-438).
//
// The error prefix differs from CheckFile's on purpose: the reference says
// "could not read file: %s" here and "could not read/parse JSON: %s" there, and
// both strings are part of the output contract.
//
// On Python's universal-newline translation, which this deliberately does NOT
// implement: open(..., encoding="utf-8") rewrites "\r\n" and a lone "\r" to
// "\n" before the caller sees the text. That is provably unobservable here —
// the only consumer is pySplitlines, which already treats "\r\n" as a single
// terminator and "\r" as a terminator in its own right, so the line sequence is
// identical either way. Implementing it would be a second thing to keep correct
// for no change in behaviour; leaving it out silently would be a divergence
// nobody had checked. It is checked, and it is out.
func readSourceText(path string) (text string, readError string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "could not read file: " + pyOSError(err)
	}
	if !utf8.Valid(data) {
		// Python decodes during the read, so undecodable bytes are a *read*
		// failure and the file never reaches the extractor. Go would silently
		// substitute U+FFFD and happily scan the result.
		return "", "could not read file: " + pyUnicodeDecodeError(data)
	}
	return string(data), ""
}

// SchemaPath is the port of the SCHEMA_PATH constant
// (bin/validate-intent:76-77): the repo root is the parent of the directory
// holding the executable, so a Go binary built into `bin/` resolves the schema
// to exactly the path the Python script does — which is what lets the
// "could not load schema" diagnostic be byte-identical too.
func SchemaPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(exe))
	return filepath.Join(root, "schemas", "open-test-intent.v1.json"), nil
}

// LoadSchema is the port of `load_schema` (bin/validate-intent:250-252). It
// returns the schema path alongside the error so the caller can render the
// reference's diagnostic verbatim.
//
// It does one thing the reference does not: CompileSchema translates and
// compiles every `pattern` keyword up front, and refuses the whole schema if
// any of them cannot be given Python's exact meaning under Go's RE2 engine.
// That refusal is a Go-only exit 2 — Python would have loaded the schema fine —
// and it is deliberate. The alternative is a binary that loads happily and then
// disagrees with the reference about whether a document is valid. See
// pypattern.go for what is accepted, what is refused, and why.
func LoadSchema() (*Schema, string, error) {
	path, err := SchemaPath()
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, errors.New(pyOSError(err))
	}
	if !utf8.Valid(data) {
		return nil, path, errors.New(pyUnicodeDecodeError(data))
	}
	root, err := DecodeOrdered(data)
	if err != nil {
		return nil, path, err
	}
	schema, err := CompileSchema(root)
	if err != nil {
		return nil, path, err
	}
	return schema, path, nil
}

// --------------------------------------------------------------------------- //
// Python exception prose
// --------------------------------------------------------------------------- //

// pyOSError renders an os error the way str(OSError) does in Python:
//
//	[Errno 2] No such file or directory: '/path/to/thing'
//
// Go's syscall.Errno carries the same strerror text but lower-cased, and the
// filename is repr'd rather than bare — both are reproduced here so the
// "could not load schema" and unreadable-file diagnostics match the reference
// byte for byte rather than being a documented divergence.
func pyOSError(err error) string {
	var pathErr *os.PathError
	var errno syscall.Errno
	if errors.As(err, &pathErr) && errors.As(err, &errno) {
		return fmt.Sprintf("[Errno %d] %s: %s",
			int(errno), capitalizeFirst(errno.Error()), PyReprString(pathErr.Path))
	}
	return err.Error()
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// pyUnicodeDecodeError approximates str(UnicodeDecodeError) for a byte string
// that is not valid UTF-8.
//
// Best-effort: Python distinguishes more failure reasons than this and widens
// the reported span past a single byte for some of them. The classification and
// exit code are exact; only the tail of the prose can differ, which is why
// tests/parity/run_parity.sh lists non-UTF-8 input as an excluded case.
func pyUnicodeDecodeError(data []byte) string {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r != utf8.RuneError || size > 1 {
			i += size
			continue
		}
		reason := "invalid start byte"
		if b := data[i]; b >= 0xC2 && b <= 0xF4 {
			want := 2
			switch {
			case b >= 0xF0:
				want = 4
			case b >= 0xE0:
				want = 3
			}
			if len(data)-i < want {
				reason = "unexpected end of data"
			} else {
				reason = "invalid continuation byte"
			}
		}
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s",
			data[i], i, reason)
	}
	return "'utf-8' codec can't decode the input"
}
